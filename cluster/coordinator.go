// Package cluster provides distributed coordination for openzro components
// running in HA mode (multiple instances of management or signal sharing
// state across a network).
//
// The Coordinator interface abstracts two primitives both components need
// from a broker: distributed locks and pub/sub. Implementations live in
// sibling packages (cluster/redis, cluster/nats); the embedded NATS server
// bootstrap is in cluster/embedded for deployments that prefer not to run
// an external broker.
//
// HA broker is a hard requirement for multi-instance deployments — see
// ADR-0001 §3.4 for the rationale.
//
// The package is openzro-original; nothing here was copied from any
// post-AGPL upstream code.
package cluster

import (
	"context"
	"errors"
)

// Event is a single message delivered through Subscribe.
type Event struct {
	Topic   string
	Payload []byte
}

// Coordinator is the contract every distributed-coordination backend
// satisfies. Implementations are go-routine safe.
//
// Lifecycle: callers construct one Coordinator per process at startup
// and reuse it. Close shuts the coordinator down; in-flight Lock/Subscribe
// calls return an error after Close.
type Coordinator interface {
	// Lock acquires a named exclusive distributed lock. Blocks until the
	// lock is held or ctx is canceled. The returned release function
	// MUST be called to free the lock (typically via defer). Releasing
	// twice is a no-op, not an error.
	//
	// Implementations protect against a holder crashing without
	// releasing — typically through a TTL renewed by an internal
	// heartbeat. The lock name should be a stable string that all
	// participants compute the same way (e.g. "account:" + accountID).
	Lock(ctx context.Context, name string) (release func(), err error)

	// Publish sends payload to every subscriber of topic. Best-effort:
	// implementations do not retry on broker failures; callers handle
	// errors at the call site. Returns when the broker has accepted
	// the message for delivery (not when subscribers have processed it).
	Publish(ctx context.Context, topic string, payload []byte) error

	// Subscribe returns a channel that receives every event published
	// to topic from now until ctx is canceled. The channel is closed
	// when ctx is canceled or Close is called on the coordinator.
	// At-most-once delivery: if a subscriber falls behind, events MAY
	// be dropped (with a warning log) rather than blocking publishers.
	Subscribe(ctx context.Context, topic string) (<-chan Event, error)

	// Close releases all resources held by the coordinator. After Close
	// returns, all method calls error and all Subscribe channels are
	// closed. Idempotent.
	Close() error
}

// ErrClosed is returned from coordinator methods after Close.
var ErrClosed = errors.New("cluster coordinator is closed")
