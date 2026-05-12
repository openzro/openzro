// Package flow_policy_resolver fills the RuleId field on flow events
// that the agent could not stamp at firewall time. See ADR-0018 for
// the design.
//
// The resolver runs only when FlowEvent.RuleId is empty (the agent's
// kernel-firewall path could not stamp it — typically outbound flows
// from a Linux peer in initiator role). Events that already carry a
// rule_id pass through untouched.
//
// Lookup is O(1) hash on peer_id to fetch the peer's candidate list,
// then a linear scan of the (typically 5-50) candidates with cheap
// predicate checks. Memory cost is bounded by the candidate-list
// shape: the same []policyCandidate slice would be repeated across
// peers in the same source group, so we keep a single shared backing
// array per source group in v1 to avoid the cartesian product.
//
// Invalidation is driven by the existing Account.Manager event
// surface: any time the management would re-compute network maps
// (policy save, peer add/remove, group membership change, resource
// edit), the resolver rebuilds the affected account's index.
package flow_policy_resolver

import (
	"net/netip"
	"sync"

	"github.com/openzro/openzro/flow/store"
)

// Resolver fills FlowEvent.RuleId server-side. Construct with New().
// Methods are safe for concurrent use; the hot path holds an RLock
// while reading the per-account index.
type Resolver struct {
	mu     sync.RWMutex
	cache  map[string]*accountIndex // accountID → index
}

// New returns a Resolver with no accounts cached. Callers must
// invoke Rebuild(accountID, *types.Account) at least once before
// the resolver can serve queries for that account.
func New() *Resolver {
	return &Resolver{
		cache: make(map[string]*accountIndex),
	}
}

// Resolve attempts to fill an empty RuleId on the given event by
// looking up the (peer_id, dst_ip, dst_port, protocol) tuple against
// the cached policy graph. Returns the event unchanged when:
//
//   - the event already has a RuleId set (agent stamped it; primary
//     path), OR
//   - the resolver has no index for the account, OR
//   - no candidate policy matches the tuple.
//
// Mutates the event in place when a match is found, then returns it.
// The bool reports whether a match was applied.
func (r *Resolver) Resolve(accountID string, e *store.Event) bool {
	if e == nil {
		return false
	}
	if len(e.RuleID) != 0 {
		return false
	}
	if accountID == "" || e.PeerID == "" {
		return false
	}

	r.mu.RLock()
	idx := r.cache[accountID]
	r.mu.RUnlock()
	if idx == nil {
		return false
	}

	candidates := idx.candidatesForPeer(e.PeerID)
	if len(candidates) == 0 {
		return false
	}

	dst, ok := parseIP(e.DestIP)
	if !ok {
		return false
	}

	for _, c := range candidates {
		if !c.matches(dst, e.DestPort, e.Protocol) {
			continue
		}
		e.RuleID = []byte(c.policyID)
		return true
	}
	return false
}

// Forget drops the cached index for an account. Used when an account
// is deleted; lets us reclaim memory without waiting for restart.
func (r *Resolver) Forget(accountID string) {
	r.mu.Lock()
	delete(r.cache, accountID)
	r.mu.Unlock()
}

// parseIP turns the wire-shape IP string back into a netip.Addr.
// store.Event holds destinations as strings (the DTO layer formats
// them); the resolver matches in netip space for cheap CIDR
// containment checks.
func parseIP(s string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return a, true
}
