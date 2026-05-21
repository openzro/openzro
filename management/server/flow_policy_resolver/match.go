package flow_policy_resolver

import (
	"net/netip"
)

// policyCandidate is one (policy, rule) pair the resolver checks
// against an event. A peer appears in multiple candidates when it
// is on the source side of multiple policies — each rule that
// could attribute its outbound traffic produces one entry.
//
// candidates are sorted by policy creation time (older first) so
// the first match wins, mirroring the dataplane's ordering.
type policyCandidate struct {
	policyID string

	// dstIPs is the set of single IPs in destination groups (peers
	// + Host resources). Cheap pointer-compare on membership.
	dstIPs map[netip.Addr]struct{}

	// dstPrefixes is the set of CIDRs in destination groups (Subnet
	// resources). Linear scan; typical accounts have <10 prefixes
	// per candidate so this is fine.
	dstPrefixes []netip.Prefix

	// ports is the union of single ports and ranges the rule
	// allows. Empty slice means "any port".
	ports []portRange

	// protocol is the rule's protocol filter. protoAny means any.
	protocol proto
}

// proto is a uint8 mirror of types.PolicyRuleProtocolType so the hot
// path stays branch-friendly. Values match the IANA numbers the
// netflow collector already emits.
type proto uint8

const (
	protoAny  proto = 0 // matches anything (PolicyRuleProtocolALL)
	protoICMP proto = 1
	protoTCP  proto = 6
	protoUDP  proto = 17
)

// portRange is a half-open [Start, End] interval in network byte
// space. The dataplane matches inclusive on both ends, so we follow.
type portRange struct {
	Start uint16
	End   uint16
}

// matches returns true when (dst, port, p) satisfies this
// candidate's predicates. Ordering: protocol → port → destination,
// cheapest checks first.
func (c *policyCandidate) matches(dst netip.Addr, port uint32, p uint16) bool {
	if !matchProto(c.protocol, proto(p)) {
		return false
	}
	if !matchPort(c.ports, uint16(port)) {
		return false
	}
	return c.matchDest(dst)
}

// matchProto returns true when filter matches actual, with
// protoAny acting as wildcard.
func matchProto(filter, actual proto) bool {
	return filter == protoAny || filter == actual
}

// matchPort returns true when port is in any of the ranges, or when
// the rule had no port filter (empty slice).
func matchPort(ports []portRange, port uint16) bool {
	if len(ports) == 0 {
		return true
	}
	for _, r := range ports {
		if port >= r.Start && port <= r.End {
			return true
		}
	}
	return false
}

// matchDest checks the destination IP against the candidate's
// destination set (single IPs + CIDR prefixes).
func (c *policyCandidate) matchDest(dst netip.Addr) bool {
	if _, ok := c.dstIPs[dst]; ok {
		return true
	}
	for _, prefix := range c.dstPrefixes {
		if prefix.Contains(dst) {
			return true
		}
	}
	return false
}
