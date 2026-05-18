package controlcenter

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/posture"
	"github.com/openzro/openzro/management/server/types"
)

// C3 / ADR-0017 D1.2: a policy-permitted pair the engine drops *only*
// because of posture must surface as a distinct posture_blocked edge
// (named by the failing check), never collapsed into "no edge".

func postureAccount(focusWtVersion string) *types.Account {
	pA := &nbpeer.Peer{ID: "pA", IP: net.IP{10, 0, 0, 1}, Name: "alice",
		Meta: nbpeer.PeerSystemMeta{WtVersion: focusWtVersion}}
	pB := &nbpeer.Peer{ID: "pB", IP: net.IP{10, 0, 0, 2}, Name: "bob",
		Meta: nbpeer.PeerSystemMeta{WtVersion: "99.0.0"}}
	return &types.Account{
		Id:    "acc1",
		Peers: map[string]*nbpeer.Peer{"pA": pA, "pB": pB},
		Groups: map[string]*types.Group{
			"all": {ID: "all", Name: "All", Peers: []string{"pA", "pB"}},
			"g1":  {ID: "g1", Name: "src", Peers: []string{"pA"}},
			"g2":  {ID: "g2", Name: "dst", Peers: []string{"pB"}},
		},
		PostureChecks: []*posture.Checks{
			{ID: "pc1", Name: "min-nb", Checks: posture.ChecksDefinition{
				NBVersionCheck: &posture.NBVersionCheck{MinVersion: "99.0.0"}}},
		},
		Policies: []*types.Policy{
			{
				ID: "pol1", Name: "allow-ssh", Enabled: true,
				SourcePostureChecks: []string{"pc1"},
				Rules: []*types.PolicyRule{
					{
						ID: "r1", Enabled: true,
						Action:       types.PolicyTrafficActionAccept,
						Sources:      []string{"g1"},
						Destinations: []string{"g2"},
						Protocol:     types.PolicyRuleProtocolTCP,
						Ports:        []string{"22"},
					},
				},
			},
		},
	}
}

func TestBuildGraph_PostureBlocked_FocusNonCompliant(t *testing.T) {
	acc := postureAccount("1.0.0") // pA fails the min-nb check
	validated := map[string]struct{}{"pA": {}, "pB": {}}

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusPeer, ID: "pA"}, validated)
	require.NoError(t, err)

	require.Len(t, g.Edges, 1)
	e := g.Edges[0]
	require.Equal(t, "pA", e.From)
	require.Equal(t, "pB", e.To)
	require.Equal(t, EdgePostureBlocked, e.State)
	require.Equal(t, PermitPolicy, e.PermitSource)
	require.Equal(t, "pol1", e.PolicyID)
	require.Equal(t, "tcp", e.Protocol)
	require.Equal(t, []string{"22"}, e.Ports)
	require.Equal(t, "min-nb", e.Meta["postureCheck"])
	require.Equal(t, "pc1", e.Meta["postureCheckId"])
	// pB must still be a node so the blocked edge has both endpoints.
	require.Contains(t, g.Nodes, Node{ID: "pB", Kind: NodePeer, Label: "bob"})
}

func TestBuildGraph_PostureCompliant_EnforcedNotBlocked(t *testing.T) {
	acc := postureAccount("99.0.0") // pA passes
	validated := map[string]struct{}{"pA": {}, "pB": {}}

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusPeer, ID: "pA"}, validated)
	require.NoError(t, err)
	require.Len(t, g.Edges, 1)
	require.Equal(t, EdgeEnforced, g.Edges[0].State)
}

func TestBuildGraph_PostureBlocked_FocusIsDestination(t *testing.T) {
	acc := postureAccount("1.0.0") // pA (a source) fails posture
	// flip: focus is pB (a destination); pA is a source that fails posture.
	validated := map[string]struct{}{"pA": {}, "pB": {}}

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusPeer, ID: "pB"}, validated)
	require.NoError(t, err)
	require.Len(t, g.Edges, 1)
	e := g.Edges[0]
	// focus-anchored convention (same as the enforced pass): From is
	// always the focus; Direction encodes that pA would reach pB (IN).
	require.Equal(t, "pB", e.From)
	require.Equal(t, "pA", e.To)
	require.Equal(t, EdgePostureBlocked, e.State)
	require.Equal(t, DirectionIn, e.Direction)
}

// Finding 3: if the OTHER endpoint is not validated, the pair is
// unreachable regardless of posture — posture is not the sole
// remaining blocker, so no posture_blocked edge may be emitted.
func TestBuildGraph_PostureBlocked_OtherEndpointUnvalidated(t *testing.T) {
	acc := postureAccount("1.0.0") // pA (source) fails posture
	// focus pB (destination) is NOT validated; only pA is.
	validated := map[string]struct{}{"pA": {}}

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusPeer, ID: "pB"}, validated)
	require.NoError(t, err)
	for _, e := range g.Edges {
		require.NotEqual(t, EdgePostureBlocked, e.State,
			"no posture_blocked when the other endpoint is unvalidated")
	}
}
