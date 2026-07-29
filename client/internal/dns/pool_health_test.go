package dns

// This file covers the health side of pooling: how a member's deactivation and
// return move the zone's registration, and how the server wires the two levels
// together. The selection behavior itself is in pool_test.go.

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/client/internal/dns/selection"
	"github.com/openzro/openzro/client/internal/peer"
	nbdns "github.com/openzro/openzro/dns"
	"github.com/openzro/openzro/management/domain"
)

// TestResolverPool_MemberTransitions covers the membership bookkeeping the health
// hooks drive.
func TestResolverPool_MemberTransitions(t *testing.T) {
	pool := newTestPool(t, "prefer_private",
		&fakeGroup{src: "a-first", resp: aReply(privateAddr)},
		&fakeGroup{src: "b-second", resp: aReply(publicAddr)},
	)

	assert.True(t, pool.setMember(0, false), "disabling a group changes the pool")
	assert.False(t, pool.setMember(0, false), "disabling twice is a no-op")
	assert.True(t, pool.serving(), "one group is left")

	assert.True(t, pool.setMember(1, false), "the last group goes down")
	assert.False(t, pool.serving(), "no group is left")

	assert.True(t, pool.setMember(1, true), "a group comes back")
	assert.True(t, pool.serving())
	assert.False(t, pool.setMember(1, true), "enabling twice is a no-op")

	assert.False(t, pool.setMember(-1, false), "an out-of-range member is ignored")
	assert.False(t, pool.setMember(7, true), "an out-of-range member is ignored")
}

// TestResolverPool_ZoneStateFollowsMembership pins that the zone-level hooks are
// driven by the membership, not by edges. Two groups can flip at the same moment
// — one deactivating under its own lock while another reactivates from its
// backoff goroutine — so reacting to "was that the last one?" would let the hooks
// run in either order and leave the zone deregistered with a healthy group in the
// pool. Deciding from the membership converges whatever the order.
func TestResolverPool_ZoneStateFollowsMembership(t *testing.T) {
	// Each case flips members in order and states the zone state and the number
	// of zone hook calls that must follow.
	type step struct {
		member  int
		enabled bool
	}
	cases := []struct {
		name     string
		steps    []step
		wantUp   bool
		wantDown int
		wantBack int
	}{
		{
			name:   "losing one of two groups keeps the zone served",
			steps:  []step{{0, false}},
			wantUp: true,
		},
		{
			name:     "losing the last group deactivates the zone",
			steps:    []step{{0, false}, {1, false}},
			wantDown: 1,
		},
		{
			name:     "the group that returns first brings the zone back",
			steps:    []step{{0, false}, {1, false}, {0, true}},
			wantUp:   true,
			wantDown: 1,
			wantBack: 1,
		},
		{
			// The adversarial interleaving: the reactivation of the group that
			// was already down is applied before the other group's failure.
			name:     "a reactivation racing a failure leaves the zone served",
			steps:    []step{{1, false}, {1, true}, {0, false}},
			wantUp:   true,
			wantDown: 0,
			wantBack: 0,
		},
		{
			name:     "repeated failures do not deactivate twice",
			steps:    []step{{0, false}, {1, false}, {1, false}, {0, false}},
			wantDown: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := newTestPool(t, "prefer_private",
				&fakeGroup{src: "a-first", resp: aReply(privateAddr)},
				&fakeGroup{src: "b-second", resp: aReply(publicAddr)},
			)

			var down, back int
			deactivate := func(error) { down++ }
			reactivate := func() { back++ }

			for _, s := range tc.steps {
				pool.setMember(s.member, s.enabled)
				pool.applyZoneState(nil, deactivate, reactivate)
			}

			assert.Equal(t, tc.wantUp, pool.zoneUp, "zone state")
			assert.Equal(t, tc.wantUp, pool.serving(), "zone state must match the membership")
			assert.Equal(t, tc.wantDown, down, "zone deactivations")
			assert.Equal(t, tc.wantBack, back, "zone reactivations")
		})
	}
}

// TestServer_PooledZoneHooksAreSerialized proves the hooks of two groups cannot
// interleave: while one zone hook runs, the other group's transition has to wait.
// Without that, both groups decide their transition first and the hooks then run
// in an unspecified order — reactivate before deactivate leaves the zone
// deregistered although a group is healthy, and violates the hook contract.
func TestServer_PooledZoneHooksAreSerialized(t *testing.T) {
	const zone = "eu.example.com"

	server := poolServer(t, zone)
	pool := registerPools(t, server, []*nbdns.NameServerGroup{
		nsGroupFor("10.0.0.53", zone),
		nsGroupFor("8.8.8.8", zone),
	})[zone]

	downA, _ := memberHooks(t, pool, 0)
	downB, upB := memberHooks(t, pool, 1)

	// B is already down, so A failing is what takes the zone with it.
	downB(errors.New("timeout"))
	require.True(t, zoneDisabled(t, server, zone) == false, "one group left: zone still served")

	entered := make(chan struct{})
	release := make(chan struct{})
	blocked := make(chan struct{})

	// Hold the zone-level deactivation open, then have B return underneath it.
	pool.hookMu.Lock()
	go func() {
		close(entered)
		downA(errors.New("timeout"))
	}()
	<-entered

	go func() {
		upB()
		close(blocked)
	}()

	select {
	case <-blocked:
		t.Fatal("a member transition ran while another was in progress")
	case <-time.After(100 * time.Millisecond):
	}

	pool.hookMu.Unlock()
	close(release)
	<-blocked

	// Whatever order the two got the lock in, the zone must match the membership.
	assert.True(t, pool.serving(), "B is back, so the pool serves the zone")
	assert.False(t, zoneDisabled(t, server, zone), "the zone must not stay disabled")
	assert.True(t, zoneRegistered(server, zone, PriorityUpstream), "the handler must be registered")
}

// TestServer_PoolReplacesPerGroupHandlers asserts the merge point: with a
// selection policy configured, a zone served by several nameserver groups gets a
// single pooled handler instead of one handler per group at descending
// priorities. Without a policy the pre-existing per-group handlers stay.
func TestServer_PoolReplacesPerGroupHandlers(t *testing.T) {
	nsGroups := []*nbdns.NameServerGroup{
		{
			Domains:     []string{"eu.example.com"},
			NameServers: []nbdns.NameServer{{IP: netip.MustParseAddr("10.0.0.53"), NSType: nbdns.UDPNameServerType, Port: 53}},
		},
		{
			Domains:     []string{"eu.example.com"},
			NameServers: []nbdns.NameServer{{IP: netip.MustParseAddr("8.8.8.8"), NSType: nbdns.UDPNameServerType, Port: 53}},
		},
	}

	newServer := func(policy selection.Policy) *DefaultServer {
		return &DefaultServer{
			ctx:             context.Background(),
			wgInterface:     &mocWGIface{},
			handlerChain:    NewHandlerChain(),
			service:         &mockService{},
			extraDomains:    make(map[domain.Domain]int),
			selectionPolicy: policy,
		}
	}

	t.Run("without a policy every group keeps its own handler", func(t *testing.T) {
		updates, err := newServer(nil).buildUpstreamHandlerUpdate(nsGroups)
		require.NoError(t, err)
		require.Len(t, updates, 2)
		assert.Equal(t, PriorityUpstream, updates[0].priority)
		assert.Equal(t, PriorityUpstream-1, updates[1].priority)
		for _, u := range updates {
			_, isPool := u.handler.(*resolverPool)
			assert.False(t, isPool, "expected a per-group handler, got a pool")
			u.handler.Stop()
		}
	})

	t.Run("with a policy the zone gets one pooled handler", func(t *testing.T) {
		policy, ok := selection.Get("prefer_private")
		require.True(t, ok)

		updates, err := newServer(policy).buildUpstreamHandlerUpdate(nsGroups)
		require.NoError(t, err)
		require.Len(t, updates, 1)
		assert.Equal(t, "eu.example.com", updates[0].domain)
		assert.Equal(t, PriorityUpstream, updates[0].priority)

		pool, isPool := updates[0].handler.(*resolverPool)
		require.True(t, isPool, "expected a pooled handler, got %T", updates[0].handler)
		assert.Len(t, pool.members, 2)
		pool.Stop()
	})
}

// nsGroupFor builds a nameserver group serving the given domains.
func nsGroupFor(ip string, domains ...string) *nbdns.NameServerGroup {
	return &nbdns.NameServerGroup{
		Domains:     domains,
		NameServers: []nbdns.NameServer{{IP: netip.MustParseAddr(ip), NSType: nbdns.UDPNameServerType, Port: 53}},
	}
}

// hooks returns the health hooks installed on this resolver. It lives in the
// test because only the test needs to read them back — production only ever
// installs them (setCallbacks) and calls them from within the resolver.
func (u *upstreamResolverBase) hooks() (deactivate func(error), reactivate func()) {
	return u.deactivate, u.reactivate
}

// hookedResolver is what the server installs the health hooks on. Every
// platform's resolver satisfies it through the embedded upstreamResolverBase, so
// a test can drive exactly the pair the resolver itself would call.
type hookedResolver interface {
	hooks() (deactivate func(error), reactivate func())
}

// poolServer builds a server that pools multi-group zones, with just enough
// state for the health hooks to run: a host config listing the zones and a
// status recorder for the per-group state.
func poolServer(t *testing.T, zones ...string) *DefaultServer {
	t.Helper()

	policy, ok := selection.Get("prefer_private")
	require.True(t, ok)

	domains := make([]DomainConfig, 0, len(zones))
	for _, zone := range zones {
		domains = append(domains, DomainConfig{Domain: zone})
	}

	return &DefaultServer{
		ctx:             context.Background(),
		wgInterface:     &mocWGIface{},
		handlerChain:    NewHandlerChain(),
		service:         &mockService{},
		hostManager:     &noopHostConfigurator{},
		extraDomains:    make(map[domain.Domain]int),
		statusRecorder:  peer.NewRecorder("test"),
		currentConfig:   HostDNSConfig{Domains: domains},
		selectionPolicy: policy,
	}
}

// registerPools registers every pooled handler the way updateMux does, and
// returns them by zone.
func registerPools(t *testing.T, s *DefaultServer, groups []*nbdns.NameServerGroup) map[string]*resolverPool {
	t.Helper()

	updates, err := s.buildUpstreamHandlerUpdate(groups)
	require.NoError(t, err)

	pools := make(map[string]*resolverPool, len(updates))
	for _, u := range updates {
		pool, isPool := u.handler.(*resolverPool)
		require.Truef(t, isPool, "zone %s did not get a pooled handler, got %T", u.domain, u.handler)
		s.registerHandler([]string{u.domain}, pool, u.priority)
		pools[u.domain] = pool
		t.Cleanup(pool.Stop)
	}
	return pools
}

// memberHooks returns the health hooks the server installed on a pooled member.
func memberHooks(t *testing.T, pool *resolverPool, member int) (func(error), func()) {
	t.Helper()

	hooked, ok := pool.members[member].resolver.(hookedResolver)
	require.Truef(t, ok, "member %d is a %T, which carries no health hooks", member, pool.members[member].resolver)

	deactivate, reactivate := hooked.hooks()
	require.NotNil(t, deactivate, "member %d has no deactivate hook", member)
	require.NotNil(t, reactivate, "member %d has no reactivate hook", member)
	return deactivate, reactivate
}

// zoneDisabled reports whether the host config has the zone marked disabled.
func zoneDisabled(t *testing.T, s *DefaultServer, zone string) bool {
	t.Helper()

	for _, d := range s.currentConfig.Domains {
		if d.Domain == zone {
			return d.Disabled
		}
	}
	t.Fatalf("zone %s is not in the host config", zone)
	return false
}

// zoneRegistered reports whether a handler for the zone sits in the chain.
func zoneRegistered(s *DefaultServer, zone string, priority int) bool {
	s.handlerChain.mu.RLock()
	defer s.handlerChain.mu.RUnlock()

	for _, h := range s.handlerChain.handlers {
		if h.OrigPattern == dns.Fqdn(zone) && h.Priority == priority {
			return true
		}
	}
	return false
}

// TestServer_PooledZoneRecoversWhateverGroupReturnsFirst pins the zone-level
// health contract: the last group going down deactivates the zone, and ANY group
// coming back brings it up again — not just the one that happened to be last.
// The hooks share one removeIndex per pool, so this holds whichever group wins
// the race back.
func TestServer_PooledZoneRecoversWhateverGroupReturnsFirst(t *testing.T) {
	const zone = "eu.example.com"

	server := poolServer(t, zone)
	pools := registerPools(t, server, []*nbdns.NameServerGroup{
		nsGroupFor("10.0.0.53", zone),
		nsGroupFor("8.8.8.8", zone),
	})
	pool := pools[zone]
	require.Len(t, pool.members, 2)

	downFirst, upFirst := memberHooks(t, pool, 0)
	downLast, _ := memberHooks(t, pool, 1)
	timeout := errors.New("timeout")

	downFirst(timeout)
	assert.False(t, zoneDisabled(t, server, zone), "one group left: the zone stays served")
	assert.True(t, zoneRegistered(server, zone, PriorityUpstream), "the handler stays registered")

	downLast(timeout)
	require.True(t, zoneDisabled(t, server, zone), "the last group down must deactivate the zone")
	require.False(t, zoneRegistered(server, zone, PriorityUpstream), "the handler must be deregistered")

	// The group that comes back first is NOT the one that took the zone down.
	upFirst()
	assert.False(t, zoneDisabled(t, server, zone), "any group returning must bring the zone back")
	assert.True(t, zoneRegistered(server, zone, PriorityUpstream), "the handler must be registered again")
}

// TestServer_PooledZoneDeactivationLeavesOtherZonesAlone pins the scope of the
// zone-level hooks. A group may serve several domains, each pooled separately
// with its own health; one zone losing all of its groups must not deregister
// another zone that still has a healthy group.
func TestServer_PooledZoneDeactivationLeavesOtherZonesAlone(t *testing.T) {
	const (
		failing = "eu.example.com"
		healthy = "us.example.com"
	)

	server := poolServer(t, failing, healthy)
	// The middle group serves both zones, so it is pooled in both.
	pools := registerPools(t, server, []*nbdns.NameServerGroup{
		nsGroupFor("10.0.0.53", failing),
		nsGroupFor("10.0.0.54", failing, healthy),
		nsGroupFor("8.8.8.8", healthy),
	})
	require.Len(t, pools, 2)

	timeout := errors.New("timeout")
	for member := range pools[failing].members {
		down, _ := memberHooks(t, pools[failing], member)
		down(timeout)
	}

	require.True(t, zoneDisabled(t, server, failing), "the failing zone lost every group")
	assert.False(t, zoneDisabled(t, server, healthy), "the other zone still has a healthy group")
	assert.True(t, zoneRegistered(server, healthy, PriorityUpstream),
		"the other zone's handler must stay registered")
}
