package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// PeerLoginMFAMetrics counts what happens to device logins that need the
// user's second factor (ADR-0028 D12). Attributes stay bounded: a reason
// or an outcome, never an account, user or peer.
type PeerLoginMFAMetrics struct {
	ctx              context.Context
	refusals         metric.Int64Counter
	outcomes         metric.Int64Counter
	timeToApprovalMs metric.Float64Histogram
}

// NewPeerLoginMFAMetrics creates the device-login 2FA metrics.
func NewPeerLoginMFAMetrics(ctx context.Context, meter metric.Meter) (*PeerLoginMFAMetrics, error) {
	refusals, err := meter.Int64Counter("management.peer.login.mfa.refusal.counter",
		metric.WithUnit("1"),
		metric.WithDescription("Device logins refused by the 2FA gate, by reason; "+
			"reason=required counts the client's polls while it waits for approval"))
	if err != nil {
		return nil, err
	}
	outcomes, err := meter.Int64Counter("management.peer.login.mfa.outcome.counter",
		metric.WithUnit("1"),
		metric.WithDescription("Device login 2FA approvals by outcome: approved, failed, denied, expired"))
	if err != nil {
		return nil, err
	}
	timeToApprovalMs, err := meter.Float64Histogram("management.peer.login.mfa.time.to.approval.ms",
		metric.WithUnit("milliseconds"),
		metric.WithExplicitBucketBoundaries(5000, 10000, 20000, 30000, 60000, 120000, 180000, 300000),
		metric.WithDescription("Time from a device login's first refusal to its approval"))
	if err != nil {
		return nil, err
	}
	return &PeerLoginMFAMetrics{ctx: ctx, refusals: refusals, outcomes: outcomes, timeToApprovalMs: timeToApprovalMs}, nil
}

// CountRefusal counts a device login refused by the gate. reason is one of
// required, denied, rate_limited, enrollment_required or error.
func (m *PeerLoginMFAMetrics) CountRefusal(reason string) {
	if m == nil {
		return
	}
	m.refusals.Add(m.ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// CountOutcome counts an approval reaching outcome.
func (m *PeerLoginMFAMetrics) CountOutcome(outcome string) {
	if m == nil {
		return
	}
	m.outcomes.Add(m.ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

// RecordTimeToApproval records how long a login waited for its approval.
func (m *PeerLoginMFAMetrics) RecordTimeToApproval(d time.Duration) {
	if m == nil {
		return
	}
	m.timeToApprovalMs.Record(m.ctx, float64(d.Nanoseconds())/1e6)
}
