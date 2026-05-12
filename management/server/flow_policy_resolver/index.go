package flow_policy_resolver

import (
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/openzro/openzro/management/server/types"
)

// accountIndex is the per-account reverse map from a source peer to
// the policies that could attribute its outbound traffic. Built
// lazily via Rebuild and replaced atomically (copy-on-write) on every
// account graph change so the hot path's RLock never blocks a writer.
type accountIndex struct {
	// byPeer is the lookup table. The slice is sorted by policy
	// creation time so the first match in resolver.Resolve also
	// matches what the dataplane does at packet time.
	byPeer map[string][]*policyCandidate
}

// candidatesForPeer returns the (possibly nil) slice of candidates
// where peer is on the source side. Read-only — caller MUST NOT
// mutate the slice.
func (a *accountIndex) candidatesForPeer(peerID string) []*policyCandidate {
	if a == nil {
		return nil
	}
	return a.byPeer[peerID]
}

// Rebuild reconstructs the resolver's index for the given account.
// Called by the management's account event hooks whenever the graph
// changes (policy save / delete, peer add / remove, group edit,
// resource edit). Cheap to run — typical Cora-class account is well
// under 100ms — and atomic for readers: the new index swaps in
// under the resolver's write lock, no half-built state visible.
func (r *Resolver) Rebuild(accountID string, account *types.Account) {
	if account == nil {
		r.Forget(accountID)
		return
	}
	idx := buildAccountIndex(account)

	r.mu.Lock()
	r.cache[accountID] = idx
	r.mu.Unlock()
}

// buildAccountIndex is the pure constructor — no Resolver state, no
// locks. Lives outside Rebuild so tests can exercise the shape
// without a Resolver instance.
func buildAccountIndex(account *types.Account) *accountIndex {
	idx := &accountIndex{
		byPeer: make(map[string][]*policyCandidate),
	}
	if account == nil {
		return idx
	}

	// Precompute group → []peerID and group → []destination once
	// per build. Both are read N × M times during candidate
	// expansion so the upfront map is cheaper than chasing pointers
	// through Account.Groups + Account.Peers per rule.
	groupPeers := make(map[string][]string, len(account.Groups))
	groupDestIPs := make(map[string][]netip.Addr, len(account.Groups))
	groupDestPrefixes := make(map[string][]netip.Prefix, len(account.Groups))
	for gid, g := range account.Groups {
		if g == nil {
			continue
		}
		groupPeers[gid] = append([]string(nil), g.Peers...)
		for _, pid := range g.Peers {
			peer := account.Peers[pid]
			if peer == nil || peer.IP == nil {
				continue
			}
			addr, ok := netip.AddrFromSlice(peer.IP)
			if !ok {
				continue
			}
			addr = addr.Unmap()
			groupDestIPs[gid] = append(groupDestIPs[gid], addr)
		}
	}
	for _, res := range account.NetworkResources {
		if res == nil {
			continue
		}
		addr, prefix, ok := resourceDestination(res.Prefix)
		if !ok {
			continue
		}
		for _, gid := range res.GroupIDs {
			if addr.IsValid() {
				groupDestIPs[gid] = append(groupDestIPs[gid], addr)
			} else if prefix.IsValid() {
				groupDestPrefixes[gid] = append(groupDestPrefixes[gid], prefix)
			}
		}
	}

	// Order policies for deterministic ambiguity-resolution. The
	// dataplane attributes the first matching rule it built; we
	// match that by sorting on policy ID (which embeds the xid
	// timestamp) ascending — same proxy for creation order the
	// rest of the management uses elsewhere.
	policies := append([]*types.Policy(nil), account.Policies...)
	sort.SliceStable(policies, func(i, j int) bool {
		return policies[i].ID < policies[j].ID
	})

	for _, policy := range policies {
		if policy == nil {
			continue
		}
		for _, rule := range policy.Rules {
			if rule == nil || !rule.Enabled {
				continue
			}
			if rule.Action != types.PolicyTrafficActionAccept {
				// Drop rules never produce a useful "policy that
				// allowed this connection" attribution.
				continue
			}

			cand := buildCandidate(policy.ID, rule, groupDestIPs, groupDestPrefixes)
			if cand == nil {
				continue
			}

			// Wire the candidate to every peer on the rule's
			// source side. We attach by peer ID so the hot path is
			// O(1) without a Groups indirection.
			for _, srcGroup := range rule.Sources {
				for _, peerID := range groupPeers[srcGroup] {
					idx.byPeer[peerID] = append(idx.byPeer[peerID], cand)
				}
			}
		}
	}

	return idx
}

// buildCandidate assembles a single (policy, rule) entry. Returns
// nil when the rule has no resolvable destination (the dashboard
// doesn't need to attribute traffic to "no destination").
func buildCandidate(policyID string, rule *types.PolicyRule,
	groupDestIPs map[string][]netip.Addr,
	groupDestPrefixes map[string][]netip.Prefix,
) *policyCandidate {
	c := &policyCandidate{
		policyID: policyID,
		dstIPs:   make(map[netip.Addr]struct{}),
		protocol: protoFromRule(rule.Protocol),
		ports:    portsFromRule(rule.Ports, rule.PortRanges),
	}

	for _, gid := range rule.Destinations {
		for _, addr := range groupDestIPs[gid] {
			c.dstIPs[addr] = struct{}{}
		}
		c.dstPrefixes = append(c.dstPrefixes, groupDestPrefixes[gid]...)
	}

	if rule.DestinationResource.ID != "" {
		// Resource on the rule directly (not via group). The
		// resource was already resolved into the destination
		// helpers above when we walked NetworkResources; nothing
		// to add here, the per-resource expansion handles it.
	}

	if len(c.dstIPs) == 0 && len(c.dstPrefixes) == 0 {
		return nil
	}
	return c
}

// resourceDestination converts a NetworkResource's Prefix into the
// kind of destination the matcher uses. Single-IP prefixes (/32
// IPv4, /128 IPv6) become explicit dstIPs entries; broader prefixes
// stay as CIDRs.
func resourceDestination(prefix netip.Prefix) (netip.Addr, netip.Prefix, bool) {
	if !prefix.IsValid() {
		return netip.Addr{}, netip.Prefix{}, false
	}
	if prefix.IsSingleIP() {
		return prefix.Addr(), netip.Prefix{}, true
	}
	return netip.Addr{}, prefix, true
}

// protoFromRule maps the string-typed PolicyRuleProtocolType to the
// proto enum the matcher uses.
func protoFromRule(p types.PolicyRuleProtocolType) proto {
	switch p {
	case types.PolicyRuleProtocolALL:
		return protoAny
	case types.PolicyRuleProtocolTCP:
		return protoTCP
	case types.PolicyRuleProtocolUDP:
		return protoUDP
	case types.PolicyRuleProtocolICMP:
		return protoICMP
	}
	return protoAny
}

// portsFromRule normalises the rule's Ports (single ports as strings)
// + PortRanges into a slice of portRange. Invalid entries are
// silently dropped — the dataplane treats them the same way, so the
// resolver matches that behaviour.
func portsFromRule(ports []string, ranges []types.RulePortRange) []portRange {
	out := make([]portRange, 0, len(ports)+len(ranges))
	for _, p := range ports {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			continue
		}
		out = append(out, portRange{Start: uint16(n), End: uint16(n)})
	}
	for _, r := range ranges {
		if r.Start == 0 || r.End == 0 || r.End < r.Start {
			continue
		}
		out = append(out, portRange{Start: r.Start, End: r.End})
	}
	return out
}
