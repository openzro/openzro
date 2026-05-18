package controlcenter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// C1: the DTO envelope is the contract Phase 2 (dashboard) and the
// HTTP layer depend on. Pin the enum wire values and the omitempty
// behaviour so a later refactor can't silently change the JSON shape.

func TestEnumWireValues(t *testing.T) {
	cases := []struct {
		got  string
		want string
	}{
		{string(FocusPeer), "peer"},
		{string(FocusUser), "user"},
		{string(FocusGroup), "group"},
		{string(FocusNetwork), "network"},
		{string(NodeFocus), "focus"},
		{string(NodePolicy), "policy"},
		{string(NodeGroup), "group"},
		{string(NodePeer), "peer"},
		{string(NodeUser), "user"},
		{string(NodeRoute), "route"},
		{string(NodeNetworkResource), "network_resource"},
		{string(NodeNetwork), "network"},
		{string(EdgeEnforced), "enforced"},
		{string(EdgePostureBlocked), "posture_blocked"},
		{string(PermitPolicy), "policy"},
		{string(PermitRouteDefault), "route_default_permit"},
		{string(PermitRouterLocal), "router_local"},
		{string(DirectionIn), "in"},
		{string(DirectionOut), "out"},
		{string(DirectionBidirectional), "bidirectional"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("enum wire value = %q, want %q", c.got, c.want)
		}
	}
}

func TestGraphDTO_JSONRoundTrip(t *testing.T) {
	in := GraphDTO{
		Focus: Focus{Type: FocusPeer, ID: "peer-1"},
		Nodes: []Node{
			{ID: "peer-1", Kind: NodeFocus, Label: "alice"},
			{ID: "peer-2", Kind: NodePeer, Label: "bob", Meta: map[string]string{"reachedBy": "3 of 5 members"}},
		},
		Edges: []Edge{
			{
				From: "peer-1", To: "peer-2",
				PermitSource: PermitPolicy,
				PolicyID:     "pol-1", PolicyName: "allow-ssh",
				Protocol: "tcp", Ports: []string{"22", "1000-2000"},
				Direction: DirectionBidirectional, State: EdgeEnforced,
			},
			{
				From: "peer-1", To: "route-1",
				PermitSource: PermitRouteDefault,
				Protocol:     "all", SourceRanges: []string{"0.0.0.0/0"},
				Direction: DirectionIn, State: EdgeEnforced,
			},
		},
	}

	b, err := json.Marshal(in)
	require.NoError(t, err)

	// route_default_permit edge must not emit a policy chip.
	require.NotContains(t, string(b), `"policyId":""`)
	require.NotContains(t, string(b), `"policyName":""`)

	var out GraphDTO
	require.NoError(t, json.Unmarshal(b, &out))
	require.Equal(t, in, out)
}
