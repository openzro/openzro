package dns

// This file holds the server side of pooling: how a DefaultServer builds a
// resolverPool for a zone and wires the groups' health hooks to it. The pool
// itself lives in pool.go.

import (
	"errors"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/client/internal/dns/selection"
	nbdns "github.com/openzro/openzro/dns"
)

// resolveSelectionPolicy turns a configured policy name into the policy the
// server uses. Case and surrounding space are ignored: the only way to set this
// today is by hand in the client's configuration file, so the likeliest mistake
// is "Prefer_Private" or a stray space, and neither deserves to be treated as a
// different policy. A name we still do not know is only warned about — a
// misspelled preference must not stop the client from resolving — and the
// warning echoes what was configured so the typo is recognizable.
//
// An empty name means the operator did not opt in and yields no policy at all,
// deliberately not selection.DefaultPolicy: "nothing configured" stays a state
// the server can represent. Whether a policy actually pools a zone is decided
// at the handler, by selection.Ranking.
func resolveSelectionPolicy(configured string) selection.Policy {
	name := strings.ToLower(strings.TrimSpace(configured))
	if name == "" {
		return nil
	}

	policy, ok := selection.Get(name)
	if !ok {
		log.Warnf("ignoring unknown DNS selection policy %q, keeping the default behavior", configured)
		return nil
	}

	log.Infof("DNS selection policy is %s", policy.Name())
	return policy
}

// createPooledHandler builds the one handler that serves a zone from all of its
// nameserver groups at once. It replaces the per-group handlers at descending
// priorities, of which only the highest would ever be consulted.
func (s *DefaultServer) createPooledHandler(domainGroup nsGroupsByDomain, basePriority int) ([]handlerWrapper, error) {
	// A pool ranks candidates against each other, so it dereferences its policy on
	// every query. Establish that here, where the pool is built, instead of
	// nil-checking each use: the caller only pools for a ranking policy, and if
	// that ever changes this fails loudly rather than panicking on a query.
	if !selection.Ranking(s.selectionPolicy) {
		return nil, fmt.Errorf("pooling zone %s needs a ranking selection policy", domainGroup.domain)
	}

	handlers := make([]upstreamGroupHandler, 0, len(domainGroup.groups))
	groups := make([]*nbdns.NameServerGroup, 0, len(domainGroup.groups))

	for _, nsGroup := range domainGroup.groups {
		handler, err := s.newGroupResolver(nsGroup, domainGroup.domain)
		if errors.Is(err, errNoUsableNameserver) {
			continue
		}
		if err != nil {
			return nil, err
		}
		handlers = append(handlers, handler)
		groups = append(groups, nsGroup)
	}

	if len(handlers) == 0 {
		return nil, nil
	}

	resolvers := make([]groupResolver, 0, len(handlers))
	for _, handler := range handlers {
		resolvers = append(resolvers, handler)
	}

	pool := newResolverPool(domainGroup.domain, s.selectionPolicy, resolvers)
	log.Debugf("creating %s with policy=%s priority=%d", pool, s.selectionPolicy.Name(), basePriority)

	// The zone-level hooks are derived once for the whole pool, not per member:
	// they close over the removeIndex that reactivate needs to undo what deactivate
	// did, so a pair per member would let whichever group comes back first
	// reactivate through its own empty index, leaving the zone dead until the next
	// configuration push. zoneScope keeps them to this zone alone, because a
	// member's group may serve further domains that are pooled separately.
	zoneDeactivate, zoneReactivate := s.upstreamCallbacks(zoneScope(domainGroup.domain), pool, basePriority)

	for i, handler := range handlers {
		handler.setCallbacks(s.poolMemberCallbacks(pool, i, groups[i], zoneDeactivate, zoneReactivate))
	}

	return []handlerWrapper{{
		domain:   domainGroup.domain,
		handler:  pool,
		priority: basePriority,
	}}, nil
}

// zoneScope describes the pooled zone as a nameserver group, so the zone-level
// health hooks cover exactly the domain this pool serves — and not every domain
// the members' groups happen to serve.
func zoneScope(domain string) *nbdns.NameServerGroup {
	return &nbdns.NameServerGroup{
		Domains: []string{domain},
		Primary: domain == nbdns.RootZone,
	}
}

// poolMemberCallbacks returns the health hooks for one pooled nameserver group,
// given the zone-level pair shared by every member. A group that stops answering
// is only dropped from the fan-out — the zone stays served while another group is
// left, which is the point of pooling — and only the last group going down runs
// the zone-level deactivation (deregister the handler, mark the domain disabled,
// re-apply the host config), as a lone upstream resolver does today; the first
// group back reverses it. The group's own state is reported either way, so the
// status keeps showing which group is down. Both hooks run the membership change
// and the zone hook under hookMu — see applyZoneState.
func (s *DefaultServer) poolMemberCallbacks(
	pool *resolverPool,
	member int,
	nsGroup *nbdns.NameServerGroup,
	zoneDeactivate func(error),
	zoneReactivate func(),
) (deactivate func(error), reactivate func()) {
	deactivate = func(err error) {
		// The group's own domains matter here: the same group can sit in several
		// pools, one per zone, each with its own health.
		nsGroupLogger(nsGroup).WithField("zone", pool.domain).
			Info("Temporarily removing nameserver group from the pool")
		s.updateNSState(nsGroup, err, false)

		pool.hookMu.Lock()
		defer pool.hookMu.Unlock()

		pool.setMember(member, false)
		pool.applyZoneState(err, zoneDeactivate, zoneReactivate)
	}

	reactivate = func() {
		s.updateNSState(nsGroup, nil, true)

		pool.hookMu.Lock()
		defer pool.hookMu.Unlock()

		pool.setMember(member, true)
		pool.applyZoneState(nil, zoneDeactivate, zoneReactivate)
	}

	return deactivate, reactivate
}
