package controlcenter

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/types"
)

// C2: peer↔peer reach. A focus peer's graph must contain the peers it
// actually reaches (GetPeerConnectionResources) with the permitting
// policy chip, protocol/ports and direction on the edge. No policy =>
// no edge (absence is a distinct audit answer, never a fake node).

func twoPeerAccount(policyEnabled, ruleEnabled bool) *types.Account {
	pA := &nbpeer.Peer{ID: "pA", IP: net.IP{10, 0, 0, 1}, Name: "alice"}
	pB := &nbpeer.Peer{ID: "pB", IP: net.IP{10, 0, 0, 2}, Name: "bob"}
	return &types.Account{
		Id:    "acc1",
		Peers: map[string]*nbpeer.Peer{"pA": pA, "pB": pB},
		Groups: map[string]*types.Group{
			"all": {ID: "all", Name: "All", Peers: []string{"pA", "pB"}},
			"g1":  {ID: "g1", Name: "src", Peers: []string{"pA"}},
			"g2":  {ID: "g2", Name: "dst", Peers: []string{"pB"}},
		},
		Policies: []*types.Policy{
			{
				ID: "pol1", Name: "allow-ssh", Enabled: policyEnabled,
				Rules: []*types.PolicyRule{
					{
						ID: "r1", Enabled: ruleEnabled,
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

func TestBuildGraph_PeerToPeer_Enforced(t *testing.T) {
	acc := twoPeerAccount(true, true)
	validated := map[string]struct{}{"pA": {}, "pB": {}}

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusPeer, ID: "pA"}, validated)
	require.NoError(t, err)
	require.Equal(t, Focus{Type: FocusPeer, ID: "pA"}, g.Focus)

	// focus node + the one reachable peer node.
	require.Contains(t, g.Nodes, Node{ID: "pA", Kind: NodeFocus, Label: "alice"})
	require.Contains(t, g.Nodes, Node{ID: "pB", Kind: NodePeer, Label: "bob"})

	require.Len(t, g.Edges, 1)
	e := g.Edges[0]
	require.Equal(t, "pA", e.From)
	require.Equal(t, "pB", e.To)
	require.Equal(t, PermitPolicy, e.PermitSource)
	require.Equal(t, "pol1", e.PolicyID)
	require.Equal(t, "allow-ssh", e.PolicyName)
	require.Equal(t, "tcp", e.Protocol)
	require.Equal(t, []string{"22"}, e.Ports)
	require.Equal(t, EdgeEnforced, e.State)
	require.Equal(t, DirectionOut, e.Direction)
}

func TestBuildGraph_NoPolicy_NoEdge(t *testing.T) {
	acc := twoPeerAccount(false, true) // policy disabled
	validated := map[string]struct{}{"pA": {}, "pB": {}}

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusPeer, ID: "pA"}, validated)
	require.NoError(t, err)
	require.Empty(t, g.Edges)
	// only the focus node; no reachable peer => no peer node.
	require.Equal(t, []Node{{ID: "pA", Kind: NodeFocus, Label: "alice"}}, g.Nodes)
}

func TestBuildGraph_UnknownFocusPeer(t *testing.T) {
	acc := twoPeerAccount(true, true)
	_, err := BuildGraph(context.Background(), acc, Focus{Type: FocusPeer, ID: "ghost"}, nil)
	require.Error(t, err)
}
