package posture

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// BufferedRecorder is the production EvalRecorder: a non-blocking
// Record() that hands the row to a background drainer, which batch-
// inserts every flushInterval or when the in-memory buffer reaches
// batchSize. Lossy on overflow (channel full -> drop with a counter)
// so the eval hot path never blocks on the persistence layer.
//
// The same Recorder instance is wired into the account manager once
// at startup and re-used across every Sync/policy-eval cycle.
type BufferedRecorder struct {
	store EvalStore

	queueSize     int
	batchSize     int
	flushInterval time.Duration

	in        chan PostureEvaluation
	closeOnce sync.Once
	stop      chan struct{}
	done      chan struct{}

	mu       sync.Mutex
	dropped  uint64
	flushed  uint64
	errored  uint64
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
}

// NewBufferedRecorder starts the drainer goroutine. Close() must be
// called at shutdown to flush the tail and stop the goroutine.
func NewBufferedRecorder(store EvalStore, opts BufferedRecorderOpts) *BufferedRecorder {
	if opts.QueueSize <= 0 {
		opts.QueueSize = 4096
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 200
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = 5 * time.Second
	}
	r := &BufferedRecorder{
		store:         store,
		queueSize:     opts.QueueSize,
		batchSize:     opts.BatchSize,
		flushInterval: opts.FlushInterval,
		in:            make(chan PostureEvaluation, opts.QueueSize),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}
	go r.run()
	return r
}

// Record is the public write hook. Non-blocking: drops on overflow
// and increments the dropped counter (surfaced periodically via the
// drainer's log line). Safe to call from many goroutines.
func (r *BufferedRecorder) Record(ctx context.Context, e PostureEvaluation) {
	if r == nil {
		return
	}
	select {
	case r.in <- e:
	default:
		r.mu.Lock()
		r.dropped++
		r.mu.Unlock()
	}
}

// Close flushes the in-flight buffer and stops the drainer. Idempotent.
func (r *BufferedRecorder) Close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		close(r.stop)
		<-r.done
	})
}

// run is the drainer goroutine. Reads from the channel, batches by
// size or time, flushes, repeats. Exits on stop signal after a final
// flush so a graceful shutdown doesn't lose buffered records.
func (r *BufferedRecorder) run() {
	defer close(r.done)

	buf := make([]PostureEvaluation, 0, r.batchSize)
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
			d, f, e := r.dropped, r.flushed, r.errored
			r.mu.Unlock()
			if d > 0 || e > 0 {
				log.Infof(
					"posture eval recorder stats: flushed=%d dropped=%d errored=%d",
					f, d, e,
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
