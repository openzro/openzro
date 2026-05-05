package cluster

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// Metrics groups the OpenTelemetry instruments the inter-pod fabric
// emits. A nil *Metrics is a valid receiver for every method so the
// hot-path call sites can stay free of nil-checks; tests pass nil
// to skip metrics entirely.
//
// The metric names follow the relay_cluster_* prefix to namespace
// them clearly against the existing relay_* counters.
type Metrics struct {
	meter metric.Meter

	forwards      metric.Int64Counter
	lookupLatency metric.Float64Histogram
	helloRejects  metric.Int64Counter
	pingsSent     metric.Int64Counter
	pingsLost     metric.Int64Counter

	// streams is wired via an ObservableGauge whose callback
	// reads from a snapshotter. The transport calls SetStreamSource
	// once at startup so the gauge can probe it on every scrape.
	streamsGauge   metric.Int64ObservableGauge
	streamSource   func() int
	registration   metric.Registration
}

// ForwardResult labels a forward attempt's outcome on the
// relay_cluster_forwards_total counter.
type ForwardResult string

const (
	ForwardResultOK            ForwardResult = "ok"
	ForwardResultPeerNotFound  ForwardResult = "peer_not_found"
	ForwardResultStreamGone    ForwardResult = "stream_gone"
	ForwardResultSendError     ForwardResult = "send_error"
)

// HelloRejectReason labels a HELLO that was dropped at handshake.
// Exported so security dashboards can split out hmac/stale/etc. as
// separate alert rules.
type HelloRejectReason string

const (
	HelloRejectHMAC       HelloRejectReason = "hmac_mismatch"
	HelloRejectUnsigned   HelloRejectReason = "unsigned_but_required"
	HelloRejectAsymmetric HelloRejectReason = "signed_but_unsupported"
	HelloRejectStale      HelloRejectReason = "stale_timestamp"
	HelloRejectMalformed  HelloRejectReason = "malformed"
	HelloRejectTimeout    HelloRejectReason = "timeout"
	HelloRejectWrongFirst HelloRejectReason = "first_frame_not_hello"
)

// NewMetrics builds the cluster instrument set against meter. A nil
// meter is valid — it falls back to the OpenTelemetry no-op meter,
// which lets the constructor never fail in tests / single-pod mode.
func NewMetrics(meter metric.Meter) (*Metrics, error) {
	if meter == nil {
		meter = noop.NewMeterProvider().Meter("noop")
	}

	forwards, err := meter.Int64Counter("relay_cluster_forwards_total",
		metric.WithDescription("Cross-pod forward attempts split by outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("relay_cluster_forwards_total: %w", err)
	}

	lookupLatency, err := meter.Float64Histogram("relay_cluster_lookup_duration_seconds",
		metric.WithDescription("Time spent in a peer locator lookup (broadcast → first I_HAVE)"),
		metric.WithExplicitBucketBoundaries(0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 2.0),
	)
	if err != nil {
		return nil, fmt.Errorf("relay_cluster_lookup_duration_seconds: %w", err)
	}

	helloRejects, err := meter.Int64Counter("relay_cluster_hello_rejects_total",
		metric.WithDescription("Inter-pod HELLO frames dropped at handshake, split by reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("relay_cluster_hello_rejects_total: %w", err)
	}

	pingsSent, err := meter.Int64Counter("relay_cluster_pings_sent_total",
		metric.WithDescription("Inter-pod PING frames the keepalive loop emitted"),
	)
	if err != nil {
		return nil, fmt.Errorf("relay_cluster_pings_sent_total: %w", err)
	}

	pingsLost, err := meter.Int64Counter("relay_cluster_pings_lost_total",
		metric.WithDescription("PINGs that didn't get a PONG back inside the keepalive deadline"),
	)
	if err != nil {
		return nil, fmt.Errorf("relay_cluster_pings_lost_total: %w", err)
	}

	streamsGauge, err := meter.Int64ObservableGauge("relay_cluster_streams",
		metric.WithDescription("Number of live inter-pod TCP streams owned by this pod"),
	)
	if err != nil {
		return nil, fmt.Errorf("relay_cluster_streams: %w", err)
	}

	m := &Metrics{
		meter:         meter,
		forwards:      forwards,
		lookupLatency: lookupLatency,
		helloRejects:  helloRejects,
		pingsSent:     pingsSent,
		pingsLost:     pingsLost,
		streamsGauge:  streamsGauge,
	}

	reg, err := meter.RegisterCallback(
		func(_ context.Context, o metric.Observer) error {
			if m.streamSource == nil {
				return nil
			}
			o.ObserveInt64(streamsGauge, int64(m.streamSource()))
			return nil
		},
		streamsGauge,
	)
	if err != nil {
		return nil, fmt.Errorf("relay_cluster_streams callback: %w", err)
	}
	m.registration = reg

	return m, nil
}

// SetStreamSource wires a snapshot function the streams gauge calls
// on every scrape. The transport sets this to its own Streams()
// length-and-snapshot path. Calling twice replaces the source —
// tests use that to swap in a counted fake.
func (m *Metrics) SetStreamSource(f func() int) {
	if m == nil {
		return
	}
	m.streamSource = f
}

// Close releases the gauge's callback registration. Call on relay
// shutdown so the gauge stops getting probed; safe to call once.
func (m *Metrics) Close() error {
	if m == nil || m.registration == nil {
		return nil
	}
	err := m.registration.Unregister()
	m.registration = nil
	return err
}

// IncForward bumps relay_cluster_forwards_total for one outcome.
// Safe on nil receiver.
func (m *Metrics) IncForward(ctx context.Context, result ForwardResult) {
	if m == nil {
		return
	}
	m.forwards.Add(ctx, 1, metric.WithAttributes(attribute.String("result", string(result))))
}

// ObserveLookup adds one sample to relay_cluster_lookup_duration_seconds.
// Safe on nil receiver.
func (m *Metrics) ObserveLookup(ctx context.Context, seconds float64) {
	if m == nil {
		return
	}
	m.lookupLatency.Record(ctx, seconds)
}

// IncHelloReject bumps relay_cluster_hello_rejects_total for one
// reason label. Safe on nil receiver.
func (m *Metrics) IncHelloReject(ctx context.Context, reason HelloRejectReason) {
	if m == nil {
		return
	}
	m.helloRejects.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", string(reason))))
}

// IncPingSent / IncPingLost bump the keepalive counters. Safe on
// nil receiver.
func (m *Metrics) IncPingSent(ctx context.Context) {
	if m == nil {
		return
	}
	m.pingsSent.Add(ctx, 1)
}

func (m *Metrics) IncPingLost(ctx context.Context) {
	if m == nil {
		return
	}
	m.pingsLost.Add(ctx, 1)
}
