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

// C5 / ADR-0017 D3: group focus is the UNION of per-member reach
// (never the intersection — that would hide real access), each
// aggregated edge tagged "k of n members", posture evaluated per
// member so a posture-blocked member contributes a distinct
// posture_blocked edge instead of being dropped from the union.

func groupFocusAccount(pBVersion string) *types.Account {
	mkPeer := func(id string, last byte, ver string) *nbpeer.Peer {
		return &nbpeer.Peer{ID: id, IP: net.IP{10, 0, 0, last}, Name: id,
			Meta: nbpeer.PeerSystemMeta{WtVersion: ver}}
	}
	return &types.Account{
		Id: "acc1",
		Peers: map[string]*nbpeer.Peer{
			"pA": mkPeer("pA", 1, "99.0.0"),
			"pB": mkPeer("pB", 2, pBVersion),
			"pC": mkPeer("pC", 3, "99.0.0"),
			"pX": mkPeer("pX", 9, "99.0.0"),
		},
		Groups: map[string]*types.Group{
			"all":  {ID: "all", Name: "All", Peers: []string{"pA", "pB", "pC", "pX"}},
			"team": {ID: "team", Name: "team", Peers: []string{"pA", "pB", "pC"}},
			"gX":   {ID: "gX", Name: "gX", Peers: []string{"pX"}},
		},
		PostureChecks: []*posture.Checks{
			{ID: "pc1", Name: "min-nb", Checks: posture.ChecksDefinition{
				NBVersionCheck: &posture.NBVersionCheck{MinVersion: "99.0.0"}}},
		},
		Policies: []*types.Policy{
			{
				ID: "P", Name: "team-to-x", Enabled: true,
				SourcePostureChecks: []string{"pc1"},
				Rules: []*types.PolicyRule{{
					ID: "r", Enabled: true, Action: types.PolicyTrafficActionAccept,
					Sources: []string{"team"}, Destinations: []string{"gX"},
					Protocol: types.PolicyRuleProtocolTCP, Ports: []string{"443"},
				}},
			},
		},
	}
}

func edgesByState(g *GraphDTO, to string) map[EdgeState]Edge {
	m := map[EdgeState]Edge{}
	for _, e := range g.Edges {
		if e.To == to {
			m[e.State] = e
		}
	}
	return m
}

func TestBuildGraph_GroupFocus_UnionAllCompliant(t *testing.T) {
	acc := groupFocusAccount("99.0.0") // pB passes too
	validated := map[string]struct{}{"pA": {}, "pB": {}, "pC": {}, "pX": {}}

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusGroup, ID: "team"}, validated)
	require.NoError(t, err)
	require.Equal(t, Focus{Type: FocusGroup, ID: "team"}, g.Focus)
	require.Contains(t, g.Nodes, Node{ID: "group:team", Kind: NodeFocus, Label: "team"})

	es := edgesByState(g, "pX")
	e, ok := es[EdgeEnforced]
	require.True(t, ok, "all 3 members reach pX")
	require.Equal(t, "group:team", e.From)
	require.Equal(t, "3 of 3 members", e.Meta["reachedBy"])
}

func TestBuildGraph_GroupFocus_PostureSplitsUnion(t *testing.T) {
	acc := groupFocusAccount("1.0.0") // pB fails the posture check
	validated := map[string]struct{}{"pA": {}, "pB": {}, "pC": {}, "pX": {}}

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusGroup, ID: "team"}, validated)
	require.NoError(t, err)

	es := edgesByState(g, "pX")
	enf, ok := es[EdgeEnforced]
	require.True(t, ok)
	require.Equal(t, "2 of 3 members", enf.Meta["reachedBy"])

	blk, ok := es[EdgePostureBlocked]
	require.True(t, ok, "posture-blocked member contributes a distinct edge, not dropped")
	require.Equal(t, "1 of 3 members", blk.Meta["reachedBy"])
	require.Equal(t, "min-nb", blk.Meta["postureCheck"])
}

func TestBuildGraph_UnknownFocusGroup(t *testing.T) {
	acc := groupFocusAccount("99.0.0")
	_, err := BuildGraph(context.Background(), acc, Focus{Type: FocusGroup, ID: "ghost"}, nil)
	require.Error(t, err)
}
