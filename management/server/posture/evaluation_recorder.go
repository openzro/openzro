package posture

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/cluster"
)

// BufferedRecorder is the production EvalRecorder: a non-blocking
// Record() that hands the row to a background drainer, which batch-
// inserts every flushInterval or when the in-memory buffer reaches
// batchSize. Lossy on overflow (channel full -> drop with a counter)
// so the eval hot path never blocks on the persistence layer.
//
// On top of that, Record() dedupes consecutive calls with identical
// (compliant, reason) for the same (account_id, peer_id, check_id,
// check_type) tuple — a compliant peer being re-evaluated on every
// Sync would otherwise write a row per eval per check forever. Only
// state CHANGES survive the dedup filter, so the persisted timeline
// is a meaningful audit trail instead of a heartbeat log.
//
// The dedup cache is per-Recorder = per-management-replica. Replicas
// come and go (rollouts), so the cache resets naturally; no explicit
// eviction policy is needed in the v1 sizing (a few thousand entries
// in steady state).
//
// The same Recorder instance is wired into the account manager once
// at startup and reused across every Sync/policy-eval cycle.
type BufferedRecorder struct {
	store EvalStore
	// coord is the optional cluster coordinator. When set, the
	// recorder broadcasts every committed dedup-cache entry to peer
	// replicas via dedupTopic so they can pre-populate their own
	// caches and stop writing duplicates to the persistent store.
	// nil in single-instance mode — falls back to per-replica dedup
	// with no cross-replica chatter (i.e. each replica writes its
	// own first row for a new state, up to N rows per state change
	// in an N-replica cluster).
	coord cluster.Coordinator

	queueSize     int
	batchSize     int
	flushInterval time.Duration
	refreshTTL    time.Duration

	in        chan Evaluation
	closeOnce sync.Once
	stop      chan struct{}
	done      chan struct{}
	subCtx    context.Context
	subCancel context.CancelFunc

	mu      sync.Mutex
	dropped uint64
	flushed uint64
	errored uint64
	deduped uint64
	// fromPeers counts inbound dedup-broadcast hits — peer replica
	// commits applied to the local cache. Useful as a signal that
	// the cluster pub/sub layer is actually serving its purpose.
	fromPeers uint64
	cache     map[dedupKey]dedupValue
}

// dedupKey is the natural key the cache tracks. Includes check_type
// because one posture_checks row can host multiple individual checks
// (NBVersion + EndpointSecurity in the same Checks bundle) — each
// must dedupe independently or a state change on one check would
// silently mask a stuck state on another.
type dedupKey struct {
	AccountID      string
	PeerID         string
	PostureCheckID string
	CheckType      string
}

// dedupValue is the last persisted state for a key plus the
// timestamp of when we wrote it. The timestamp drives the
// refresh-TTL: once the entry ages past refreshTTL, the next
// Record bypasses dedup even if the state hasn't changed, so
// the timeline always carries a recent row for a stable peer
// instead of going blank after retention purges history.
type dedupValue struct {
	Compliant      bool
	Reason         string
	LastRecordedAt time.Time
}

// BufferedRecorderOpts pins the runtime sizing knobs. Sensible
// defaults are filled in by NewBufferedRecorder so the call site
// stays clean.
type BufferedRecorderOpts struct {
	// QueueSize is the in-memory channel capacity. Sized for a
	// burst of evals from one large policy fan-out + headroom.
	QueueSize int
	// BatchSize is the soft cap that triggers a flush before the
	// flush timer fires. Aligned with the store's
	// CreateInBatches chunk size (200).
	BatchSize int
	// FlushInterval is the upper bound on staleness — even with
	// no events, the buffer flushes this often. 5s is a fair
	// compromise between dashboard freshness and write batching.
	FlushInterval time.Duration
	// RefreshTTL is the maximum age a cached dedup state can have
	// before the next Record() call bypasses dedup and writes a
	// fresh row even if the (compliant, reason) hasn't changed.
	// Without this, a peer whose state is stable for longer than
	// the retention TTL would drop off the dashboard timeline
	// entirely (retention purges the only persisted row; dedup
	// blocks any new write that matches the cached state). 1h is
	// the default — well under the 24h retention TTL so the
	// dashboard always has a row younger than retention purges.
	RefreshTTL time.Duration
}

// NewBufferedRecorder starts the drainer goroutine. Close() must be
// called at shutdown to flush the tail and stop the goroutine.
//
// coord is the optional cluster coordinator wired in HA deployments.
// When non-nil, the recorder broadcasts every committed dedup entry
// to sibling replicas so they can pre-populate their local caches,
// dropping the duplicate-row count on state changes from N (replica
// count) to ~1 in steady state, ~2 in the race window. nil = single
// instance mode, behaves as before.
func NewBufferedRecorder(store EvalStore, opts BufferedRecorderOpts, coord cluster.Coordinator) *BufferedRecorder {
	if opts.QueueSize <= 0 {
		opts.QueueSize = 4096
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 200
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = 5 * time.Second
	}
	if opts.RefreshTTL <= 0 {
		opts.RefreshTTL = 1 * time.Hour
	}
	subCtx, subCancel := context.WithCancel(context.Background())
	r := &BufferedRecorder{
		store:         store,
		coord:         coord,
		queueSize:     opts.QueueSize,
		batchSize:     opts.BatchSize,
		flushInterval: opts.FlushInterval,
		refreshTTL:    opts.RefreshTTL,
		in:            make(chan Evaluation, opts.QueueSize),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
		subCtx:        subCtx,
		subCancel:     subCancel,
		cache:         make(map[dedupKey]dedupValue),
	}
	go r.run()

	// Subscribe to peer broadcasts AFTER the drainer is up so we
	// never drop an inbound event into a half-built recorder. The
	// subscription is tied to a recorder-owned ctx (NOT the caller's)
	// so it survives whatever request lifetime spawned the recorder.
	if _, err := subscribeDedup(subCtx, coord, r); err != nil {
		log.Warnf("posture: subscribe dedup failed: %v — cross-replica dedup disabled this instance", err)
	}
	return r
}

// Record is the public write hook. Non-blocking: drops on overflow
// and increments the dropped counter (surfaced periodically via the
// drainer's log line). Safe to call from many goroutines.
//
// Dedup happens BEFORE the channel send: if the (compliant, reason)
// pair matches the last persisted value for this (account, peer,
// check_id, check_type) tuple, Record returns silently and the
// drainer never sees the row. State CHANGES (a previously compliant
// peer flips to non-compliant, or a denial reason changes) propagate
// through.
func (r *BufferedRecorder) Record(ctx context.Context, e Evaluation) {
	if r == nil {
		return
	}

	key := dedupKey{
		AccountID:      e.AccountID,
		PeerID:         e.PeerID,
		PostureCheckID: e.PostureCheckID,
		CheckType:      e.CheckType,
	}

	r.mu.Lock()
	if prev, ok := r.cache[key]; ok &&
		prev.Compliant == e.Compliant &&
		prev.Reason == e.Reason &&
		e.EvaluatedAt.Sub(prev.LastRecordedAt) < r.refreshTTL {
		r.deduped++
		r.mu.Unlock()
		return
	}
	commitVal := dedupValue{
		Compliant:      e.Compliant,
		Reason:         e.Reason,
		LastRecordedAt: e.EvaluatedAt,
	}
	r.cache[key] = commitVal
	r.mu.Unlock()

	select {
	case r.in <- e:
		// Broadcast the freshly-committed dedup entry to sibling
		// replicas so they can suppress their own write of the same
		// state. We publish AFTER the local channel send, not before:
		// the channel send is non-blocking, while publish goes over
		// the broker and might add ms of latency. Order matters here
		// because the publish's purpose is to prevent peer dupes,
		// and dupes only matter once OUR local row is on its way.
		publishDedup(r.coord, key, commitVal)
	default:
		r.mu.Lock()
		r.dropped++
		// Roll back the cache entry so the next attempt for this
		// key isn't silently deduped against a state we never
		// actually persisted. Without this, a single overflow
		// could mask the entire timeline for a key until the
		// state changed twice.
		delete(r.cache, key)
		r.mu.Unlock()
	}
}

// applyDedupBroadcast updates the local cache with a state another
// replica committed. Called by the subscriber goroutine on every
// inbound event — NOT exposed publicly because it bypasses
// publishDedup deliberately (an inbound event echoed back to the
// broker would cause N² amplification across the cluster).
//
// Newer-wins by EvaluatedAt timestamp: if the local cache already
// holds an entry with the same or fresher LastRecordedAt, the
// inbound event is ignored. This is the simplest path to dedup
// across replicas when both happen to evaluate the same peer in
// the same window — whoever's timestamp is later wins, and the
// loser becomes a no-op on its next Record(). Without this guard
// a slow broadcast could clobber a fresher local state and
// re-enable a duplicate write on the next Record().
func (r *BufferedRecorder) applyDedupBroadcast(key dedupKey, value dedupValue) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prev, ok := r.cache[key]; ok && !value.LastRecordedAt.After(prev.LastRecordedAt) {
		return
	}
	r.cache[key] = value
	r.fromPeers++
}

// Close flushes the in-flight buffer, tears down the cluster
// subscription, and stops the drainer. Idempotent.
func (r *BufferedRecorder) Close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		// Cancel the subscriber ctx first so the subscriber goroutine
		// stops trying to apply peer broadcasts to a recorder that's
		// about to drain its tail. The stop signal then unwinds the
		// main drainer loop after a final flush.
		if r.subCancel != nil {
			r.subCancel()
		}
		close(r.stop)
		<-r.done
	})
}

// run is the drainer goroutine. Reads from the channel, batches by
// size or time, flushes, repeats. Exits on stop signal after a final
// flush so a graceful shutdown doesn't lose buffered records.
func (r *BufferedRecorder) run() {
	defer close(r.done)

	buf := make([]Evaluation, 0, r.batchSize)
	flushTimer := time.NewTimer(r.flushInterval)
	defer flushTimer.Stop()

	statsTicker := time.NewTicker(1 * time.Minute)
	defer statsTicker.Stop()

	flush := func() {
		if len(buf) == 0 {
			return
		}
		// Detach from the request ctx — the drainer is a background
		// process, the rows are best-effort, and a parent ctx
		// cancellation must not strand the batch we just collected.
		writeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := r.store.Insert(writeCtx, buf); err != nil {
			log.WithContext(writeCtx).Warnf(
				"posture eval recorder: batch insert of %d rows failed: %v",
				len(buf), err,
			)
			r.mu.Lock()
			r.errored += uint64(len(buf))
			r.mu.Unlock()
		} else {
			r.mu.Lock()
			r.flushed += uint64(len(buf))
			r.mu.Unlock()
		}
		buf = buf[:0]
	}

	for {
		select {
		case e := <-r.in:
			buf = append(buf, e)
			if len(buf) >= r.batchSize {
				flush()
			}
		case <-flushTimer.C:
			flush()
			flushTimer.Reset(r.flushInterval)
		case <-statsTicker.C:
			r.mu.Lock()
			d, f, e, dd, fp, sz := r.dropped, r.flushed, r.errored, r.deduped, r.fromPeers, len(r.cache)
			r.mu.Unlock()
			if d > 0 || e > 0 {
				log.Infof(
					"posture eval recorder stats: flushed=%d deduped=%d from_peers=%d dropped=%d errored=%d cache_size=%d",
					f, dd, fp, d, e, sz,
				)
			}
		case <-r.stop:
			// Drain the channel before exiting so the tail isn't lost.
			for {
				select {
				case e := <-r.in:
					buf = append(buf, e)
				default:
					flush()
					return
				}
			}
		}
	}
}
