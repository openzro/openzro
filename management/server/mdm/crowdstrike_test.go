package mdm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// newCSServer stands up a fake Falcon endpoint that handles:
//   - POST /oauth2/token       (issue a bearer token)
//   - GET  /devices/queries/devices/v1?filter=hostname:'<host>'
//   - GET  /devices/entities/devices/v2?ids=<aid>
//
// devices is keyed by hostname; the AID is hostname + "-aid".
func newCSServer(t *testing.T, devices map[string]csDevice, statusOverride int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "test-id", r.PostForm.Get("client_id"))
		assert.Equal(t, "test-secret", r.PostForm.Get("client_secret"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"test-token","token_type":"Bearer","expires_in":1799}`)
	})

	mux.HandleFunc("/devices/queries/devices/v1", func(w http.ResponseWriter, r *http.Request) {
		if statusOverride != 0 && statusOverride != http.StatusOK {
			w.WriteHeader(statusOverride)
			return
		}
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"),
			"Falcon expects an OAuth bearer token")
		filter, _ := url.QueryUnescape(r.URL.Query().Get("filter"))
		host := strings.TrimPrefix(strings.TrimSuffix(filter, "'"), "hostname:'")
		var resp csQueryResp
		if _, ok := devices[host]; ok {
			resp.Resources = []string{host + "-aid"}
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/devices/entities/devices/v2", func(w http.ResponseWriter, r *http.Request) {
		if statusOverride != 0 && statusOverride != http.StatusOK {
			w.WriteHeader(statusOverride)
			return
		}
		ids := r.URL.Query().Get("ids")
		host := strings.TrimSuffix(ids, "-aid")
		var resp csEntitiesResp
		if d, ok := devices[host]; ok {
			resp.Resources = []csDevice{d}
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	return httptest.NewServer(mux)
}

func newCSFor(t *testing.T, srv *httptest.Server) *CrowdStrike {
	t.Helper()
	c, err := NewCrowdStrike(CrowdStrikeConfig{
		ClientID:     "test-id",
		ClientSecret: "test-secret",
	})
	require.NoError(t, err)
	// Override base URL + token URL so the OAuth client and
	// device lookups both hit the test server.
	overrideCSEndpoints(c, srv.URL)
	return c
}

// overrideCSEndpoints rewires the driver to point at an httptest
// server. We can't do this through public API because the OAuth2
// http.Client is built once at NewCrowdStrike — rebuild it here so
// the bearer-token round trip resolves to the test server too.
func overrideCSEndpoints(c *CrowdStrike, base string) {
	cc := &clientcredentials.Config{
		ClientID:     c.cfg.ClientID,
		ClientSecret: c.cfg.ClientSecret,
		TokenURL:     base + "/oauth2/token",
		AuthStyle:    oauth2.AuthStyleInParams,
	}
	httpClient := cc.Client(context.Background())
	httpClient.Timeout = 10 * time.Second
	c.baseURL = base
	c.client = httpClient
}

func TestCrowdStrike_RequiresCredentials(t *testing.T) {
	_, err := NewCrowdStrike(CrowdStrikeConfig{ClientID: "x"})
	assert.Error(t, err)
}

func TestCrowdStrike_UnknownCloudIsRejected(t *testing.T) {
	_, err := NewCrowdStrike(CrowdStrikeConfig{
		ClientID: "x", ClientSecret: "y", Cloud: "mars-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown cloud")
}

func TestCrowdStrike_NormalSensorIsCompliant(t *testing.T) {
	srv := newCSServer(t, map[string]csDevice{
		"alice-laptop": {Hostname: "alice-laptop", Status: "normal", ReducedFunctionalityMode: "no"},
	}, 0)
	defer srv.Close()

	st, err := newCSFor(t, srv).lookupAt(context.Background(), srv.URL, "alice-laptop")
	require.NoError(t, err)
	assert.True(t, st.Found)
	assert.True(t, st.Compliant)
}

func TestCrowdStrike_ContainedHostIsNonCompliant(t *testing.T) {
	srv := newCSServer(t, map[string]csDevice{
		"quarantined": {Hostname: "quarantined", Status: "contained"},
	}, 0)
	defer srv.Close()

	st, err := newCSFor(t, srv).lookupAt(context.Background(), srv.URL, "quarantined")
	require.NoError(t, err)
	assert.True(t, st.Found)
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "contained")
}

func TestCrowdStrike_ReducedFunctionalityModeIsNonCompliant(t *testing.T) {
	srv := newCSServer(t, map[string]csDevice{
		"impaired": {Hostname: "impaired", Status: "normal", ReducedFunctionalityMode: "yes"},
	}, 0)
	defer srv.Close()

	st, err := newCSFor(t, srv).lookupAt(context.Background(), srv.URL, "impaired")
	require.NoError(t, err)
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "reduced_functionality_mode")
}

func TestCrowdStrike_NotFound(t *testing.T) {
	srv := newCSServer(t, map[string]csDevice{}, 0)
	defer srv.Close()

	st, err := newCSFor(t, srv).lookupAt(context.Background(), srv.URL, "ghost")
	require.NoError(t, err)
	assert.False(t, st.Found)
	assert.Contains(t, st.Reason, "no Falcon sensor")
}

func TestCrowdStrike_403GivesActionableError(t *testing.T) {
	srv := newCSServer(t, nil, http.StatusForbidden)
	defer srv.Close()

	_, err := newCSFor(t, srv).lookupAt(context.Background(), srv.URL, "any")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Hosts:Read")
}

func TestCrowdStrike_401GivesRegionHint(t *testing.T) {
	srv := newCSServer(t, nil, http.StatusUnauthorized)
	defer srv.Close()

	_, err := newCSFor(t, srv).lookupAt(context.Background(), srv.URL, "any")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cloud")
}
