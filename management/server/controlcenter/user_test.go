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

// CC v2 T1.2: user-centric columnar projection
// User → Peers → Policies → Resources, with per-edge enforcement
// status (green=enforced, red=posture_blocked) and meta.column.

func userFocusAccount(uVersion string) *types.Account {
	pA := &nbpeer.Peer{ID: "pA", IP: net.IP{10, 0, 0, 1}, Name: "alice-laptop",
		UserID: "u1", Meta: nbpeer.PeerSystemMeta{WtVersion: uVersion}}
	pX := &nbpeer.Peer{ID: "pX", IP: net.IP{10, 0, 0, 9}, Name: "db", UserID: "svc"}
	return &types.Account{
		Id: "acc1",
		Users: map[string]*types.User{
			"u1": {Id: "u1", AccountID: "acc1", Email: "alice@corp.io", Name: "Alice"},
		},
		Peers: map[string]*nbpeer.Peer{"pA": pA, "pX": pX},
		Groups: map[string]*types.Group{
			"gSrc": {ID: "gSrc", Name: "src", Peers: []string{"pA"}},
			"gDst": {ID: "gDst", Name: "dst", Peers: []string{"pX"}},
		},
		PostureChecks: []*posture.Checks{
			{ID: "pc1", Name: "min-nb", Checks: posture.ChecksDefinition{
				NBVersionCheck: &posture.NBVersionCheck{MinVersion: "99.0.0"}}},
		},
		Policies: []*types.Policy{
			{
				ID: "P", Name: "alice-to-db", Enabled: true,
				SourcePostureChecks: []string{"pc1"},
				Rules: []*types.PolicyRule{{
					ID: "r", Enabled: true, Action: types.PolicyTrafficActionAccept,
					Sources: []string{"gSrc"}, Destinations: []string{"gDst"},
					Protocol: types.PolicyRuleProtocolTCP, Ports: []string{"443"},
				}},
			},
		},
	}
}

func nodeByID(g *GraphDTO, id string) *Node {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i]
		}
	}
	return nil
}

func edgeFromTo(g *GraphDTO, from, to string) *Edge {
	for i := range g.Edges {
		if g.Edges[i].From == from && g.Edges[i].To == to {
			return &g.Edges[i]
		}
	}
	return nil
}

func TestBuildUserFocus_ColumnarProjection_Enforced(t *testing.T) {
	acc := userFocusAccount("99.0.0") // pA passes posture
	validated := map[string]struct{}{"pA": {}, "pX": {}}

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusUser, ID: "u1"}, validated)
	require.NoError(t, err)
	require.Equal(t, Focus{Type: FocusUser, ID: "u1"}, g.Focus)

	u := nodeByID(g, "u1")
	require.NotNil(t, u)
	require.Equal(t, NodeUser, u.Kind)
	require.Equal(t, "Alice", u.Label)
	require.Equal(t, "alice@corp.io", u.Meta["email"])
	require.Equal(t, "user", u.Meta["column"])

	pa := nodeByID(g, "pA")
	require.NotNil(t, pa)
	require.Equal(t, "peers", pa.Meta["column"])
	require.Equal(t, "10.0.0.1", pa.Meta["ip"])

	pol := nodeByID(g, "policy:P")
	require.NotNil(t, pol)
	require.Equal(t, NodePolicy, pol.Kind)
	require.Equal(t, "policies", pol.Meta["column"])
	require.Equal(t, "TCP:443", pol.Meta["port"])

	res := nodeByID(g, "pX")
	require.NotNil(t, res)
	require.Equal(t, "resources", res.Meta["column"])

	// User→Peer ownership (structural), Peer→Policy enforced,
	// Policy→Resource enforced.
	require.NotNil(t, edgeFromTo(g, "u1", "pA"))
	pp := edgeFromTo(g, "pA", "policy:P")
	require.NotNil(t, pp)
	require.Equal(t, EdgeEnforced, pp.State)
	require.Equal(t, "P", pp.PolicyID)
	pr := edgeFromTo(g, "policy:P", "pX")
	require.NotNil(t, pr)
	require.Equal(t, EdgeEnforced, pr.State)
}

func TestBuildUserFocus_PeerToPolicy_PostureBlocked(t *testing.T) {
	acc := userFocusAccount("1.0.0") // pA fails the min-nb posture check
	validated := map[string]struct{}{"pA": {}, "pX": {}}

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusUser, ID: "u1"}, validated)
	require.NoError(t, err)

	pp := edgeFromTo(g, "pA", "policy:P")
	require.NotNil(t, pp)
	require.Equal(t, EdgePostureBlocked, pp.State, "peer→policy must reflect posture")
	require.Equal(t, "min-nb", pp.Meta["postureCheck"])
}

func TestBuildUserFocus_UnknownUser(t *testing.T) {
	acc := userFocusAccount("99.0.0")
	_, err := BuildGraph(context.Background(), acc, Focus{Type: FocusUser, ID: "ghost"}, nil)
	require.Error(t, err)
}
