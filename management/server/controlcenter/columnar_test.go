package controlcenter

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	resourceTypes "github.com/openzro/openzro/management/server/networks/resources/types"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/posture"
	"github.com/openzro/openzro/management/server/types"
)

// columnar_test pins the v2 topology projection (ADR-0017
// 2026-05-18b): four focus tabs, Policy always the middle pivot
// column, green/red carried as the posture EdgeState. These replace
// the retired v1 reach-graph tests.

func peerOf(id, name, ip, userID string, ver string) *nbpeer.Peer {
	return &nbpeer.Peer{
		ID: id, Name: name, UserID: userID,
		IP:   net.ParseIP(ip),
		Meta: nbpeer.PeerSystemMeta{GoOS: "linux", WtVersion: ver},
	}
}

// acct: p1(alice,u1) ∈ g1 ; p2 ∈ g2. Policy "allow-ssh" g1→g2 tcp:22.
// nr1 (10.0.0.0/24) is backed by g2. pcFail denies any peer whose NB
// version < 99 (i.e. every test peer).
func acct() *types.Account {
	return &types.Account{
		Peers: map[string]*nbpeer.Peer{
			"p1": peerOf("p1", "alice", "100.64.0.1", "u1", "0.30.0"),
			"p2": peerOf("p2", "bob", "100.64.0.2", "u2", "0.30.0"),
		},
		Users: map[string]*types.User{
			"u1": {Id: "u1", Name: "Alice", Email: "alice@example.io"},
		},
		Groups: map[string]*types.Group{
			"g1": {ID: "g1", Name: "engineers", Peers: []string{"p1"}},
			// nr1 is linked to g2 the way openZro models it — via
			// Group.Resources — so GetPoliciesForNetworkResource can
			// resolve it (res.GroupIDs is gorm:"-" / empty here).
			"g2": {
				ID: "g2", Name: "servers", Peers: []string{"p2"},
				Resources: []types.Resource{{ID: "nr1", Type: "host"}},
			},
		},
		Policies: []*types.Policy{
			{
				ID: "pol1", Name: "allow-ssh", Enabled: true,
				Rules: []*types.PolicyRule{{
					ID: "r1", Enabled: true,
					Sources:      []string{"g1"},
					Destinations: []string{"g2"},
					Protocol:     types.PolicyRuleProtocolTCP,
					Ports:        []string{"22"},
				}},
			},
		},
		NetworkResources: []*resourceTypes.NetworkResource{{
			ID: "nr1", Name: "db", Address: "10.0.0.0/24",
			Type: resourceTypes.Subnet, Enabled: true,
			GroupIDs: []string{"g2"},
			Prefix:   netip.MustParsePrefix("10.0.0.0/24"),
		}},
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

func edgeOf(g *GraphDTO, from, to string) *Edge {
	for i := range g.Edges {
		if g.Edges[i].From == from && g.Edges[i].To == to {
			return &g.Edges[i]
		}
	}
	return nil
}

func TestPeerFocus_Columnar(t *testing.T) {
	g, err := BuildGraph(context.Background(), acct(), Focus{Type: FocusPeer, ID: "p1"})
	require.NoError(t, err)

	focus := nodeByID(g, "p1")
	require.NotNil(t, focus)
	require.Equal(t, NodeFocus, focus.Kind)
	require.Equal(t, colFocus, focus.Meta["column"])
	require.Equal(t, "100.64.0.1", focus.Meta["ip"])
	require.Equal(t, "linux", focus.Meta["os"])

	pol := nodeByID(g, "policy:pol1")
	require.NotNil(t, pol)
	require.Equal(t, NodePolicy, pol.Kind)
	require.Equal(t, colPolicies, pol.Meta["column"])
	require.Equal(t, "TCP:22", pol.Meta["port"])

	res := nodeByID(g, "group:g2")
	require.NotNil(t, res)
	require.Equal(t, NodeGroup, res.Kind)
	require.Equal(t, colResources, res.Meta["column"])

	e1 := edgeOf(g, "p1", "policy:pol1")
	require.NotNil(t, e1)
	require.Equal(t, EdgeEnforced, e1.State)
	require.Equal(t, PermitPolicy, e1.PermitSource)

	e2 := edgeOf(g, "policy:pol1", "group:g2")
	require.NotNil(t, e2)
	require.Equal(t, EdgeEnforced, e2.State)

	// nr1 is backed by g2 → it must also appear in the resource column.
	require.NotNil(t, nodeByID(g, "nr:nr1"))
	require.NotNil(t, edgeOf(g, "policy:pol1", "nr:nr1"))
}

func TestPeerFocus_PostureBlocked(t *testing.T) {
	a := acct()
	a.PostureChecks = []*posture.Checks{{
		ID: "pc1", Name: "min-version",
		Checks: posture.ChecksDefinition{
			NBVersionCheck: &posture.NBVersionCheck{MinVersion: "99.0.0"},
		},
	}}
	a.Policies[0].SourcePostureChecks = []string{"pc1"}

	g, err := BuildGraph(context.Background(), a, Focus{Type: FocusPeer, ID: "p1"})
	require.NoError(t, err)

	e := edgeOf(g, "p1", "policy:pol1")
	require.NotNil(t, e)
	require.Equal(t, EdgePostureBlocked, e.State)
	require.Equal(t, "min-version", e.Meta["postureCheck"])
	require.Equal(t, "pc1", e.Meta["postureCheckId"])

	// D1.2: the resource is still policy-permitted — the node and the
	// policy→resource edge must survive so the audit shows WHAT is
	// blocked, and that edge stays enforced (posture is source-side).
	require.NotNil(t, nodeByID(g, "group:g2"))
	require.Equal(t, EdgeEnforced, edgeOf(g, "policy:pol1", "group:g2").State)
}

// Regression (#39 v2 review, finding 1): a network resource linked
// to a destination group via Group.Resources must appear in the
// resources column of a SOURCE focus (peer/user/group) even when
// NetworkResource.GroupIDs is empty — its real gorm:"-" state. The
// old fanResources keyed off res.GroupIDs and dropped it.
func TestPeerFocus_ResourceViaGroupNoGroupIDs(t *testing.T) {
	a := acct()
	a.NetworkResources[0].GroupIDs = nil // prove we don't depend on it

	g, err := BuildGraph(context.Background(), a, Focus{Type: FocusPeer, ID: "p1"})
	require.NoError(t, err)

	nr := nodeByID(g, "nr:nr1")
	require.NotNil(t, nr, "resource linked via Group.Resources must show")
	require.Equal(t, colResources, nr.Meta["column"])
	require.NotNil(t, edgeOf(g, "policy:pol1", "nr:nr1"))
}

func TestUserFocus_Columnar(t *testing.T) {
	g, err := BuildGraph(context.Background(), acct(), Focus{Type: FocusUser, ID: "u1"})
	require.NoError(t, err)

	u := nodeByID(g, "u1")
	require.NotNil(t, u)
	require.Equal(t, NodeFocus, u.Kind)
	require.Equal(t, colFocus, u.Meta["column"])
	require.Equal(t, "alice@example.io", u.Meta["email"])

	p := nodeByID(g, "p1")
	require.NotNil(t, p)
	require.Equal(t, NodePeer, p.Kind)
	require.Equal(t, colPeers, p.Meta["column"])

	// User→Peer is identity ownership, not a policy permit.
	up := edgeOf(g, "u1", "p1")
	require.NotNil(t, up)
	require.Equal(t, EdgeState(EdgeEnforced), up.State)
	require.Equal(t, PermitIdentity, up.PermitSource)
	require.Empty(t, up.PolicyID)

	// …and the peer still fans into the policy/resource columns.
	require.NotNil(t, edgeOf(g, "p1", "policy:pol1"))
	require.NotNil(t, edgeOf(g, "policy:pol1", "group:g2"))
}

func TestGroupFocus_Columnar(t *testing.T) {
	g, err := BuildGraph(context.Background(), acct(), Focus{Type: FocusGroup, ID: "g1"})
	require.NoError(t, err)

	focus := nodeByID(g, "group:g1")
	require.NotNil(t, focus)
	require.Equal(t, NodeFocus, focus.Kind)
	require.Equal(t, colFocus, focus.Meta["column"])

	e := edgeOf(g, "group:g1", "policy:pol1")
	require.NotNil(t, e)
	require.Equal(t, EdgeEnforced, e.State)
	require.Equal(t, "1 of 1 members", e.Meta["reachedBy"])
}

func TestGroupFocus_AllMembersPostureBlocked(t *testing.T) {
	a := acct()
	a.PostureChecks = []*posture.Checks{{
		ID: "pc1", Name: "min-version",
		Checks: posture.ChecksDefinition{
			NBVersionCheck: &posture.NBVersionCheck{MinVersion: "99.0.0"},
		},
	}}
	a.Policies[0].SourcePostureChecks = []string{"pc1"}

	g, err := BuildGraph(context.Background(), a, Focus{Type: FocusGroup, ID: "g1"})
	require.NoError(t, err)

	e := edgeOf(g, "group:g1", "policy:pol1")
	require.NotNil(t, e)
	require.Equal(t, EdgePostureBlocked, e.State)
	require.Equal(t, "0 of 1 members", e.Meta["reachedBy"])
	require.Equal(t, "min-version", e.Meta["postureCheck"])
}

// Strict group aggregate semantics (#39 v2 review, finding 2).
func TestGroupState_Strict(t *testing.T) {
	t.Run("empty group emits no enforced edge", func(t *testing.T) {
		a := acct()
		a.Groups["g1"] = &types.Group{ID: "g1", Name: "engineers"} // 0 peers

		g, err := BuildGraph(context.Background(), a,
			Focus{Type: FocusGroup, ID: "g1"})
		require.NoError(t, err)
		require.NotNil(t, nodeByID(g, "group:g1")) // focus card stays
		require.Nil(t, edgeOf(g, "group:g1", "policy:pol1"))
		require.Len(t, g.Edges, 0)
	})

	t.Run("stale member is not a posture pass", func(t *testing.T) {
		a := acct()
		// g1 = one real peer (p1, fails posture) + one stale id.
		a.Groups["g1"] = &types.Group{
			ID: "g1", Name: "engineers", Peers: []string{"p1", "ghost"},
		}
		a.PostureChecks = []*posture.Checks{{
			ID: "pc1", Name: "min-version",
			Checks: posture.ChecksDefinition{
				NBVersionCheck: &posture.NBVersionCheck{MinVersion: "99.0.0"},
			},
		}}
		a.Policies[0].SourcePostureChecks = []string{"pc1"}

		g, err := BuildGraph(context.Background(), a,
			Focus{Type: FocusGroup, ID: "g1"})
		require.NoError(t, err)
		e := edgeOf(g, "group:g1", "policy:pol1")
		require.NotNil(t, e)
		// ghost excluded from n; p1 the only real member, denied.
		require.Equal(t, EdgePostureBlocked, e.State)
		require.Equal(t, "0 of 1 members", e.Meta["reachedBy"])
	})

	t.Run("partial pass is enforced but reports k of n", func(t *testing.T) {
		a := acct()
		a.Peers["p3"] = peerOf("p3", "carol", "100.64.0.3", "u3", "9.9.9")
		a.Groups["g1"] = &types.Group{
			ID: "g1", Name: "engineers", Peers: []string{"p1", "p3"},
		}
		a.PostureChecks = []*posture.Checks{{
			ID: "pc1", Name: "min-version",
			Checks: posture.ChecksDefinition{
				NBVersionCheck: &posture.NBVersionCheck{MinVersion: "1.0.0"},
			},
		}}
		a.Policies[0].SourcePostureChecks = []string{"pc1"}

		g, err := BuildGraph(context.Background(), a,
			Focus{Type: FocusGroup, ID: "g1"})
		require.NoError(t, err)
		e := edgeOf(g, "group:g1", "policy:pol1")
		require.NotNil(t, e)
		// p1 = 0.30.0 fails >=1.0.0; p3 = 9.9.9 passes. Partial reach
		// must stay enforced (union) but report "1 of 2", never look
		// like full reach.
		require.Equal(t, EdgeEnforced, e.State)
		require.Equal(t, "1 of 2 members", e.Meta["reachedBy"])
	})
}

func TestNetworkFocus_InverseFanIn(t *testing.T) {
	g, err := BuildGraph(context.Background(), acct(), Focus{Type: FocusNetwork, ID: "nr1"})
	require.NoError(t, err)

	focus := nodeByID(g, "nr:nr1")
	require.NotNil(t, focus)
	require.Equal(t, NodeFocus, focus.Kind)
	require.Equal(t, colFocus, focus.Meta["column"])
	require.Equal(t, "10.0.0.0/24", focus.Meta["sub"])

	// Inverse: the source group sits in the leftmost "groups" column,
	// the policy in the middle, the resource is the focus on the right.
	src := nodeByID(g, "group:g1")
	require.NotNil(t, src)
	require.Equal(t, NodeGroup, src.Kind)
	require.Equal(t, colGroups, src.Meta["column"])

	require.NotNil(t, edgeOf(g, "group:g1", "policy:pol1"))
	pf := edgeOf(g, "policy:pol1", "nr:nr1")
	require.NotNil(t, pf)
	require.Equal(t, EdgeEnforced, pf.State)
}

// Regression (#39 v2 review): a policy that targets the resource
// only via its GROUP — and an account where NetworkResource.GroupIDs
// is empty (its real gorm:"-" state) — must still show
// group → policy → resource with the source group's peer count. The
// old hand-rolled match keyed off res.GroupIDs and showed nothing.
func TestNetworkFocus_PolicyViaResourceGroup(t *testing.T) {
	a := acct()
	a.NetworkResources[0].GroupIDs = nil // prove we don't depend on it
	a.Policies[0].Rules[0].DestinationResource = types.Resource{}

	g, err := BuildGraph(context.Background(), a, Focus{Type: FocusNetwork, ID: "nr1"})
	require.NoError(t, err)

	require.NotNil(t, nodeByID(g, "nr:nr1"))
	src := nodeByID(g, "group:g1")
	require.NotNil(t, src)
	require.Equal(t, colGroups, src.Meta["column"])
	require.Equal(t, "1 peer(s)", src.Meta["sub"])
	require.NotNil(t, edgeOf(g, "group:g1", "policy:pol1"))
	require.NotNil(t, edgeOf(g, "policy:pol1", "nr:nr1"))
}

// Network focus must NOT draw an orphan green policy→resource flow
// when every source group of the targeting policy is empty/stale —
// nobody can reach the resource, so under strict-green there is no
// policy path at all (#39 v2 review, finding R2-net).
func TestNetworkFocus_EmptyOrAllStaleSourceGroupsEmitNoPolicyPath(t *testing.T) {
	t.Run("empty source group", func(t *testing.T) {
		a := acct()
		a.Groups["g1"] = &types.Group{ID: "g1", Name: "engineers"} // 0 peers

		g, err := BuildGraph(context.Background(), a,
			Focus{Type: FocusNetwork, ID: "nr1"})
		require.NoError(t, err)
		require.NotNil(t, nodeByID(g, "nr:nr1")) // focus card stays
		require.Nil(t, nodeByID(g, "policy:pol1"))
		require.Nil(t, nodeByID(g, "group:g1"))
		require.Nil(t, edgeOf(g, "policy:pol1", "nr:nr1"))
		require.Len(t, g.Edges, 0)
	})

	t.Run("all-stale source group", func(t *testing.T) {
		a := acct()
		a.Groups["g1"] = &types.Group{
			ID: "g1", Name: "engineers", Peers: []string{"ghost1", "ghost2"},
		}

		g, err := BuildGraph(context.Background(), a,
			Focus{Type: FocusNetwork, ID: "nr1"})
		require.NoError(t, err)
		require.Nil(t, nodeByID(g, "policy:pol1"))
		require.Nil(t, edgeOf(g, "policy:pol1", "nr:nr1"))
		require.Len(t, g.Edges, 0)
	})
}

func TestBuildGraph_Errors(t *testing.T) {
	a := acct()
	_, err := BuildGraph(context.Background(), a, Focus{Type: FocusPeer, ID: "ghost"})
	require.ErrorIs(t, err, ErrFocusNotFound)

	_, err = BuildGraph(context.Background(), a, Focus{Type: FocusUser, ID: "ghost"})
	require.ErrorIs(t, err, ErrFocusNotFound)

	_, err = BuildGraph(context.Background(), a, Focus{Type: FocusNetwork, ID: "ghost"})
	require.ErrorIs(t, err, ErrFocusNotFound)

	_, err = BuildGraph(context.Background(), a, Focus{Type: "bogus", ID: "x"})
	require.ErrorIs(t, err, ErrUnsupportedFocus)
}

func TestBuildGraph_Deterministic(t *testing.T) {
	a := acct()
	first, err := BuildGraph(context.Background(), a, Focus{Type: FocusPeer, ID: "p1"})
	require.NoError(t, err)
	for i := 0; i < 20; i++ {
		next, err := BuildGraph(context.Background(), a, Focus{Type: FocusPeer, ID: "p1"})
		require.NoError(t, err)
		require.Equal(t, first, next)
	}
}

// A focus with no matching policy must serialize edges/nodes as
// JSON arrays, never `null` — the dashboard calls graph.edges.length
// straight on the wire (#39 v2 review: null crashed the canvas).
func TestEmptyGraphMarshalsAsArrays(t *testing.T) {
	a := acct()
	a.Policies = nil // peer p1 now matches no policy

	g, err := BuildGraph(context.Background(), a, Focus{Type: FocusPeer, ID: "p1"})
	require.NoError(t, err)
	require.NotNil(t, g.Edges)
	require.Len(t, g.Edges, 0)

	b, err := json.Marshal(g)
	require.NoError(t, err)
	s := string(b)
	require.Contains(t, s, `"edges":[]`)
	require.False(t, strings.Contains(s, `"edges":null`))
	require.False(t, strings.Contains(s, `"nodes":null`))
}

func TestDisabledPolicyAndRuleSkipped(t *testing.T) {
	a := acct()
	a.Policies[0].Enabled = false
	g, err := BuildGraph(context.Background(), a, Focus{Type: FocusPeer, ID: "p1"})
	require.NoError(t, err)
	require.Nil(t, nodeByID(g, "policy:pol1"))

	a.Policies[0].Enabled = true
	a.Policies[0].Rules[0].Enabled = false
	g, err = BuildGraph(context.Background(), a, Focus{Type: FocusPeer, ID: "p1"})
	require.NoError(t, err)
	require.Nil(t, nodeByID(g, "policy:pol1"))
}
