package mdm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newHuntressServer(t *testing.T, agents map[string]huntressAgent, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		auth := r.Header.Get("Authorization")
		require.True(t, len(auth) > len("Basic "), "Authorization header should be Basic")
		raw, err := base64.StdEncoding.DecodeString(auth[len("Basic "):])
		require.NoError(t, err)
		assert.Equal(t, "key:secret", string(raw),
			"Huntress uses Basic auth with API Key as username")

		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			return
		}

		host := r.URL.Query().Get("hostname")
		var resp huntressResp
		if a, ok := agents[host]; ok {
			resp.Agents = []huntressAgent{a}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func newHuntressFor(t *testing.T, srv *httptest.Server) *Huntress {
	t.Helper()
	h, err := NewHuntress(HuntressConfig{APIKey: "key", APISecret: "secret"})
	require.NoError(t, err)
	h.base = srv.URL
	return h
}

func TestHuntress_RequiresCredentials(t *testing.T) {
	_, err := NewHuntress(HuntressConfig{APIKey: "k"})
	assert.Error(t, err, "api_secret is required")
}

func TestHuntress_HealthyAgent(t *testing.T) {
	srv := newHuntressServer(t, map[string]huntressAgent{
		"alice": {Hostname: "alice", IncidentReportsCount: 0},
	}, 0)
	defer srv.Close()

	st, err := newHuntressFor(t, srv).lookupAt(context.Background(), srv.URL, "alice")
	require.NoError(t, err)
	assert.True(t, st.Compliant)
}

func TestHuntress_OpenIncidentBlocks(t *testing.T) {
	srv := newHuntressServer(t, map[string]huntressAgent{
		"bob": {Hostname: "bob", IncidentReportsCount: 2},
	}, 0)
	defer srv.Close()

	st, err := newHuntressFor(t, srv).lookupAt(context.Background(), srv.URL, "bob")
	require.NoError(t, err)
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "incident")
}

func TestHuntress_OutdatedAgent(t *testing.T) {
	srv := newHuntressServer(t, map[string]huntressAgent{
		"old": {Hostname: "old", OutdatedAgentVersion: true},
	}, 0)
	defer srv.Close()

	st, err := newHuntressFor(t, srv).lookupAt(context.Background(), srv.URL, "old")
	require.NoError(t, err)
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "outdated")
}

func TestHuntress_NotFound(t *testing.T) {
	srv := newHuntressServer(t, map[string]huntressAgent{}, 0)
	defer srv.Close()

	st, err := newHuntressFor(t, srv).lookupAt(context.Background(), srv.URL, "ghost")
	require.NoError(t, err)
	assert.False(t, st.Found)
}

func TestHuntress_AuthErrorActionable(t *testing.T) {
	srv := newHuntressServer(t, nil, http.StatusUnauthorized)
	defer srv.Close()

	_, err := newHuntressFor(t, srv).lookupAt(context.Background(), srv.URL, "any")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API Key and Secret")
}
