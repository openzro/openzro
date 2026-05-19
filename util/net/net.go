package net

import (
	"fmt"
	"math/big"
	"net"
	"net/netip"

	"github.com/google/uuid"
)

const (
	// ControlPlaneMark is the fwmark value used to mark packets that should not be routed through the Openzro interface to
	// avoid routing loops.
	// This includes all control plane traffic (mgmt, signal, flows), relay, ICE/stun/turn and everything that is emitted by the wireguard socket.
	// It doesn't collide with the other marks, as the others are used for data plane traffic only.
	ControlPlaneMark = 0x1BD00

	// Data plane marks (0x1BD10 - 0x1BDFF)

	// DataPlaneMarkLower is the lowest value for the data plane range
	DataPlaneMarkLower = 0x1BD10
	// DataPlaneMarkUpper is the highest value for the data plane range
	DataPlaneMarkUpper = 0x1BDFF

	// DataPlaneMarkIn is the mark for inbound data plane traffic.
	DataPlaneMarkIn = 0x1BD10

	// DataPlaneMarkOut is the mark for outbound data plane traffic.
	DataPlaneMarkOut = 0x1BD11

	// PreroutingFwmarkRedirected is applied to packets that are were redirected (input -> forward, e.g. by Docker or Podman) for special handling.
	PreroutingFwmarkRedirected = 0x1BD20

	// PreroutingFwmarkMasquerade is applied to packets that arrive from the Openzro interface and should be masqueraded.
	PreroutingFwmarkMasquerade = 0x1BD21

	// PreroutingFwmarkMasqueradeReturn is applied to packets that will leave through the Openzro interface and should be masqueraded.
	PreroutingFwmarkMasqueradeReturn = 0x1BD22

	// Bit layout of the ct mark / fwmark when openzro is the writer
	// (per ADR-0013):
	//
	//   bit 31 ............ 17 | bit 16 ........... 0
	//   [    rule_index      ] | [ legacy mark space ]
	//        15 bits                  17 bits
	//
	// Bits 0-16 carry the constants above unchanged. Bits 17-31
	// are reserved for an in-process index that the agent assigns
	// to each peer ACL rule, so the netlink-based conntrack
	// collector can correlate a flow event back to the originating
	// PolicyID. Routing / masquerade / control-plane writers leave
	// the high bits zero, in which case `MarkRuleIndex(mark) == 0`
	// and the collector emits an empty RuleId — matching the
	// behavior before ADR-0013.

	// MarkValueMask isolates the legacy 17-bit fwmark space.
	MarkValueMask uint32 = 0x0001FFFF

	// RuleIndexMask isolates the 15-bit rule_index that ACL rules
	// stamp on packets they match.
	RuleIndexMask uint32 = 0xFFFE0000

	// RuleIndexShift is the bit position where rule_index starts.
	RuleIndexShift uint32 = 17

	// MaxRuleIndex is the largest rule_index that fits in the
	// reserved 15 bits. Index 0 is a sentinel meaning "no rule
	// associated" and is therefore never handed out.
	MaxRuleIndex uint32 = (RuleIndexMask >> RuleIndexShift) - 1
)

// IsDataPlaneMark determines if a fwmark is in the data plane range (0x1BD10-0x1BDFF).
// The rule_index in the upper bits (per ADR-0013) is masked off before the
// range check so a rule-stamped mark like 0x000A_0000_001BD10 still classifies
// as data-plane.
func IsDataPlaneMark(fwmark uint32) bool {
	v := MarkValue(fwmark)
	return v >= DataPlaneMarkLower && v <= DataPlaneMarkUpper
}

// MarkValue returns the legacy 17-bit fwmark portion (DataPlaneMarkIn,
// ControlPlaneMark, PreroutingFwmark*, …). Use this whenever you want
// to compare a mark against one of the existing constants — the bare
// value may carry a rule_index in its upper bits.
func MarkValue(fwmark uint32) uint32 {
	return fwmark & MarkValueMask
}

// MarkRuleIndex returns the in-process index the agent assigned to
// the matched ACL rule, or 0 when no rule stamped the packet.
// Callers resolve the index back to a PolicyID through the ACL
// manager's PolicyResolver interface.
func MarkRuleIndex(fwmark uint32) uint32 {
	return (fwmark & RuleIndexMask) >> RuleIndexShift
}

// ComposeRuleMark returns `base | (ruleIndex << RuleIndexShift)`,
// clamping the index to MaxRuleIndex. Used by the firewall backends
// to stamp rule_index onto a base mark when installing rules.
func ComposeRuleMark(base uint32, ruleIndex uint32) uint32 {
	if ruleIndex == 0 || ruleIndex > MaxRuleIndex {
		return base
	}
	return (base & MarkValueMask) | (ruleIndex << RuleIndexShift)
}

// ConnectionID provides a globally unique identifier for network connections.
// It's used to track connections throughout their lifecycle so the close hook can correlate with the dial hook.
type ConnectionID string

type AddHookFunc func(connID ConnectionID, IP net.IP) error
type RemoveHookFunc func(connID ConnectionID) error

// GenerateConnID generates a unique identifier for each connection.
func GenerateConnID() ConnectionID {
	return ConnectionID(uuid.NewString())
}

func GetLastIPFromNetwork(network netip.Prefix, fromEnd int) (netip.Addr, error) {
	var endIP net.IP
	addr := network.Addr().AsSlice()
	mask := net.CIDRMask(network.Bits(), len(addr)*8)

	for i := 0; i < len(addr); i++ {
		endIP = append(endIP, addr[i]|^mask[i])
	}

	// convert to big.Int
	endInt := big.NewInt(0)
	endInt.SetBytes(endIP)

	// subtract fromEnd from the last ip
	fromEndBig := big.NewInt(int64(fromEnd))
	resultInt := big.NewInt(0)
	resultInt.Sub(endInt, fromEndBig)

	ip, ok := netip.AddrFromSlice(resultInt.Bytes())
	if !ok {
		return netip.Addr{}, fmt.Errorf("invalid IP address from network %s", network)
	}

	return ip.Unmap(), nil
}
