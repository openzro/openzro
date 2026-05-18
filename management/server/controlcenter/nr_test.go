package controlcenter

import (
	"context"
	"net"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	resourceTypes "github.com/openzro/openzro/management/server/networks/resources/types"
	routerTypes "github.com/openzro/openzro/management/server/networks/routers/types"
	networkTypes "github.com/openzro/openzro/management/server/networks/types"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/posture"
	"github.com/openzro/openzro/management/server/types"
)

// C* / Finding 1: network-resource reach must come from the engine's
// real GetPeerNetworkResourceFirewallRules — the permitting policy,
// protocol and port are the rule's, never an arbitrary pols[0]. A
// resource the focus is excluded from only by posture surfaces as a
// distinct posture_blocked edge.

func nrAccount(includePolicyB bool, postureFails bool) *types.Account {
	pA := &nbpeer.Peer{ID: "pA", IP: net.IP{10, 0, 0, 1}, Key: "kA", Name: "alice",
		Meta: nbpeer.PeerSystemMeta{WtVersion: "99.0.0"}}
	if postureFails {
		pA.Meta.WtVersion = "1.0.0"
	}
	pR := &nbpeer.Peer{ID: "pR", IP: net.IP{10, 0, 0, 9}, Key: "kR", Name: "router"}

	policies := []*types.Policy{
		{ // policyA: source group gA — does NOT contain the focus.
			ID: "policyA", Enabled: true,
			Rules: []*types.PolicyRule{{
				ID: "rA", PolicyID: "policyA", Enabled: true,
				Sources:             []string{"gA"},
				DestinationResource: types.Resource{ID: "res1", Type: "Host"},
				Protocol:            types.PolicyRuleProtocolTCP, Ports: []string{"22"},
				Action: types.PolicyTrafficActionAccept,
			}},
		},
	}
	if includePolicyB {
		policies = append(policies, &types.Policy{ // focus permitted ONLY here
			ID: "policyB", Enabled: true,
			Rules: []*types.PolicyRule{{
				ID: "rB", PolicyID: "policyB", Enabled: true,
				Sources:             []string{"gB"},
				DestinationResource: types.Resource{ID: "res1", Type: "Host"},
				Protocol:            types.PolicyRuleProtocolTCP, Ports: []string{"443"},
				Action: types.PolicyTrafficActionAccept,
			}},
		})
	} else {
		policies = append(policies, &types.Policy{ // posture-gated only
			ID: "policyC", Enabled: true,
			SourcePostureChecks: []string{"pc1"},
			Rules: []*types.PolicyRule{{
				ID: "rC", PolicyID: "policyC", Enabled: true,
				Sources:             []string{"gB"},
				DestinationResource: types.Resource{ID: "res1", Type: "Host"},
				Protocol:            types.PolicyRuleProtocolTCP, Ports: []string{"8443"},
				Action: types.PolicyTrafficActionAccept,
			}},
		})
	}

	return &types.Account{
		Id:    "acc1",
		Peers: map[string]*nbpeer.Peer{"pA": pA, "pR": pR},
		Groups: map[string]*types.Group{
			"all": {ID: "all", Name: "All", Peers: []string{"pA", "pR"}},
			"gA":  {ID: "gA", Name: "gA", Peers: []string{}},
			"gB":  {ID: "gB", Name: "gB", Peers: []string{"pA"}},
		},
		Networks: []*networkTypes.Network{
			{ID: "net1", AccountID: "acc1", Name: "net1"},
		},
		NetworkRouters: []*routerTypes.NetworkRouter{
			{ID: "rt1", NetworkID: "net1", AccountID: "acc1", Peer: "pR", Enabled: true, Metric: 100},
		},
		NetworkResources: []*resourceTypes.NetworkResource{
			{ID: "res1", AccountID: "acc1", NetworkID: "net1",
				Address: "10.50.0.0/24", Prefix: netip.MustParsePrefix("10.50.0.0/24"),
				Type: resourceTypes.NetworkResourceType("subnet"), Enabled: true},
		},
		PostureChecks: []*posture.Checks{
			{ID: "pc1", Name: "min-nb", Checks: posture.ChecksDefinition{
				NBVersionCheck: &posture.NBVersionCheck{MinVersion: "99.0.0"}}},
		},
		Policies: policies,
	}
}

func nrEdge(g *GraphDTO) *Edge {
	for i := range g.Edges {
		if len(g.Edges[i].To) >= 3 && g.Edges[i].To[:3] == "nr:" {
			return &g.Edges[i]
		}
	}
	return nil
}

func TestBuildGraph_NetworkResource_CorrectPolicyNotPolsZero(t *testing.T) {
	acc := nrAccount(true, false) // policyA first, focus permitted only by policyB
	validated := map[string]struct{}{"pA": {}, "pR": {}}

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusPeer, ID: "pA"}, validated)
	require.NoError(t, err)

	e := nrEdge(g)
	require.NotNil(t, e, "focus is permitted on res1 via policyB")
	require.Equal(t, PermitPolicy, e.PermitSource)
	require.Equal(t, "policyB", e.PolicyID, "must be the permitting policy, NOT pols[0]=policyA")
	require.Equal(t, EdgeEnforced, e.State)
	require.Equal(t, []string{"443"}, e.Ports, "port from the real firewall rule, not hardcoded")
	require.NotEmpty(t, e.SourceRanges)
}

func TestBuildGraph_NetworkResource_PostureBlocked(t *testing.T) {
	acc := nrAccount(false, true) // only posture-gated policyC; focus fails posture
	validated := map[string]struct{}{"pA": {}, "pR": {}}

	g, err := BuildGraph(context.Background(), acc, Focus{Type: FocusPeer, ID: "pA"}, validated)
	require.NoError(t, err)

	e := nrEdge(g)
	require.NotNil(t, e, "posture-blocked resource must still surface as an edge")
	require.Equal(t, EdgePostureBlocked, e.State)
	require.Equal(t, "policyC", e.PolicyID)
	require.Equal(t, "min-nb", e.Meta["postureCheck"])
}
