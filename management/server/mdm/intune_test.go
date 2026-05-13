package mdm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAzure simulates Microsoft Entra's token endpoint and Graph's
// /managedDevices endpoint on a single httptest server. Tests
// override the Intune driver's `client` to point at it and call
// lookupAt with the server URL.
type fakeAzure struct {
	tokenIssued atomic.Int32
	graphCalled atomic.Int32

	// devicesByName matches the legacy hostname-only filter path.
	// Tests that don't exercise the combined / UPN filter still
	// populate this map.
	devicesByName map[string]graphDevice

	// devicesByNameUPN keys on "<deviceName>|<userPrincipalName>" and
	// satisfies the combined filter only. Useful to assert that the
	// combined filter wins over the hostname-only one.
	devicesByNameUPN map[string]graphDevice

	// devicesByUPN keys on userPrincipalName alone — UPN-only
	// fallback. Used to validate the rename-hostname recovery path.
	devicesByUPN map[string]graphDevice

	// lastFilter records the most recent $filter the driver sent.
	// Tests use this to assert the filter shape.
	lastFilter string
	// lastSelect records the most recent $select projection.
	lastSelect string

	graphStatus int
}

func (f *fakeAzure) start(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/oauth2/v2.0/token"):
			f.tokenIssued.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"dummy","token_type":"Bearer","expires_in":3600}`))

		case strings.Contains(r.URL.Path, "/managedDevices"):
			f.graphCalled.Add(1)
			if f.graphStatus != 0 && f.graphStatus != http.StatusOK {
				w.WriteHeader(f.graphStatus)
				return
			}
			filter := r.URL.Query().Get("$filter")
			f.lastFilter = filter
			f.lastSelect = r.URL.Query().Get("$select")

			name, upn := parseFilter(filter)
			var devs []graphDevice
			switch {
			case name != "" && upn != "":
				if dev, ok := f.devicesByNameUPN[name+"|"+upn]; ok {
					devs = []graphDevice{dev}
				}
			case upn != "":
				if dev, ok := f.devicesByUPN[upn]; ok {
					devs = []graphDevice{dev}
				}
			case name != "":
				if dev, ok := f.devicesByName[name]; ok {
					devs = []graphDevice{dev}
				}
			}
			if devs == nil {
				devs = []graphDevice{}
			}
			_ = json.NewEncoder(w).Encode(graphResp{Value: devs})
		default:
			http.NotFound(w, r)
		}
	})
	return httptest.NewServer(mux)
}

// parseFilter extracts the deviceName and userPrincipalName values
// from any of the three filter shapes the driver emits. Returns empty
// strings for fields the filter doesn't mention.
func parseFilter(filter string) (name, upn string) {
	// Filter values are quoted with single quotes; literal single
	// quotes inside the value are doubled (OData escape). The driver
	// only emits alphanumeric / @ / . / - / _ characters so we don't
	// need a full OData parser here.
	if i := strings.Index(filter, "deviceName eq '"); i >= 0 {
		rest := filter[i+len("deviceName eq '"):]
		if end := strings.Index(rest, "'"); end >= 0 {
			name = rest[:end]
		}
	}
	if i := strings.Index(filter, "userPrincipalName eq '"); i >= 0 {
		rest := filter[i+len("userPrincipalName eq '"):]
		if end := strings.Index(rest, "'"); end >= 0 {
			upn = rest[:end]
		}
	}
	return
}

func newIntuneFor(t *testing.T, srv *httptest.Server) *Intune {
	t.Helper()
	cfg := IntuneConfig{
		TenantID:     "tenant",
		ClientID:     "client",
		ClientSecret: "secret",
		Authority:    srv.URL, // override the login.microsoftonline.com default
	}
	intune, err := NewIntune(cfg)
	require.NoError(t, err)
	return intune
}

func TestIntune_RequiresCredentials(t *testing.T) {
	_, err := NewIntune(IntuneConfig{TenantID: "t", ClientID: "c"})
	assert.Error(t, err, "client_secret is required")
}

func TestIntune_CompliantDevice(t *testing.T) {
	f := &fakeAzure{
		devicesByName: map[string]graphDevice{
			"alice-laptop": {
				ID: "uuid", DeviceName: "alice-laptop",
				ComplianceState: "compliant", ManagementState: "managed",
			},
		},
	}
	srv := f.start(t)
	defer srv.Close()

	intune := newIntuneFor(t, srv)
	st, err := intune.lookupAt(context.Background(), srv.URL, DeviceLookup{Hostname: "alice-laptop"})
	require.NoError(t, err)
	assert.True(t, st.Found)
	assert.True(t, st.Compliant)
	assert.Equal(t, int32(1), f.tokenIssued.Load(), "token must be acquired")
	assert.Equal(t, int32(1), f.graphCalled.Load())
}

func TestIntune_InGracePeriodIsCompliant(t *testing.T) {
	f := &fakeAzure{
		devicesByName: map[string]graphDevice{
			"bob-laptop": {DeviceName: "bob-laptop", ComplianceState: "inGracePeriod"},
		},
	}
	srv := f.start(t)
	defer srv.Close()
	intune := newIntuneFor(t, srv)

	st, err := intune.lookupAt(context.Background(), srv.URL, DeviceLookup{Hostname: "bob-laptop"})
	require.NoError(t, err)
	assert.True(t, st.Compliant,
		"inGracePeriod is documented as 'compliant pending re-evaluation' — treat as compliant")
}

func TestIntune_NoncompliantSurfacesState(t *testing.T) {
	f := &fakeAzure{
		devicesByName: map[string]graphDevice{
			"charlie": {DeviceName: "charlie", ComplianceState: "noncompliant", ManagementState: "managed"},
		},
	}
	srv := f.start(t)
	defer srv.Close()
	intune := newIntuneFor(t, srv)

	st, err := intune.lookupAt(context.Background(), srv.URL, DeviceLookup{Hostname: "charlie"})
	require.NoError(t, err)
	assert.True(t, st.Found)
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "noncompliant")
}

func TestIntune_DeviceNotFound(t *testing.T) {
	f := &fakeAzure{devicesByName: map[string]graphDevice{}}
	srv := f.start(t)
	defer srv.Close()
	intune := newIntuneFor(t, srv)

	st, err := intune.lookupAt(context.Background(), srv.URL, DeviceLookup{Hostname: "ghost"})
	require.NoError(t, err)
	assert.False(t, st.Found)
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "not enrolled")
}

func TestIntune_403GivesActionableError(t *testing.T) {
	f := &fakeAzure{graphStatus: http.StatusForbidden}
	srv := f.start(t)
	defer srv.Close()
	intune := newIntuneFor(t, srv)

	_, err := intune.lookupAt(context.Background(), srv.URL, DeviceLookup{Hostname: "any"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DeviceManagementManagedDevices.Read.All",
		"403 reason must point operators at the missing Graph permission")
}

func TestIntune_FilterEscapesSingleQuotes(t *testing.T) {
	// OData filter: a single quote in the value is escaped by
	// doubling. Without this, `deviceName eq 'O'Brien-laptop'` is a
	// syntax error.
	assert.Equal(t, "O''Brien-laptop", escapeFilterValue("O'Brien-laptop"))
}

// TestIntune_CombinedFilterPrefersUserMatch validates that when both
// hostname and user email are present, the driver issues a combined
// deviceName + userPrincipalName filter on the first try.
func TestIntune_CombinedFilterPrefersUserMatch(t *testing.T) {
	f := &fakeAzure{
		devicesByNameUPN: map[string]graphDevice{
			"shared-laptop|alice@example.com": {
				DeviceName: "shared-laptop", ComplianceState: "compliant",
				UserPrincipalName: "alice@example.com",
			},
		},
	}
	srv := f.start(t)
	defer srv.Close()
	intune := newIntuneFor(t, srv)

	st, err := intune.lookupAt(context.Background(), srv.URL,
		DeviceLookup{Hostname: "shared-laptop", UserEmail: "alice@example.com"})
	require.NoError(t, err)
	assert.True(t, st.Compliant)
	assert.Equal(t, int32(1), f.graphCalled.Load(),
		"single Graph hit when the combined filter resolves on the first try")
	assert.Contains(t, f.lastFilter, "deviceName eq 'shared-laptop'")
	assert.Contains(t, f.lastFilter, "userPrincipalName eq 'alice@example.com'")
}

// TestIntune_FallsBackToUPNWhenHostnameChanged simulates the
// renamed-hostname recovery path: the agent reports a hostname Intune
// no longer recognises, but the user's email anchor still resolves to
// the device.
func TestIntune_FallsBackToUPNWhenHostnameChanged(t *testing.T) {
	f := &fakeAzure{
		devicesByUPN: map[string]graphDevice{
			"bob@example.com": {
				DeviceName: "bob-NEW", ComplianceState: "compliant",
				UserPrincipalName: "bob@example.com",
			},
		},
	}
	srv := f.start(t)
	defer srv.Close()
	intune := newIntuneFor(t, srv)

	// Driver tries combined → empty → UPN-only → hit.
	st, err := intune.lookupAt(context.Background(), srv.URL,
		DeviceLookup{Hostname: "bob-OLD", UserEmail: "bob@example.com"})
	require.NoError(t, err)
	assert.True(t, st.Compliant)
	assert.Equal(t, int32(2), f.graphCalled.Load(),
		"two Graph hits: combined miss, then UPN-only fallback")
}

// TestIntune_HostnameOnlyWhenNoEmail covers the setup-key path where
// the peer was registered without a user attribution. The driver
// degrades to the legacy hostname-only filter on the first try.
func TestIntune_HostnameOnlyWhenNoEmail(t *testing.T) {
	f := &fakeAzure{
		devicesByName: map[string]graphDevice{
			"server-01": {
				DeviceName: "server-01", ComplianceState: "compliant",
			},
		},
	}
	srv := f.start(t)
	defer srv.Close()
	intune := newIntuneFor(t, srv)

	st, err := intune.lookupAt(context.Background(), srv.URL,
		DeviceLookup{Hostname: "server-01"})
	require.NoError(t, err)
	assert.True(t, st.Compliant)
	assert.Equal(t, int32(1), f.graphCalled.Load(),
		"with no UserEmail the driver skips the combined + UPN-only filters")
	assert.NotContains(t, f.lastFilter, "userPrincipalName",
		"hostname-only path must not include a userPrincipalName clause")
}

// TestIntune_SelectProjection asserts $select is pinned on the URL.
// Reduces payload and protects against bandwidth regression on tenants
// with thousands of managed devices.
func TestIntune_SelectProjection(t *testing.T) {
	f := &fakeAzure{
		devicesByName: map[string]graphDevice{
			"x": {DeviceName: "x", ComplianceState: "compliant"},
		},
	}
	srv := f.start(t)
	defer srv.Close()
	intune := newIntuneFor(t, srv)

	_, err := intune.lookupAt(context.Background(), srv.URL,
		DeviceLookup{Hostname: "x"})
	require.NoError(t, err)
	assert.Contains(t, f.lastSelect, "complianceState")
	assert.Contains(t, f.lastSelect, "userPrincipalName")
}

// TestIntune_StrictComplianceRejectsGracePeriod validates the
// opt-in strict mode: when StrictCompliance=true on the config,
// inGracePeriod no longer maps to compliant.
func TestIntune_StrictComplianceRejectsGracePeriod(t *testing.T) {
	f := &fakeAzure{
		devicesByName: map[string]graphDevice{
			"strict": {DeviceName: "strict", ComplianceState: "inGracePeriod"},
		},
	}
	srv := f.start(t)
	defer srv.Close()
	cfg := IntuneConfig{
		TenantID: "t", ClientID: "c", ClientSecret: "s",
		Authority:        srv.URL,
		StrictCompliance: true,
	}
	intune, err := NewIntune(cfg)
	require.NoError(t, err)

	st, err := intune.lookupAt(context.Background(), srv.URL,
		DeviceLookup{Hostname: "strict"})
	require.NoError(t, err)
	assert.True(t, st.Found)
	assert.False(t, st.Compliant,
		"strict mode treats inGracePeriod as non-compliant")
}
