// Package exporter ships activity events to external systems (SIEMs,
// log aggregators, generic webhooks). All exporters are best-effort:
// they run off the StoreEvent fire-and-forget goroutine and never
// block the request path. Drops on transport failure are logged at
// Error level so they remain visible without dedicated metrics.
//
// The package is intentionally narrow: one Exporter interface, one
// HTTPWebhook implementation, and a NewFromEnv factory. Adding a
// dedicated exporter for a specific SIEM (Datadog, Splunk HEC, …)
// only requires a new file alongside http.go that implements
// Exporter; the factory and StoreEvent integration do not change.
package exporter

import (
	"context"

	"github.com/openzro/openzro/management/server/activity"
)

// Exporter accepts activity events and ships them somewhere external.
// Implementations MUST NOT block — the caller invokes Export from the
// hot path of StoreEvent's fire-and-forget goroutine, and callers
// upstream of that goroutine are doing real work.
type Exporter interface {
	// Export ships a single event. The implementation is responsible
	// for its own batching, retry, and backoff; a returned error is
	// purely diagnostic and never propagated to the API caller.
	Export(ctx context.Context, event *activity.Event) error

	// Name returns a short identifier used in log lines, e.g.
	// "http-webhook". Stable; do not change once shipped.
	Name() string

	// Close releases resources (HTTP keep-alive pools, in-flight
	// goroutines). Safe to call multiple times.
	Close() error
}
