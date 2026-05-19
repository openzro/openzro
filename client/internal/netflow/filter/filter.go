// Package filter implements the client-side port filter applied to
// every conntrack event before it lands on the FlowLogger send queue.
//
// Default behavior: drop a small built-in list of ports known to
// generate "device discovery" noise on a typical corporate network —
// SSDP, mDNS, NetBIOS, LLMNR. Operators who actually want to see
// those events flip ExtraSettings.FlowDisableDefaultPortFilter on the
// account; operators with extra protocols generating uninteresting
// events extend the list via ExtraSettings.FlowExcludedPorts.
//
// The filter mirrors the management-side ExtraSettings.FlowExcludedPorts
// shape: each entry is a (port, protocol) pair, where protocol is one
// of "tcp", "udp", or "any" / "" (matches both). Comparison is case-
// insensitive on protocol; case-folding happens once at construction
// time so the hot path is a flat map lookup.
package filter

import (
	"strings"

	nftypes "github.com/openzro/openzro/client/internal/netflow/types"
)

// DefaultExcluded is the built-in skip list. Picked from RFC and
// IANA assignments for what is universally "discovery noise" on a
// corporate VPN: nothing that a security audit would want to see,
// nothing that a routing decision could plausibly hinge on.
//
//   - 137 udp   NetBIOS Name Service
//   - 138 udp   NetBIOS Datagram Service
//   - 1900 udp  SSDP / UPnP
//   - 5353 udp  mDNS (Bonjour, Avahi)
//   - 5355 udp  LLMNR
//
// 67/68 (DHCP), 53 (DNS), 123 (NTP), 5060 (SIP) deliberately stay
// off the list — they're real connections an operator might want
// to audit. Add them via FlowExcludedPorts when needed.
var DefaultExcluded = []nftypes.FlowPortFilter{
	{Port: 137, Protocol: "udp"},
	{Port: 138, Protocol: "udp"},
	{Port: 1900, Protocol: "udp"},
	{Port: 5353, Protocol: "udp"},
	{Port: 5355, Protocol: "udp"},
}

// Filter answers "should this event be dropped?" based on a compiled
// (port, protocol) set. Constructed once per FlowConfig update; the
// hot path is a single map lookup with no allocations.
type Filter struct {
	// keyed by (port << 8) | protocolID where protocolID is the
	// nftypes.Protocol byte. A zero-value entry matches "any
	// protocol on this port". We never insert a zero-port entry
	// (port 0 is invalid for the conntrack events we're filtering).
	exact map[uint32]struct{}
	any   map[uint16]struct{}
}

// New compiles a Filter from the operator's configuration. When
// disableDefault=true, the built-in list is dropped and only `extra`
// applies. When disableDefault=false (the typical case), the built-in
// list runs alongside `extra` and an event matching either gets
// dropped.
func New(disableDefault bool, extra []nftypes.FlowPortFilter) *Filter {
	f := &Filter{
		exact: make(map[uint32]struct{}),
		any:   make(map[uint16]struct{}),
	}
	if !disableDefault {
		for _, p := range DefaultExcluded {
			f.add(p)
		}
	}
	for _, p := range extra {
		f.add(p)
	}
	return f
}

// Excludes reports whether an event should be dropped. proto is the
// L4 protocol number (TCP=6, UDP=17 — see nftypes.Protocol); port is
// the destination port of the event (we filter by destination because
// the source port is ephemeral and not stable enough to be a sensible
// filter key).
func (f *Filter) Excludes(proto nftypes.Protocol, port uint16) bool {
	if f == nil {
		return false
	}
	if port == 0 {
		return false
	}
	if _, ok := f.any[port]; ok {
		return true
	}
	key := keyOf(port, proto)
	_, ok := f.exact[key]
	return ok
}

func (f *Filter) add(p nftypes.FlowPortFilter) {
	if p.Port == 0 {
		return
	}
	switch normProtocol(p.Protocol) {
	case "tcp":
		f.exact[keyOf(p.Port, nftypes.TCP)] = struct{}{}
	case "udp":
		f.exact[keyOf(p.Port, nftypes.UDP)] = struct{}{}
	default: // "any" or empty
		f.any[p.Port] = struct{}{}
	}
}

func keyOf(port uint16, proto nftypes.Protocol) uint32 {
	return uint32(port)<<8 | uint32(proto)
}

func normProtocol(p string) string {
	return strings.ToLower(strings.TrimSpace(p))
}
