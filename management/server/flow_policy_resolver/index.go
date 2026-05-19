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
// resource edit). Cheap to run — typical medium-tier account is well
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

// groupExpansion is the precomputed shape for each group used during
// index build: peer IDs in the group (for attaching candidates to
// peers on the source side) and the destination set the group
// produces (peer mesh IPs + resource CIDRs/IPs).
type groupExpansion struct {
	peers    []string
	destIPs  []netip.Addr
	prefixes []netip.Prefix
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

	groups := buildGroupExpansion(account)
	resourceByID := indexResourcesByID(account)

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
		if policy == nil || !policy.Enabled {
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

			// Build forward direction: source-side peers can reach
			// destination peers / resources.
			fwdSources := peersOnSide(rule.Sources, groups)
			fwdDestIPs, fwdPrefixes := destinationsForRule(rule.Destinations, rule.DestinationResource, groups, resourceByID)
			if len(fwdSources) > 0 && (len(fwdDestIPs) > 0 || len(fwdPrefixes) > 0) {
				cand := newCandidate(policy.ID, rule, fwdDestIPs, fwdPrefixes)
				attach(idx, fwdSources, cand)
			}

			// Bidirectional rules install reverse-direction firewall
			// entries too: peers in destinations can initiate to
			// source peers. Mirrors account.go:998 in the dataplane.
			// Note: posture checks (policy.SourcePostureChecks) gate
			// who is on the original source side — peers that fail
			// posture are excluded from the reverse direction's
			// destinations as well, because the dataplane never
			// installs a rule to them. We document this caveat at
			// the resolver entry-point; posture evaluation in the
			// index builder is a planned follow-up.
			if rule.Bidirectional {
				revSources := peersOnSide(rule.Destinations, groups)
				revDestIPs, revPrefixes := destinationsForRule(rule.Sources, rule.SourceResource, groups, resourceByID)
				if len(revSources) > 0 && (len(revDestIPs) > 0 || len(revPrefixes) > 0) {
					cand := newCandidate(policy.ID, rule, revDestIPs, revPrefixes)
					attach(idx, revSources, cand)
				}
			}
		}
	}

	return idx
}

// buildGroupExpansion precomputes the per-group peer list AND the
// destination set the group contributes (peer mesh IPs + resource
// CIDRs/IPs). Done once per Rebuild so the inner loops don't
// re-walk Account.Peers / NetworkResources per rule.
func buildGroupExpansion(account *types.Account) map[string]*groupExpansion {
	groups := make(map[string]*groupExpansion, len(account.Groups))
	for gid, g := range account.Groups {
		if g == nil {
			continue
		}
		exp := &groupExpansion{
			peers: append([]string(nil), g.Peers...),
		}
		for _, pid := range g.Peers {
			peer := account.Peers[pid]
			if peer == nil || peer.IP == nil {
				continue
			}
			addr, ok := netip.AddrFromSlice(peer.IP)
			if !ok {
				continue
			}
			exp.destIPs = append(exp.destIPs, addr.Unmap())
		}
		groups[gid] = exp
	}
	// Fold NetworkResources into the groups they belong to. Resources
	// are referenced from policy rules either directly (rule.Destination
	// Resource / rule.SourceResource) or via groups (resource.GroupIDs);
	// the via-groups path is the dominant one.
	for _, res := range account.NetworkResources {
		if res == nil || !res.Enabled {
			continue
		}
		addr, prefix, ok := resourceDestination(res.Prefix)
		if !ok {
			continue
		}
		for _, gid := range res.GroupIDs {
			exp, exists := groups[gid]
			if !exists {
				exp = &groupExpansion{}
				groups[gid] = exp
			}
			if addr.IsValid() {
				exp.destIPs = append(exp.destIPs, addr)
			} else if prefix.IsValid() {
				exp.prefixes = append(exp.prefixes, prefix)
			}
		}
	}
	return groups
}

// indexResourcesByID returns NetworkResources keyed by ID so the
// direct-reference rule.DestinationResource / rule.SourceResource
// paths can look up CIDRs in O(1).
func indexResourcesByID(account *types.Account) map[string]*resourceDest {
	out := make(map[string]*resourceDest, len(account.NetworkResources))
	for _, res := range account.NetworkResources {
		if res == nil || !res.Enabled {
			continue
		}
		addr, prefix, ok := resourceDestination(res.Prefix)
		if !ok {
			continue
		}
		d := &resourceDest{}
		if addr.IsValid() {
			d.addr = addr
		} else if prefix.IsValid() {
			d.prefix = prefix
		}
		out[res.ID] = d
	}
	return out
}

// resourceDest is the simplified destination shape for a resource:
// either a single IP (Host) or a CIDR prefix (Subnet). Domain
// resources currently land empty — the management resolves them at
// network-map time and the resolver doesn't have the resolved IPs
// in scope. Documented as a known limitation in ADR-0018.
type resourceDest struct {
	addr   netip.Addr
	prefix netip.Prefix
}

// peersOnSide returns the union of peer IDs across a slice of group
// IDs. Mirrors getUniquePeerIDsFromGroupsIDs in the dataplane but
// avoids the GetGroup indirection per call.
func peersOnSide(groupIDs []string, groups map[string]*groupExpansion) []string {
	if len(groupIDs) == 0 {
		return nil
	}
	if len(groupIDs) == 1 {
		// Short-circuit matches account.go:1467: a single group
		// reference returns its peer list verbatim.
		exp := groups[groupIDs[0]]
		if exp == nil {
			return nil
		}
		return exp.peers
	}
	seen := make(map[string]struct{})
	var out []string
	for _, gid := range groupIDs {
		exp := groups[gid]
		if exp == nil {
			continue
		}
		for _, pid := range exp.peers {
			if _, dup := seen[pid]; dup {
				continue
			}
			seen[pid] = struct{}{}
			out = append(out, pid)
		}
	}
	return out
}

// destinationsForRule produces the union of destination IPs and
// CIDRs for a rule's destination side: groups + an optional direct
// resource reference. Mirrors what the dataplane installs as
// FirewallRule entries pointing at this rule's destinations.
//
// The signature is parameterised on (groupIDs, directResource) so
// the same helper serves both forward (rule.Destinations,
// rule.DestinationResource) and reverse (rule.Sources,
// rule.SourceResource) directions of a bidirectional rule.
func destinationsForRule(groupIDs []string, directResource types.Resource,
	groups map[string]*groupExpansion, resourcesByID map[string]*resourceDest,
) ([]netip.Addr, []netip.Prefix) {
	var ips []netip.Addr
	var prefixes []netip.Prefix

	for _, gid := range groupIDs {
		exp := groups[gid]
		if exp == nil {
			continue
		}
		ips = append(ips, exp.destIPs...)
		prefixes = append(prefixes, exp.prefixes...)
	}

	if directResource.ID != "" {
		if d := resourcesByID[directResource.ID]; d != nil {
			if d.addr.IsValid() {
				ips = append(ips, d.addr)
			} else if d.prefix.IsValid() {
				prefixes = append(prefixes, d.prefix)
			}
		}
	}

	return ips, prefixes
}

// newCandidate builds a single policyCandidate from a rule plus
// pre-resolved destinations. The protocol filter + port set come
// straight from the rule; deduplication happens via the dstIPs map
// (a peer that appears in multiple destination groups produces only
// one entry).
func newCandidate(policyID string, rule *types.PolicyRule, ips []netip.Addr, prefixes []netip.Prefix) *policyCandidate {
	c := &policyCandidate{
		policyID:    policyID,
		dstIPs:      make(map[netip.Addr]struct{}, len(ips)),
		dstPrefixes: append([]netip.Prefix(nil), prefixes...),
		protocol:    protoFromRule(rule.Protocol),
		ports:       portsFromRule(rule.Ports, rule.PortRanges),
	}
	for _, ip := range ips {
		c.dstIPs[ip] = struct{}{}
	}
	return c
}

// attach wires a candidate to every peer on a given side of a rule.
// The same candidate pointer is shared across peers — no copy, no
// per-peer allocation beyond the slice header.
func attach(idx *accountIndex, peers []string, cand *policyCandidate) {
	for _, pid := range peers {
		if pid == "" {
			continue
		}
		idx.byPeer[pid] = append(idx.byPeer[pid], cand)
	}
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
// resolver matches that behavior.
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
