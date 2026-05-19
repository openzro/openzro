package posture

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// EvalRetention runs a background loop that trims old rows from
// posture_evaluations. The table grows fast (one row per
// peer × policy × posture check per eval), so keeping it bounded
// matters more than keeping deep history — the dashboard's
// Posture Status timeline reads at most the last ~50 rows per
// peer anyway.
//
// Default policy: rows older than 24h get purged every 10 min.
// Operators with regulated retention requirements can either
// disable the retention loop and pipe evals to their own store
// (open follow-up) or extend the TTL via EvalRetentionOpts.
type EvalRetention struct {
	store    EvalStore
	ttl      time.Duration
	interval time.Duration

	closeOnce sync.Once
	stop      chan struct{}
	done      chan struct{}
}

// EvalRetentionOpts pins the retention knobs. Sensible defaults
// fall through NewEvalRetention so the call site is clean.
type EvalRetentionOpts struct {
	// TTL is the maximum age a row is allowed to keep. Rows whose
	// EvaluatedAt is older than time.Now() - TTL get deleted on
	// the next tick. Default: 24h.
	TTL time.Duration
	// Interval is how often the retention worker fires. Default:
	// 10 min — short enough that operators don't have to wait long
	// to see disk reclaim, long enough that the DELETE doesn't
	// thrash on a busy table.
	Interval time.Duration
}

// NewEvalRetention starts the background loop. Close() must be
// called at shutdown to stop the goroutine; it's idempotent.
func NewEvalRetention(store EvalStore, opts EvalRetentionOpts) *EvalRetention {
	if opts.TTL <= 0 {
		opts.TTL = 24 * time.Hour
	}
	if opts.Interval <= 0 {
		opts.Interval = 10 * time.Minute
	}
	r := &EvalRetention{
		store:    store,
		ttl:      opts.TTL,
		interval: opts.Interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go r.run()
	return r
}

// Close stops the retention loop and waits for the goroutine to
// exit. Idempotent.
func (r *EvalRetention) Close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		close(r.stop)
		<-r.done
	})
}

// run is the worker loop. Fires once at startup then every interval.
// Errors during purge are logged but never fatal — a transient DB
// hiccup just means we'll try again on the next tick.
func (r *EvalRetention) run() {
	defer close(r.done)

	// Stagger the first run by a small random-ish delay so a cluster
	// of management replicas don't all hit the DB at the same instant
	// on a synchronized reboot. The interval is divided by a small
	// constant rather than calling math/rand so this stays
	// deterministic test-wise; the staggering is a soft optimization,
	// not a correctness requirement.
	initial := time.NewTimer(r.interval / 6)
	defer initial.Stop()

	purge := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cutoff := time.Now().UTC().Add(-r.ttl)
		removed, err := r.store.PurgeOlderThan(ctx, cutoff)
		if err != nil {
			log.WithContext(ctx).Warnf(
				"posture eval retention: purge of rows older than %s failed: %v",
				cutoff.Format(time.RFC3339), err,
			)
			return
		}
		if removed > 0 {
			log.WithContext(ctx).Infof(
				"posture eval retention: purged %d rows older than %s",
				removed, cutoff.Format(time.RFC3339),
			)
		}
	}

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-initial.C:
			purge()
		case <-ticker.C:
			purge()
		case <-r.stop:
			return
		}
	}
}
