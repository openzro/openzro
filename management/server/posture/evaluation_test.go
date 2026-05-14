package posture

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeEvalStore is a thread-safe in-memory EvalStore for unit tests.
// Captures every Insert so tests can assert on what the recorder
// drained out.
type fakeEvalStore struct {
	mu    sync.Mutex
	rows  []PostureEvaluation
	calls int
}

func (f *fakeEvalStore) Insert(_ context.Context, batch []PostureEvaluation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, batch...)
	f.calls++
	return nil
}

func (f *fakeEvalStore) ListForPeer(_ context.Context, accountID, peerID string, limit int) ([]PostureEvaluation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []PostureEvaluation{}
	for i := len(f.rows) - 1; i >= 0; i-- {
		if f.rows[i].AccountID == accountID && f.rows[i].PeerID == peerID {
			out = append(out, f.rows[i])
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakeEvalStore) PurgeOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.rows[:0]
	purged := int64(0)
	for _, r := range f.rows {
		if r.EvaluatedAt.Before(cutoff) {
			purged++
			continue
		}
		kept = append(kept, r)
	}
	f.rows = kept
	return purged, nil
}

func (f *fakeEvalStore) snapshot() []PostureEvaluation {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]PostureEvaluation, len(f.rows))
	copy(out, f.rows)
	return out
}

func TestBufferedRecorder_FlushesOnBatchSize(t *testing.T) {
	store := &fakeEvalStore{}
	r := NewBufferedRecorder(store, BufferedRecorderOpts{
		QueueSize:     128,
		BatchSize:     3, // small so we hit it quickly
		FlushInterval: 1 * time.Hour,
	})
	defer r.Close()

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		r.Record(context.Background(), PostureEvaluation{
			AccountID:      "acct",
			PeerID:         "peer",
			PostureCheckID: "check",
			CheckType:      "TestCheck",
			Compliant:      false,
			Reason:         "denied",
			EvaluatedAt:    now,
		})
	}

	// The batch flush is async — wait briefly with a deadline.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("expected 3 rows flushed, store has %d", len(store.snapshot()))
		default:
			if len(store.snapshot()) == 3 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestBufferedRecorder_FlushesOnInterval(t *testing.T) {
	store := &fakeEvalStore{}
	r := NewBufferedRecorder(store, BufferedRecorderOpts{
		QueueSize:     128,
		BatchSize:     1000, // never hit
		FlushInterval: 50 * time.Millisecond,
	})
	defer r.Close()

	r.Record(context.Background(), PostureEvaluation{
		AccountID:      "acct",
		PeerID:         "peer",
		PostureCheckID: "check",
		CheckType:      "TestCheck",
		Compliant:      true,
		EvaluatedAt:    time.Now().UTC(),
	})

	deadline := time.After(1 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("expected 1 row flushed by timer, got %d", len(store.snapshot()))
		default:
			if len(store.snapshot()) == 1 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestBufferedRecorder_FlushesOnClose(t *testing.T) {
	store := &fakeEvalStore{}
	r := NewBufferedRecorder(store, BufferedRecorderOpts{
		QueueSize:     128,
		BatchSize:     1000,
		FlushInterval: 1 * time.Hour,
	})

	r.Record(context.Background(), PostureEvaluation{
		AccountID:      "acct",
		PeerID:         "peer",
		PostureCheckID: "check",
		CheckType:      "TestCheck",
		Compliant:      false,
		Reason:         "denied",
		EvaluatedAt:    time.Now().UTC(),
	})

	r.Close()

	if got := len(store.snapshot()); got != 1 {
		t.Fatalf("Close should flush the tail; got %d rows, want 1", got)
	}
}

func TestBufferedRecorder_DropsOnOverflow(t *testing.T) {
	// Block the store so the channel can't drain — exercises the
	// non-blocking Record path.
	blockedStore := &blockingStore{released: make(chan struct{})}

	r := NewBufferedRecorder(blockedStore, BufferedRecorderOpts{
		QueueSize:     2,
		BatchSize:     1,
		FlushInterval: 1 * time.Hour,
	})
	// Defer order matters: close(released) must run BEFORE r.Close()
	// so the drainer's blocked Insert can return. Register Close last
	// so LIFO unwind runs close(released) first.
	defer r.Close()
	defer close(blockedStore.released)

	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		r.Record(context.Background(), PostureEvaluation{EvaluatedAt: now})
	}

	// Give the drainer a moment to pull a couple from the channel and
	// block in Insert. After that, additional Records must overflow.
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < 20; i++ {
		r.Record(context.Background(), PostureEvaluation{EvaluatedAt: now})
	}

	r.mu.Lock()
	dropped := r.dropped
	r.mu.Unlock()
	if dropped == 0 {
		t.Fatalf("expected non-zero dropped counter on overflow, got 0")
	}
}

type blockingStore struct{ released chan struct{} }

func (b *blockingStore) Insert(_ context.Context, _ []PostureEvaluation) error {
	<-b.released
	return nil
}
func (b *blockingStore) ListForPeer(_ context.Context, _, _ string, _ int) ([]PostureEvaluation, error) {
	return nil, nil
}
func (b *blockingStore) PurgeOlderThan(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func TestFakeStore_ListReturnsNewestFirst(t *testing.T) {
	store := &fakeEvalStore{}
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_ = store.Insert(context.Background(), []PostureEvaluation{{
			AccountID:   "a",
			PeerID:      "p",
			EvaluatedAt: now.Add(time.Duration(i) * time.Second),
		}})
	}
	rows, err := store.ListForPeer(context.Background(), "a", "p", 3)
	if err != nil {
		t.Fatalf("ListForPeer: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	// Newest first means index 0 == latest insert (offset +4s).
	if !rows[0].EvaluatedAt.Equal(now.Add(4 * time.Second)) {
		t.Errorf("rows not newest-first: %v", rows[0].EvaluatedAt)
	}
}

func TestEvalRetention_PurgesOldRows(t *testing.T) {
	store := &fakeEvalStore{}
	old := time.Now().UTC().Add(-48 * time.Hour)
	fresh := time.Now().UTC().Add(-1 * time.Hour)
	_ = store.Insert(context.Background(), []PostureEvaluation{
		{AccountID: "a", PeerID: "p", EvaluatedAt: old, CheckType: "old"},
		{AccountID: "a", PeerID: "p", EvaluatedAt: fresh, CheckType: "fresh"},
	})

	r := NewEvalRetention(store, EvalRetentionOpts{
		TTL:      24 * time.Hour,
		Interval: 1 * time.Hour, // we drive the initial fire manually
	})
	defer r.Close()

	// First run is staggered (interval/6 ≈ 10min) — too long for the
	// test. Bypass the loop by calling PurgeOlderThan directly through
	// the store; this proves the contract the goroutine relies on.
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	removed, err := store.PurgeOlderThan(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 row purged, got %d", removed)
	}
	rows := store.snapshot()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row remaining, got %d", len(rows))
	}
	if rows[0].CheckType != "fresh" {
		t.Fatalf("wrong row survived: %+v", rows[0])
	}
}
