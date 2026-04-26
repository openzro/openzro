package mdm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newS1Server(t *testing.T, agents map[string]s1Agent, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "ApiToken test-token", r.Header.Get("Authorization"),
			"S1 expects 'ApiToken <secret>' literal — not 'Bearer'")

		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			return
		}

		name := r.URL.Query().Get("computerName")
		var resp s1Resp
		if a, ok := agents[name]; ok {
			resp.Data = []s1Agent{a}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func newS1For(t *testing.T, srv *httptest.Server) *SentinelOne {
	t.Helper()
	s, err := NewSentinelOne(SentinelOneConfig{
		ManagementURL: srv.URL,
		APIToken:      "test-token",
	})
	require.NoError(t, err)
	return s
}

func TestSentinelOne_RequiresCredentials(t *testing.T) {
	_, err := NewSentinelOne(SentinelOneConfig{ManagementURL: "x"})
	assert.Error(t, err, "api_token is required")
}

func TestSentinelOne_HealthyAgentIsCompliant(t *testing.T) {
	srv := newS1Server(t, map[string]s1Agent{
		"alice-laptop": {
			ComputerName: "alice-laptop", IsActive: true,
			OperationalState: "active",
		},
	}, 0)
	defer srv.Close()

	st, err := newS1For(t, srv).lookupAt(context.Background(), srv.URL, "alice-laptop")
	require.NoError(t, err)
	assert.True(t, st.Found)
	assert.True(t, st.Compliant)
}

func TestSentinelOne_InfectedIsNonCompliant(t *testing.T) {
	srv := newS1Server(t, map[string]s1Agent{
		"bob": {ComputerName: "bob", IsActive: true, Infected: true},
	}, 0)
	defer srv.Close()

	st, err := newS1For(t, srv).lookupAt(context.Background(), srv.URL, "bob")
	require.NoError(t, err)
	assert.True(t, st.Found)
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "infection")
}

func TestSentinelOne_DecommissionedAgent(t *testing.T) {
	srv := newS1Server(t, map[string]s1Agent{
		"old": {ComputerName: "old", IsDecommissioned: true},
	}, 0)
	defer srv.Close()

	st, err := newS1For(t, srv).lookupAt(context.Background(), srv.URL, "old")
	require.NoError(t, err)
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "decommissioned")
}

func TestSentinelOne_InactiveAgent(t *testing.T) {
	srv := newS1Server(t, map[string]s1Agent{
		"sleepy": {
			ComputerName: "sleepy", IsActive: false,
			OperationalState: "offline",
		},
	}, 0)
	defer srv.Close()

	st, err := newS1For(t, srv).lookupAt(context.Background(), srv.URL, "sleepy")
	require.NoError(t, err)
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "inactive")
}

func TestSentinelOne_NotFound(t *testing.T) {
	srv := newS1Server(t, map[string]s1Agent{}, 0)
	defer srv.Close()

	st, err := newS1For(t, srv).lookupAt(context.Background(), srv.URL, "ghost")
	require.NoError(t, err)
	assert.False(t, st.Found)
	assert.Contains(t, st.Reason, "no S1 agent")
}

func TestSentinelOne_403GivesActionableError(t *testing.T) {
	srv := newS1Server(t, nil, http.StatusForbidden)
	defer srv.Close()

	_, err := newS1For(t, srv).lookupAt(context.Background(), srv.URL, "any")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Viewer role")
}
