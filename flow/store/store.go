// Package store is the storage abstraction for traffic flow events.
//
// One Store instance lives per management process. The gRPC handler at
// management/server/flow_service.go delivers FlowEvents into Save();
// the HTTP handlers at /api/network-traffic-events query through
// Query(); a daily cron Purges anything older than the configured
// retention.
//
// The interface lives in this package; concrete backends live in
// sibling subpackages (sql, future clickhouse). A factory at
// flow/store/factory selects the backend at process start from
// environment variables — see ADR-0002 §"HOT tier".
//
// All methods are safe for concurrent use. Save in particular MUST
// scale to thousands of calls per second on the medium tier; backends
// implement bulk inserts where supported.
package store

import (
	"context"
	"errors"
	"time"
)

// ErrNoStore is returned when the operator has explicitly chosen
// engine=none. Callers (gRPC handler, query API) should handle this
// gracefully — for the gRPC handler it means "ack the event but skip
// persistence", for query it means "return empty list".
var ErrNoStore = errors.New("flow store: not configured")

// EventType mirrors flow.proto Type enum without importing the proto
// package — keeps the storage layer free of gRPC types and lets us
// evolve persistence independently.
type EventType uint8

const (
	EventTypeUnknown EventType = 0
	EventTypeStart   EventType = 1
	EventTypeEnd     EventType = 2
	EventTypeDrop    EventType = 3
)

// Direction mirrors flow.proto Direction.
type Direction uint8

const (
	DirectionUnknown Direction = 0
	DirectionIngress Direction = 1
	DirectionEgress  Direction = 2
)

// Event is one flow record at rest. Field shapes match the proto where
// possible; large optional fields use pointers/empty slices so the
// zero value is "not present".
type Event struct {
	// Identity — supplied by the peer
	EventID       []byte // unique client event id
	FlowID        []byte // unique flow session id
	PeerPublicKey []byte
	IsInitiator   bool

	// Routing — set by the management at ingest time
	AccountID  string
	PeerID     string
	OccurredAt time.Time // peer's timestamp on the event
	ReceivedAt time.Time // when the management received it

	// Flow shape
	Type      EventType
	Direction Direction
	Protocol  uint16

	SourceIP   string
	DestIP     string
	SourcePort uint32 // 0 when not applicable (ICMP)
	DestPort   uint32

	ICMPType uint16
	ICMPCode uint16

	RxPackets uint64
	TxPackets uint64
	RxBytes   uint64
	TxBytes   uint64

	RuleID         []byte
	SourceResource []byte
	DestResource   []byte
}

// Filter scopes a Query. AccountID is required. All other fields are
// optional; the zero value of each is "do not filter on this".
type Filter struct {
	AccountID string

	PeerID     string
	UserID     string // filters via the peer's owning user
	SourceIP   string
	DestIP     string
	SourcePort *uint32
	DestPort   *uint32
	Protocol   *uint16
	Type       *EventType
	Direction  *Direction
	RuleID     []byte

	// Time range — both endpoints inclusive of nanoseconds. Either or
	// both may be the zero time, in which case it is treated as
	// "unbounded" on that side.
	Since time.Time
	Until time.Time

	// Pagination. Limit defaults to 100 when zero; backends should cap
	// to a sane maximum (ADR-0002 references 50k as upstream's cap).
	Limit  int
	Offset int
}

// Sink is anywhere flow events land. The hot Store is a Sink;
// streaming SIEM exporters and cold archives are also Sinks. The
// FlowService fans events out to a slice of Sinks, so adding a
// destination is purely additive — no code in the hot path changes.
type Sink interface {
	// Save handles a batch of events. Implementations MUST be
	// non-blocking on the hot path beyond a small bounded buffer:
	// the gRPC handler calls Save off the request goroutine but
	// hundreds of peers may share a Sink, and a slow destination
	// must not back-pressure the others.
	Save(ctx context.Context, events []*Event) error

	// Close releases backend resources.
	Close() error
}

// Store is the queryable Sink that backs the dashboard's
// /api/network-traffic-events page. The factory selects exactly one
// at process start; further destinations come in as Sinks alongside.
type Store interface {
	Sink

	// Query returns events matching the filter, ordered by ReceivedAt
	// descending. Returns an empty slice (not an error) when no rows
	// match.
	Query(ctx context.Context, filter Filter) ([]*Event, error)

	// Purge deletes events older than the cutoff and returns the
	// number deleted. Backends with native partitioning may implement
	// this as DROP PARTITION rather than DELETE for O(1) cost.
	Purge(ctx context.Context, olderThan time.Time) (int64, error)
}
