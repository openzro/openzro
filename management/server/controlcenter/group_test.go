package controlcenter

import (
	"context"
	"encoding/json"
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

	// #54 review: the group-focus aggregation must preserve the
	// per-member node meta, so target peer nodes keep their IP.
	var pX *Node
	for i := range g.Nodes {
		if g.Nodes[i].ID == "pX" {
			pX = &g.Nodes[i]
		}
	}
	require.NotNil(t, pX)
	require.Equal(t, "10.0.0.9", pX.Meta["ip"],
		"group focus must not strip peer meta.ip")
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

// Finding 4: two members blocked by DIFFERENT posture checks for the
// same target/policy/protocol must NOT collapse into one aggregated
// edge with an arbitrary member's reason.
func twoCheckGroupAccount() *types.Account {
	mk := func(id string, last byte, ver string) *nbpeer.Peer {
		return &nbpeer.Peer{ID: id, IP: net.IP{10, 0, 0, last}, Name: id,
			Meta: nbpeer.PeerSystemMeta{WtVersion: ver}}
	}
	return &types.Account{
		Id: "acc1",
		Peers: map[string]*nbpeer.Peer{
			"pA": mk("pA", 1, "10.0.0"), // fails pcA (min 20)
			"pB": mk("pB", 2, "30.0.0"), // passes pcA, fails pcB (min 40)
			"pC": mk("pC", 3, "99.0.0"), // passes both -> enforced
			"pX": mk("pX", 9, "99.0.0"),
		},
		Groups: map[string]*types.Group{
			"all":  {ID: "all", Name: "All", Peers: []string{"pA", "pB", "pC", "pX"}},
			"team": {ID: "team", Name: "team", Peers: []string{"pA", "pB", "pC"}},
			"gX":   {ID: "gX", Name: "gX", Peers: []string{"pX"}},
		},
		PostureChecks: []*posture.Checks{
			{ID: "pcA", Name: "min-20", Checks: posture.ChecksDefinition{
				NBVersionCheck: &posture.NBVersionCheck{MinVersion: "20.0.0"}}},
			{ID: "pcB", Name: "min-40", Checks: posture.ChecksDefinition{
				NBVersionCheck: &posture.NBVersionCheck{MinVersion: "40.0.0"}}},
		},
		Policies: []*types.Policy{
			{
				ID: "P", Name: "team-to-x", Enabled: true,
				SourcePostureChecks: []string{"pcA", "pcB"},
				Rules: []*types.PolicyRule{{
					ID: "r", Enabled: true, Action: types.PolicyTrafficActionAccept,
					Sources: []string{"team"}, Destinations: []string{"gX"},
					Protocol: types.PolicyRuleProtocolTCP, Ports: []string{"443"},
				}},
			},
		},
	}
}

func TestBuildGraph_GroupFocus_DistinctPostureCausesDoNotCollapse(t *testing.T) {
	acc := twoCheckGroupAccount()
	validated := map[string]struct{}{"pA": {}, "pB": {}, "pC": {}, "pX": {}}

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusGroup, ID: "team"}, validated)
	require.NoError(t, err)

	var blocked []Edge
	for _, e := range g.Edges {
		if e.To == "pX" && e.State == EdgePostureBlocked {
			blocked = append(blocked, e)
		}
	}
	require.Len(t, blocked, 2, "different posture checks must yield distinct edges")

	byCheck := map[string]Edge{}
	for _, e := range blocked {
		byCheck[e.Meta["postureCheckId"]] = e
	}
	require.Contains(t, byCheck, "pcA")
	require.Contains(t, byCheck, "pcB")
	require.Equal(t, "1 of 3 members", byCheck["pcA"].Meta["reachedBy"])
	require.Equal(t, "1 of 3 members", byCheck["pcB"].Meta["reachedBy"])

	// pC (compliant) still produces a distinct enforced edge.
	var enforced int
	for _, e := range g.Edges {
		if e.To == "pX" && e.State == EdgeEnforced {
			enforced++
		}
	}
	require.Equal(t, 1, enforced)
}

// #50-r2 F1: an unvalidated group member contributes zero reach but
// still counts in the "k of n" denominator.
func TestBuildGraph_GroupFocus_UnvalidatedMemberCountsInDenominator(t *testing.T) {
	acc := groupFocusAccount("99.0.0")                             // all compliant
	validated := map[string]struct{}{"pA": {}, "pC": {}, "pX": {}} // pB NOT validated

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusGroup, ID: "team"}, validated)
	require.NoError(t, err)
	es := edgesByState(g, "pX")
	e, ok := es[EdgeEnforced]
	require.True(t, ok)
	require.Equal(t, "2 of 3 members", e.Meta["reachedBy"],
		"unvalidated member excluded from k, still in n")
}

// #50-r2 F4: the DTO must be byte-stable across builds even when
// multiple posture causes produce edges that tie on to/policy/state.
func TestBuildGraph_GroupFocus_DeterministicOrdering(t *testing.T) {
	validated := map[string]struct{}{"pA": {}, "pB": {}, "pC": {}, "pX": {}}
	var first string
	for i := 0; i < 12; i++ {
		acc := twoCheckGroupAccount()
		g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusGroup, ID: "team"}, validated)
		require.NoError(t, err)
		j, err := json.Marshal(g)
		require.NoError(t, err)
		if i == 0 {
			first = string(j)
			continue
		}
		require.Equal(t, first, string(j), "DTO must be deterministic across builds")
	}
}
