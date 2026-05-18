package controlcenter

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

// C8: the GraphDTO wire shape is the contract the Phase 2 dashboard
// codes against. This codebase deliberately omits admin endpoints
// from openapi.yml (flow_exports / network_events / events are all
// registered in code but absent from the spec + its generated
// types.gen.go), so — consistent with that precedent and to avoid
// regenerating a 2140-line generated file — the Control Center
// endpoint is likewise spec-omitted and its contract is pinned HERE.
//
// Renaming or dropping a JSON field, or changing omitempty on the
// optional policy/route fields, breaks this test loudly. Keep it in
// lockstep with docs/adr/0017 and the dashboard client.
func keysOf(t *testing.T, v any) []string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &m))
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func TestWireContract_GraphDTO(t *testing.T) {
	require.Equal(t, []string{"edges", "focus", "nodes"},
		keysOf(t, GraphDTO{}))

	require.Equal(t, []string{"id", "type"},
		keysOf(t, Focus{Type: FocusPeer, ID: "p1"}))

	// Node: meta is omitempty (absent when nil).
	require.Equal(t, []string{"id", "kind", "label"},
		keysOf(t, Node{ID: "p1", Kind: NodeFocus, Label: "a"}))
	require.Equal(t, []string{"id", "kind", "label", "meta"},
		keysOf(t, Node{ID: "p1", Kind: NodeFocus, Label: "a", Meta: map[string]string{"k": "v"}}))

	// Edge: a route_default_permit edge must NOT carry policy chip
	// fields and may omit ports/sourceRanges/meta.
	require.Equal(t, []string{"direction", "from", "permitSource", "protocol", "state", "to"},
		keysOf(t, Edge{
			From: "f", To: "t", PermitSource: PermitRouteDefault,
			Protocol: "all", Direction: DirectionOut, State: EdgeEnforced,
		}))

	// Edge: a fully-populated policy edge carries the full field set.
	require.Equal(t,
		[]string{"direction", "from", "meta", "permitSource", "policyId", "policyName", "ports", "protocol", "sourceRanges", "state", "to"},
		keysOf(t, Edge{
			From: "f", To: "t", PermitSource: PermitPolicy,
			PolicyID: "pol1", PolicyName: "p", Protocol: "tcp",
			Ports: []string{"22"}, SourceRanges: []string{"0.0.0.0/0"},
			Direction: DirectionBidirectional, State: EdgePostureBlocked,
			Meta: map[string]string{"postureCheck": "min-nb"},
		}))
}
