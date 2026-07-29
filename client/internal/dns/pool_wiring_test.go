package dns

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/client/internal/dns/selection"
	"github.com/openzro/openzro/client/internal/peer"
	nbdns "github.com/openzro/openzro/dns"
)

// TestResolveSelectionPolicy pins how a configured policy name becomes a policy.
// Nothing configured and a name we do not know both have to leave the server on
// its pre-existing path: a DNS knob is not worth refusing to resolve over, so an
// unknown name is a warning and not an error.
func TestResolveSelectionPolicy(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		// normalized is the name the resolved policy must report, when it differs
		// from what was configured. Empty means "same as configured".
		normalized string
		wantNil    bool
		wantRanks  bool
	}{
		{
			name:       "nothing configured keeps the unpooled path",
			configured: "",
			wantNil:    true,
		},
		{
			name:       "the default policy is not pooled",
			configured: selection.DefaultPolicy,
		},
		{
			name:       "prefer_private ranks, so its zones are pooled",
			configured: "prefer_private",
			wantRanks:  true,
		},
		{
			name:       "an unknown name falls back instead of failing",
			configured: "prefer_pubic",
			wantNil:    true,
		},
		{
			// Hand-edited config file: case and stray space are typos, not policies.
			name:       "case is ignored",
			configured: "Prefer_Private",
			normalized: "prefer_private",
			wantRanks:  true,
		},
		{
			name:       "surrounding space is ignored",
			configured: "  prefer_private\t",
			normalized: "prefer_private",
			wantRanks:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := resolveSelectionPolicy(tt.configured)

			if tt.wantNil {
				assert.Nil(t, policy)
			} else {
				require.NotNil(t, policy)
				want := tt.normalized
				if want == "" {
					want = tt.configured
				}
				assert.Equal(t, want, policy.Name())
			}
			assert.Equal(t, tt.wantRanks, selection.Ranking(policy))
		})
	}
}

// TestCreatePooledHandler_RefusesWithoutARankingPolicy pins the invariant the pool
// relies on: it dereferences its policy on every query, so building one without a
// ranking policy has to fail loudly here rather than panic later. Unreachable in
// production — the caller gates on selection.Ranking — which is exactly why the
// guard needs a test of its own.
func TestCreatePooledHandler_RefusesWithoutARankingPolicy(t *testing.T) {
	zone := nsGroupsByDomain{
		domain: "example.com",
		groups: []*nbdns.NameServerGroup{{Domains: []string{"example.com"}}, {Domains: []string{"example.com"}}},
	}

	for _, policy := range []selection.Policy{nil, mustPolicy(t, selection.DefaultPolicy)} {
		server := &DefaultServer{selectionPolicy: policy}

		handlers, err := server.createPooledHandler(zone, PriorityUpstream)

		require.Error(t, err, "a %T policy must not yield a pool", policy)
		assert.Nil(t, handlers)
	}
}

func mustPolicy(t *testing.T, name string) selection.Policy {
	t.Helper()
	policy, ok := selection.Get(name)
	require.True(t, ok, "policy %q must exist", name)
	return policy
}

// TestNewDefaultServer_ThreadsTheSelectionPolicy pins the constructor seam: the
// configured name has to reach the server, otherwise the pool is never built in
// production no matter what the operator configures.
func TestNewDefaultServer_ThreadsTheSelectionPolicy(t *testing.T) {
	server, err := NewDefaultServer(
		context.Background(),
		&mocWGIface{},
		"",
		peer.NewRecorder("mgm"),
		nil,
		false,
		"prefer_private",
	)
	require.NoError(t, err)
	defer server.Stop()

	require.NotNil(t, server.selectionPolicy)
	assert.Equal(t, "prefer_private", server.selectionPolicy.Name())
	assert.True(t, selection.Ranking(server.selectionPolicy))
}
