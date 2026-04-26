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
	tokenIssued   atomic.Int32
	graphCalled   atomic.Int32
	devicesByName map[string]graphDevice
	graphStatus   int
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
			name := extractFilterName(filter)
			if dev, ok := f.devicesByName[name]; ok {
				_ = json.NewEncoder(w).Encode(graphResp{Value: []graphDevice{dev}})
				return
			}
			_ = json.NewEncoder(w).Encode(graphResp{Value: []graphDevice{}})
		default:
			http.NotFound(w, r)
		}
	})
	return httptest.NewServer(mux)
}

// extractFilterName parses `deviceName eq 'foo'` out of the filter
// expression. Quick and dirty — good enough for tests.
func extractFilterName(filter string) string {
	if !strings.Contains(filter, "deviceName eq") {
		return ""
	}
	parts := strings.Split(filter, "'")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
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
	st, err := intune.lookupAt(context.Background(), srv.URL, "alice-laptop")
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

	st, err := intune.lookupAt(context.Background(), srv.URL, "bob-laptop")
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

	st, err := intune.lookupAt(context.Background(), srv.URL, "charlie")
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

	st, err := intune.lookupAt(context.Background(), srv.URL, "ghost")
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

	_, err := intune.lookupAt(context.Background(), srv.URL, "any")
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
