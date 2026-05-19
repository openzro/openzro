package admission

import (
	"context"
	"os"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
)

// EventEmitter is the abstract dependency the expiry worker has on
// the management server's activity log. Defined here (rather than
// imported from server) to avoid an import cycle: the admission
// package would otherwise need server, and server already needs
// admission. The signature mirrors DefaultAccountManager.StoreEvent
// stripped of the activity.Activity type — caller passes the
// activity-code int directly.
type EventEmitter func(
	ctx context.Context,
	initiatorID, targetID, accountID string,
	activityCode uint32,
	meta map[string]any,
)

// Activity code for `peer.admission.bypass.expired`. Mirrors the
// constant in management/server/activity/codes.go; duplicated here
// so this package does not import activity. The activity package
// uses an int alias under the hood; cast at the call site.
const ActivityBypassExpired = 93

const (
	defaultSweepInterval = 1 * time.Hour
	envSweepInterval     = "OPENZRO_ADMISSION_BYPASS_SWEEP_INTERVAL_SECONDS"

	// minSweepInterval guards against pathologically tight values
	// (someone setting "1" by mistake). 60s floor — bypass expiry
	// has no real-time SLA, hourly is plenty for the audit story.
	minSweepInterval = 60 * time.Second
)

// RunExpiryWorker is the long-running janitor that deletes expired
// bypass rows and emits a `peer.admission.bypass.expired` audit event
// per row. Idempotent: a row already physically deleted by another
// instance is skipped without error.
//
// The worker is single-instance-safe but multi-instance-tolerant —
// in HA, every management instance ticks independently; each tries
// to delete the same set; whichever wins emits the event. SweepExpired
// uses the row IDs returned from the SELECT to delete in one batch,
// so a parallel instance racing on the same row gets a no-op
// (RowsAffected = 0) and its event is skipped. Acceptable: the audit
// log gets the event from the winner, no double-counting.
//
// Lifecycle: started from cmd/management.go after the Store is
// constructed; runs until ctx is canceled.
func RunExpiryWorker(ctx context.Context, store *Store, emit EventEmitter) {
	interval := resolveSweepInterval()
	log.WithContext(ctx).Infof("admission bypass expiry worker running every %s", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run once at startup so a process restart promptly sweeps any
	// rows that expired while the management was down.
	sweepOnce(ctx, store, emit)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepOnce(ctx, store, emit)
		}
	}
}

func sweepOnce(ctx context.Context, store *Store, emit EventEmitter) {
	expired, err := store.SweepExpired(ctx, time.Now().UTC())
	if err != nil {
		log.WithContext(ctx).Errorf("admission bypass sweep failed: %v", err)
		return
	}
	for i := range expired {
		row := expired[i]
		emit(ctx, row.InitiatorID, row.PeerID, row.AccountID, ActivityBypassExpired, map[string]any{
			"reason":     row.Reason,
			"granted_at": row.GrantedAt.Format(time.RFC3339),
			"expired_at": row.ExpiresAt.Format(time.RFC3339),
		})
	}
	if len(expired) > 0 {
		log.WithContext(ctx).Infof("admission bypass sweep: expired %d row(s)", len(expired))
	}
}

func resolveSweepInterval() time.Duration {
	v := os.Getenv(envSweepInterval)
	if v == "" {
		return defaultSweepInterval
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Warnf("admission: invalid %s=%q, using default %s",
			envSweepInterval, v, defaultSweepInterval)
		return defaultSweepInterval
	}
	if n <= 0 {
		return defaultSweepInterval
	}
	d := time.Duration(n) * time.Second
	if d < minSweepInterval {
		log.Warnf("admission: %s=%d below minimum %s; flooring",
			envSweepInterval, n, minSweepInterval)
		return minSweepInterval
	}
	return d
}
