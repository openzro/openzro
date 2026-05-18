package controlcenter

import (
	"context"
	"net"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/types"
	"github.com/openzro/openzro/route"
)

// C4 / ADR-0017 D1.1: a route edge is the COMPOSITION of distribution
// (GetRoutesToSync) and permission. A route synced to the focus but
// not permitted is NOT drawn reachable (the route_test.go:1934
// invariant). No AccessControlGroups => route_default_permit, no chip.

func routeAccount() *types.Account {
	pA := &nbpeer.Peer{ID: "pA", IP: net.IP{10, 0, 0, 1}, Key: "kA", Name: "alice"}
	pR := &nbpeer.Peer{ID: "pR", IP: net.IP{10, 0, 0, 9}, Key: "kR", Name: "router"}
	mk := func(id, cidr string, acg []string) *route.Route {
		return &route.Route{
			ID:                  route.ID(id),
			Network:             netip.MustParsePrefix(cidr),
			Peer:                "pR",
			Groups:              []string{"gClients"},
			Enabled:             true,
			AccessControlGroups: acg,
		}
	}
	return &types.Account{
		Id:    "acc1",
		Peers: map[string]*nbpeer.Peer{"pA": pA, "pR": pR},
		Groups: map[string]*types.Group{
			"all":      {ID: "all", Name: "All", Peers: []string{"pA", "pR"}},
			"gClients": {ID: "gClients", Name: "clients", Peers: []string{"pA"}},
			"gRouter":  {ID: "gRouter", Name: "routers", Peers: []string{"pR"}},
			"gACL2":    {ID: "gACL2", Name: "acl2", Peers: []string{}},
			"gACL3":    {ID: "gACL3", Name: "acl3", Peers: []string{}},
		},
		Routes: map[route.ID]*route.Route{
			"r1": mk("r1", "10.10.0.0/24", nil),               // default permit
			"r2": mk("r2", "10.20.0.0/24", []string{"gACL2"}), // permitted via P2
			"r3": mk("r3", "10.30.0.0/24", []string{"gACL3"}), // distributed, NOT permitted
		},
		Policies: []*types.Policy{
			{
				ID: "polPeer", Name: "clients-to-router", Enabled: true,
				Rules: []*types.PolicyRule{{
					ID: "rp", PolicyID: "polPeer", Enabled: true, Action: types.PolicyTrafficActionAccept,
					Sources: []string{"gClients"}, Destinations: []string{"gRouter"},
					Protocol: types.PolicyRuleProtocolALL,
				}},
			},
			{
				// Same policy + protocol, TWO rules (ports 80 and 443):
				// the real firewall emits two RouteFirewallRules; both
				// ports must survive into one merged edge (Finding 2).
				ID: "P2", Name: "route2-access", Enabled: true,
				Rules: []*types.PolicyRule{
					{
						ID: "r2a", PolicyID: "P2", Enabled: true, Action: types.PolicyTrafficActionAccept,
						Sources: []string{"gClients"}, Destinations: []string{"gACL2"},
						Protocol: types.PolicyRuleProtocolTCP, Ports: []string{"80"},
					},
					{
						ID: "r2b", PolicyID: "P2", Enabled: true, Action: types.PolicyTrafficActionAccept,
						Sources: []string{"gClients"}, Destinations: []string{"gACL2"},
						Protocol: types.PolicyRuleProtocolTCP, Ports: []string{"443"},
					},
				},
			},
		},
	}
}

func edgeTo(g *GraphDTO, to string) *Edge {
	for i := range g.Edges {
		if g.Edges[i].To == to {
			return &g.Edges[i]
		}
	}
	return nil
}

func TestBuildGraph_RouteReach_CompositionAndDefaultPermit(t *testing.T) {
	acc := routeAccount()
	validated := map[string]struct{}{"pA": {}, "pR": {}}

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusPeer, ID: "pA"}, validated)
	require.NoError(t, err)

	// r1: no AccessControlGroups => route_default_permit, no policy chip.
	e1 := edgeTo(g, "route:r1")
	require.NotNil(t, e1, "default-permit route must be reachable")
	require.Equal(t, PermitRouteDefault, e1.PermitSource)
	require.Empty(t, e1.PolicyID)
	require.Equal(t, EdgeEnforced, e1.State)
	require.Equal(t, DirectionOut, e1.Direction)
	require.Contains(t, g.Nodes, Node{ID: "route:r1", Kind: NodeRoute, Label: "10.10.0.0/24"})

	// r2: permitted via policy P2, built from the REAL route firewall
	// rules — both ports (80 and 443) must survive into one edge, and
	// the effective SourceRanges must be populated (Finding 2).
	e2 := edgeTo(g, "route:r2")
	require.NotNil(t, e2, "policy-permitted route must be reachable")
	require.Equal(t, PermitPolicy, e2.PermitSource)
	require.Equal(t, "P2", e2.PolicyID)
	require.Equal(t, "route2-access", e2.PolicyName)
	require.Equal(t, EdgeEnforced, e2.State)
	require.ElementsMatch(t, []string{"80", "443"}, e2.Ports,
		"both rules' ports must merge, not be dropped by dedup")
	require.NotEmpty(t, e2.SourceRanges, "effective SourceRanges must come from the firewall rule")

	// r3: distributed to pA but NO permitting policy => NOT reachable.
	require.Nil(t, edgeTo(g, "route:r3"), "synced-but-unpermitted route must NOT be an edge")
	require.NotContains(t, nodeIDs(g), "route:r3", "unpermitted route must not be an orphan node")
}

func nodeIDs(g *GraphDTO) []string {
	ids := make([]string, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		ids = append(ids, n.ID)
	}
	return ids
}

// #50-r2 semantic note: when the focus IS the router serving a route
// (even one with AccessControlGroups), the edge is router_local —
// honest infrastructure-local reach, NOT route_default_permit.
func TestBuildGraph_SelfRouter_RouterLocalPermitSource(t *testing.T) {
	acc := routeAccount() // pR serves r1 (no ACG) and r2 (ACG gACL2)
	validated := map[string]struct{}{"pA": {}, "pR": {}}

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusPeer, ID: "pR"}, validated)
	require.NoError(t, err)

	e2 := edgeTo(g, "route:r2") // r2 has AccessControlGroups
	require.NotNil(t, e2, "the router reaches the network it serves")
	require.Equal(t, PermitRouterLocal, e2.PermitSource,
		"self-router reach must be router_local, not route_default_permit")
	require.Empty(t, e2.PolicyID)
}

// #50-r3: a group holding a router AND a client over the same
// default-permit route must NOT collapse — router_local and
// route_default_permit are distinct reach causes and each keeps its
// own "k of n".
func TestBuildGraph_GroupFocus_RouterLocalVsDefaultNotCollapsed(t *testing.T) {
	acc := routeAccount() // group "all" = {pA client, pR router}; pR serves r1 (no ACG)
	validated := map[string]struct{}{"pA": {}, "pR": {}}

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusGroup, ID: "all"}, validated)
	require.NoError(t, err)

	var r1 []Edge
	for _, e := range g.Edges {
		if e.To == "route:r1" {
			r1 = append(r1, e)
		}
	}
	require.Len(t, r1, 2, "router_local and route_default_permit must stay separate")

	bySrc := map[PermitSource]Edge{}
	for _, e := range r1 {
		bySrc[e.PermitSource] = e
	}
	require.Contains(t, bySrc, PermitRouterLocal)
	require.Contains(t, bySrc, PermitRouteDefault)
	require.Equal(t, "1 of 2 members", bySrc[PermitRouterLocal].Meta["reachedBy"])
	require.Equal(t, "1 of 2 members", bySrc[PermitRouteDefault].Meta["reachedBy"])
}
