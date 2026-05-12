package flow_policy_resolver

import (
	"net"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/flow/store"
	resourceTypes "github.com/openzro/openzro/management/server/networks/resources/types"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/types"
)

// fixtureAccount builds a small account graph used across cases:
//
//	peer-a (10.0.0.1) ∈ group-laptops
//	peer-b (10.0.0.2) ∈ group-laptops
//	peer-c (10.0.0.3) ∈ group-servers
//
//	policy-allow-laptops-to-servers (rule: laptops → servers, TCP/443)
//	policy-allow-any-icmp           (rule: laptops → servers, ICMP)
//
// Tests extend / mutate this fixture as needed.
func fixtureAccount() *types.Account {
	peerA := &nbpeer.Peer{ID: "peer-a", IP: net.ParseIP("10.0.0.1")}
	peerB := &nbpeer.Peer{ID: "peer-b", IP: net.ParseIP("10.0.0.2")}
	peerC := &nbpeer.Peer{ID: "peer-c", IP: net.ParseIP("10.0.0.3")}

	groupLaptops := &types.Group{ID: "group-laptops", Peers: []string{"peer-a", "peer-b"}}
	groupServers := &types.Group{ID: "group-servers", Peers: []string{"peer-c"}}

	allowTCP := &types.Policy{
		ID:      "p-allow-tcp-2026-05-01",
		Enabled: true,
		Rules: []*types.PolicyRule{{
			ID:           "r-allow-tcp",
			Enabled:      true,
			Action:       types.PolicyTrafficActionAccept,
			Sources:      []string{"group-laptops"},
			Destinations: []string{"group-servers"},
			Protocol:     types.PolicyRuleProtocolTCP,
			Ports:        []string{"443"},
		}},
	}

	allowICMP := &types.Policy{
		ID:      "p-allow-icmp-2026-05-02",
		Enabled: true,
		Rules: []*types.PolicyRule{{
			ID:           "r-allow-icmp",
			Enabled:      true,
			Action:       types.PolicyTrafficActionAccept,
			Sources:      []string{"group-laptops"},
			Destinations: []string{"group-servers"},
			Protocol:     types.PolicyRuleProtocolICMP,
		}},
	}

	return &types.Account{
		Peers: map[string]*nbpeer.Peer{
			"peer-a": peerA, "peer-b": peerB, "peer-c": peerC,
		},
		Groups: map[string]*types.Group{
			"group-laptops": groupLaptops, "group-servers": groupServers,
		},
		Policies: []*types.Policy{allowTCP, allowICMP},
	}
}

func newEvent(peerID, dstIP string, dstPort uint32, proto uint16) *store.Event {
	return &store.Event{
		PeerID:   peerID,
		DestIP:   dstIP,
		DestPort: dstPort,
		Protocol: proto,
	}
}

func TestResolve_HappyPath(t *testing.T) {
	r := New()
	r.Rebuild("acc-1", fixtureAccount())

	ev := newEvent("peer-a", "10.0.0.3", 443, 6)
	require.True(t, r.Resolve("acc-1", ev), "should resolve a TCP/443 flow to peer-c")
	assert.Equal(t, "p-allow-tcp-2026-05-01", string(ev.RuleID))
}

func TestResolve_ICMPMatchesProtocolOnly(t *testing.T) {
	r := New()
	r.Rebuild("acc-1", fixtureAccount())

	// ICMP doesn't carry a port; DestPort=0. The matcher should
	// accept (empty port filter == any).
	ev := newEvent("peer-b", "10.0.0.3", 0, 1) // proto 1 = ICMP
	require.True(t, r.Resolve("acc-1", ev))
	assert.Equal(t, "p-allow-icmp-2026-05-02", string(ev.RuleID))
}

func TestResolve_AmbiguityFirstByPolicyID(t *testing.T) {
	acc := fixtureAccount()
	// Add a second TCP/443 policy with a LATER ID. The older
	// p-allow-tcp-2026-05-01 must still win the tiebreaker.
	acc.Policies = append(acc.Policies, &types.Policy{
		ID:      "p-also-tcp-2026-05-15",
		Enabled: true,
		Rules: []*types.PolicyRule{{
			Enabled:      true,
			Action:       types.PolicyTrafficActionAccept,
			Sources:      []string{"group-laptops"},
			Destinations: []string{"group-servers"},
			Protocol:     types.PolicyRuleProtocolTCP,
			Ports:        []string{"443"},
		}},
	})

	r := New()
	r.Rebuild("acc-1", acc)

	ev := newEvent("peer-a", "10.0.0.3", 443, 6)
	require.True(t, r.Resolve("acc-1", ev))
	assert.Equal(t, "p-allow-tcp-2026-05-01", string(ev.RuleID), "older policy wins")
}

func TestResolve_NoMatchLeavesEmpty(t *testing.T) {
	r := New()
	r.Rebuild("acc-1", fixtureAccount())

	// peer-a tries to reach a destination outside any group.
	ev := newEvent("peer-a", "10.0.0.99", 443, 6)
	assert.False(t, r.Resolve("acc-1", ev))
	assert.Empty(t, ev.RuleID)
}

func TestResolve_PassThroughWhenAgentStamped(t *testing.T) {
	r := New()
	r.Rebuild("acc-1", fixtureAccount())

	ev := newEvent("peer-a", "10.0.0.3", 443, 6)
	ev.RuleID = []byte("agent-stamped-policy-id")
	assert.False(t, r.Resolve("acc-1", ev), "must not overwrite an agent-stamped RuleID")
	assert.Equal(t, "agent-stamped-policy-id", string(ev.RuleID))
}

func TestResolve_PortRangeMatch(t *testing.T) {
	acc := fixtureAccount()
	acc.Policies[0].Rules[0].Ports = nil
	acc.Policies[0].Rules[0].PortRanges = []types.RulePortRange{{Start: 8000, End: 8999}}

	r := New()
	r.Rebuild("acc-1", acc)

	hit := newEvent("peer-a", "10.0.0.3", 8443, 6)
	require.True(t, r.Resolve("acc-1", hit))
	assert.Equal(t, "p-allow-tcp-2026-05-01", string(hit.RuleID))

	miss := newEvent("peer-a", "10.0.0.3", 9000, 6)
	assert.False(t, r.Resolve("acc-1", miss))
}

func TestResolve_ProtocolALLMatchesAnything(t *testing.T) {
	acc := fixtureAccount()
	// Replace the TCP rule with an ALL rule.
	acc.Policies[0].Rules[0].Protocol = types.PolicyRuleProtocolALL
	acc.Policies[0].Rules[0].Ports = nil

	r := New()
	r.Rebuild("acc-1", acc)

	// Same policy hits for TCP, UDP, ICMP.
	for _, p := range []uint16{6, 17, 1} {
		ev := newEvent("peer-a", "10.0.0.3", 443, p)
		require.True(t, r.Resolve("acc-1", ev), "proto=%d should match ALL rule", p)
	}
}

func TestResolve_SubnetResourceDestination(t *testing.T) {
	acc := fixtureAccount()
	subnet := netip.MustParsePrefix("10.10.0.0/24")
	res := newNetworkResource("res-internal", []string{"group-servers"}, subnet)
	acc.NetworkResources = append(acc.NetworkResources, res)

	r := New()
	r.Rebuild("acc-1", acc)

	ev := newEvent("peer-a", "10.10.0.42", 443, 6)
	require.True(t, r.Resolve("acc-1", ev), "destination IP inside Subnet resource should match")
	assert.Equal(t, "p-allow-tcp-2026-05-01", string(ev.RuleID))
}

func TestResolve_HostResourceDestination(t *testing.T) {
	acc := fixtureAccount()
	host := netip.MustParsePrefix("10.20.0.5/32")
	res := newNetworkResource("res-host", []string{"group-servers"}, host)
	acc.NetworkResources = append(acc.NetworkResources, res)

	r := New()
	r.Rebuild("acc-1", acc)

	hit := newEvent("peer-a", "10.20.0.5", 443, 6)
	require.True(t, r.Resolve("acc-1", hit))

	miss := newEvent("peer-a", "10.20.0.6", 443, 6)
	assert.False(t, r.Resolve("acc-1", miss))
}

func TestResolve_UnknownPeerReturnsFalse(t *testing.T) {
	r := New()
	r.Rebuild("acc-1", fixtureAccount())

	ev := newEvent("peer-ghost", "10.0.0.3", 443, 6)
	assert.False(t, r.Resolve("acc-1", ev))
}

func TestResolve_CrossAccountIsolation(t *testing.T) {
	r := New()
	// Same peer ID, two unrelated accounts. Only acc-1's policy
	// graph contains it; querying acc-2 must miss.
	r.Rebuild("acc-1", fixtureAccount())

	ev := newEvent("peer-a", "10.0.0.3", 443, 6)
	assert.False(t, r.Resolve("acc-2", ev), "peer in acc-1 cannot resolve via acc-2 index")
	assert.False(t, r.Resolve("", ev), "empty accountID never resolves")
}

func TestResolve_DropRulesAreNotCandidates(t *testing.T) {
	acc := fixtureAccount()
	acc.Policies[0].Rules[0].Action = types.PolicyTrafficActionDrop

	r := New()
	r.Rebuild("acc-1", acc)

	ev := newEvent("peer-a", "10.0.0.3", 443, 6)
	assert.False(t, r.Resolve("acc-1", ev), "Drop rules must not produce attribution")
}

func TestResolve_DisabledRuleIgnored(t *testing.T) {
	acc := fixtureAccount()
	acc.Policies[0].Rules[0].Enabled = false

	r := New()
	r.Rebuild("acc-1", acc)

	ev := newEvent("peer-a", "10.0.0.3", 443, 6)
	assert.False(t, r.Resolve("acc-1", ev))
}

func TestResolve_NilEventSafe(t *testing.T) {
	r := New()
	r.Rebuild("acc-1", fixtureAccount())
	assert.False(t, r.Resolve("acc-1", nil), "nil event must not panic")
}

func TestRebuild_NilAccountForgets(t *testing.T) {
	r := New()
	r.Rebuild("acc-1", fixtureAccount())
	r.Rebuild("acc-1", nil)

	ev := newEvent("peer-a", "10.0.0.3", 443, 6)
	assert.False(t, r.Resolve("acc-1", ev), "Rebuild(nil) must drop the index")
}

func TestForget_RemovesAccount(t *testing.T) {
	r := New()
	r.Rebuild("acc-1", fixtureAccount())
	r.Forget("acc-1")

	ev := newEvent("peer-a", "10.0.0.3", 443, 6)
	assert.False(t, r.Resolve("acc-1", ev))
}

// newNetworkResource is a thin constructor that bypasses
// NewNetworkResource's address-parsing logic (we already have a
// netip.Prefix in hand). Mirrors the in-memory shape the account
// cache uses post-load.
func newNetworkResource(id string, groupIDs []string, prefix netip.Prefix) *resourceTypes.NetworkResource {
	return &resourceTypes.NetworkResource{
		ID:       id,
		Prefix:   prefix,
		GroupIDs: groupIDs,
		Enabled:  true,
	}
}
