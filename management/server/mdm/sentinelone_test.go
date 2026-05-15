package mdm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func intPtr(n int) *int { return &n }

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

// ─── configurable compliance gates ────────────────────────────────────

// healthy is a baseline-passing agent: active, not infected, not
// decommissioned. Each subtest layers one opt-in gate on top.
func healthy() s1Agent {
	return s1Agent{
		ComputerName: "host", IsActive: true, OperationalState: "active",
		NetworkStatus: "connected", AgentVersion: "23.4.1.234",
		LastActiveDate: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func TestStatusFromS1Agent_ZeroComplianceIsLegacyBaseline(t *testing.T) {
	// The whole point of the backward-compat contract: an empty
	// compliance config must reproduce the pre-feature behavior.
	st := statusFromS1Agent(healthy(), SentinelOneCompliance{})
	assert.True(t, st.Compliant)

	for _, a := range []s1Agent{
		{IsDecommissioned: true},
		{IsActive: true, Infected: true},
		{IsActive: false},
	} {
		st := statusFromS1Agent(a, SentinelOneCompliance{})
		assert.False(t, st.Compliant, "baseline gate must still block %+v", a)
	}
}

func TestStatusFromS1Agent_BaselineWinsOverOptInGates(t *testing.T) {
	// A decommissioned agent that would satisfy every opt-in gate is
	// still blocked, and the reason is the baseline one (most
	// fundamental signal wins the Reason string).
	a := healthy()
	a.IsDecommissioned = true
	a.EncryptedApplications = true
	a.FirewallEnabled = true
	st := statusFromS1Agent(a, SentinelOneCompliance{
		RequireDiskEncryption:   true,
		RequireFirewall:         true,
		RequireNetworkConnected: true,
	})
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "decommissioned")
}

func TestStatusFromS1Agent_MaxActiveThreats(t *testing.T) {
	a := healthy()
	a.ActiveThreats = 3

	// nil = no threshold → baseline only → compliant.
	assert.True(t, statusFromS1Agent(a, SentinelOneCompliance{}).Compliant)
	// 0 tolerance → blocked.
	st := statusFromS1Agent(a, SentinelOneCompliance{MaxActiveThreats: intPtr(0)})
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "active threats")
	// threshold above the count → allowed.
	assert.True(t, statusFromS1Agent(a, SentinelOneCompliance{MaxActiveThreats: intPtr(5)}).Compliant)
}

func TestStatusFromS1Agent_DiskEncryptionAndFirewall(t *testing.T) {
	a := healthy() // EncryptedApplications/FirewallEnabled default false

	st := statusFromS1Agent(a, SentinelOneCompliance{RequireDiskEncryption: true})
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "disk encryption")

	st = statusFromS1Agent(a, SentinelOneCompliance{RequireFirewall: true})
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "firewall")

	a.EncryptedApplications = true
	a.FirewallEnabled = true
	assert.True(t, statusFromS1Agent(a, SentinelOneCompliance{
		RequireDiskEncryption: true, RequireFirewall: true,
	}).Compliant)
}

func TestStatusFromS1Agent_NetworkConnected(t *testing.T) {
	a := healthy()
	a.NetworkStatus = "disconnected"
	st := statusFromS1Agent(a, SentinelOneCompliance{RequireNetworkConnected: true})
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "not connected")

	// Case-insensitive match on the keyword.
	a.NetworkStatus = "Connected"
	assert.True(t, statusFromS1Agent(a, SentinelOneCompliance{RequireNetworkConnected: true}).Compliant)
}

func TestStatusFromS1Agent_MinAgentVersion(t *testing.T) {
	a := healthy()
	a.AgentVersion = "23.1.0.1"
	st := statusFromS1Agent(a, SentinelOneCompliance{MinAgentVersion: "23.4"})
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "below the required")

	a.AgentVersion = "23.5.0.0"
	assert.True(t, statusFromS1Agent(a, SentinelOneCompliance{MinAgentVersion: "23.4"}).Compliant)

	// Fail-closed: unparseable agent version cannot prove the floor.
	a.AgentVersion = "garbage"
	st = statusFromS1Agent(a, SentinelOneCompliance{MinAgentVersion: "23.4"})
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "unparseable")

	// Fail-closed: misconfigured floor surfaces, does not silently pass.
	a.AgentVersion = "99.0"
	st = statusFromS1Agent(a, SentinelOneCompliance{MinAgentVersion: "not-a-version"})
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "invalid")
}

func TestStatusFromS1Agent_SyncWindow(t *testing.T) {
	a := healthy()
	a.LastActiveDate = time.Now().UTC().Add(-3 * time.Hour).Format(time.RFC3339Nano)

	// 24h window → 3h-old check-in is fine.
	assert.True(t, statusFromS1Agent(a, SentinelOneCompliance{SyncWindowMinutes: 1440}).Compliant)

	// 60m window → 3h-old check-in is stale.
	st := statusFromS1Agent(a, SentinelOneCompliance{SyncWindowMinutes: 60})
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Reason, "sync window")

	// Fail-closed: empty / unparseable timestamp is treated as stale.
	a.LastActiveDate = ""
	st = statusFromS1Agent(a, SentinelOneCompliance{SyncWindowMinutes: 60})
	assert.False(t, st.Compliant)

	a.LastActiveDate = "not-a-date"
	st = statusFromS1Agent(a, SentinelOneCompliance{SyncWindowMinutes: 60})
	assert.False(t, st.Compliant)
}
