package types

import (
	"net/netip"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/openzro/openzro/client/iface/wgaddr"
)

const ZoneID = 0x1BD0

type Protocol uint8

const (
	ProtocolUnknown = Protocol(0)
	ICMP            = Protocol(1)
	TCP             = Protocol(6)
	UDP             = Protocol(17)
	SCTP            = Protocol(132)
)

func (p Protocol) String() string {
	switch p {
	case 1:
		return "ICMP"
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	case 132:
		return "SCTP"
	default:
		return strconv.FormatUint(uint64(p), 10)
	}
}

type Type int

const (
	TypeUnknown = Type(iota)
	TypeStart
	TypeEnd
	TypeDrop
)

type Direction int

func (d Direction) String() string {
	switch d {
	case Ingress:
		return "ingress"
	case Egress:
		return "egress"
	default:
		return "unknown"
	}
}

const (
	DirectionUnknown = Direction(iota)
	Ingress
	Egress
)

type Event struct {
	ID        uuid.UUID
	Timestamp time.Time
	EventFields
}

type EventFields struct {
	FlowID           uuid.UUID
	Type             Type
	RuleID           []byte
	Direction        Direction
	Protocol         Protocol
	SourceIP         netip.Addr
	DestIP           netip.Addr
	SourceResourceID []byte
	DestResourceID   []byte
	SourcePort       uint16
	DestPort         uint16
	ICMPType         uint8
	ICMPCode         uint8
	RxPackets        uint64
	TxPackets        uint64
	RxBytes          uint64
	TxBytes          uint64
}

type FlowConfig struct {
	URL                string
	Interval           time.Duration
	Enabled            bool
	Counters           bool
	TokenPayload       string
	TokenSignature     string
	DNSCollection      bool
	ExitNodeCollection bool

	// Groups is the operator-supplied list of group IDs that scope
	// flow capture. When non-empty, the peer only enables capture if
	// its own group memberships intersect this list. Empty (default)
	// means every peer captures while Enabled=true. Set by management
	// from ExtraSettings.FlowEventsGroups (proto field
	// management.FlowConfig.groups).
	Groups []string

	// DisableDefaultPortFilter turns OFF the built-in skip list of
	// broadcast / discovery ports (SSDP-1900, mDNS-5353,
	// NetBIOS-137/138, LLMNR-5355). Default false: a corporate
	// VPN almost never wants those events polluting telemetry.
	DisableDefaultPortFilter bool

	// ExcludedPorts is operator-defined extra (port, protocol) pairs
	// to drop. ADDED to the built-in skip list, or replaces it
	// entirely when DisableDefaultPortFilter is true.
	ExcludedPorts []FlowPortFilter
}

// FlowPortFilter is a (port, protocol) pair the client drops at the
// conntrack-event boundary before queueing for the management.
// Protocol is "tcp" / "udp" / "any" (matches both); compared
// case-insensitively against the IANA-style protocol name.
type FlowPortFilter struct {
	Port     uint16
	Protocol string
}

type FlowManager interface {
	// FlowConfig handles network map updates
	Update(update *FlowConfig) error
	// Close closes the manager
	Close()
	// GetLogger returns a flow logger
	GetLogger() FlowLogger
}

type FlowLogger interface {
	// StoreEvent stores a flow event
	StoreEvent(flowEvent EventFields)
	// GetEvents returns all stored events
	GetEvents() []*Event
	// DeleteEvents deletes events from the store
	DeleteEvents([]uuid.UUID)
	// Close closes the logger
	Close()
	// Enable enables the flow logger receiver
	Enable()
	// UpdateConfig updates the flow manager configuration. portFilter
	// can be nil to disable port filtering (the fresh-Logger default).
	// Caller owns the *filter.Filter lifetime; logger reads atomically.
	UpdateConfig(dnsCollection, exitNodeCollection bool, portFilter PortFilter)
}

// PortFilter is the interface the logger consumes to decide whether
// an event should be dropped before reaching the store. Concrete
// implementation lives in client/internal/netflow/filter to avoid an
// import cycle (filter imports types, types must not import filter).
type PortFilter interface {
	Excludes(proto Protocol, port uint16) bool
}

type Store interface {
	// StoreEvent stores a flow event
	StoreEvent(event *Event)
	// GetEvents returns all stored events
	GetEvents() []*Event
	// DeleteEvents deletes events from the store
	DeleteEvents([]uuid.UUID)
	// Close closes the store
	Close()
}

// ConnTracker defines the interface for connection tracking functionality
type ConnTracker interface {
	// Start begins tracking connections by listening for conntrack events.
	Start(bool) error
	// Stop stops the connection tracking.
	Stop()
	// Close stops listening for events and cleans up resources
	Close() error
}

// IFaceMapper provides interface to check if we're using userspace WireGuard
type IFaceMapper interface {
	IsUserspaceBind() bool
	Name() string
	Address() wgaddr.Address
}
