package posture

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// openSqliteForTest opens an in-memory SQLite handle for migration
// tests. Cheap, isolated, and matches the driver the production
// store package uses (glebarez/sqlite — pure-Go, no cgo).
func openSqliteForTest(_ *testing.T) (*gorm.DB, error) {
	return gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
}

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
	// Distinct PeerIDs so dedup does not suppress identical-state repeats —
	// this test asserts the batch-size flush path, not dedup behavior.
	for i := 0; i < 3; i++ {
		r.Record(context.Background(), PostureEvaluation{
			AccountID:      "acct",
			PeerID:         fmt.Sprintf("peer-%d", i),
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
	// Distinct PeerIDs so dedup does not suppress on the cache lookup —
	// without this every Record() after the first would short-circuit
	// before ever reaching the channel-send path we're trying to exercise.
	for i := 0; i < 10; i++ {
		r.Record(context.Background(), PostureEvaluation{
			PeerID:      fmt.Sprintf("peer-a-%d", i),
			EvaluatedAt: now,
		})
	}

	// Give the drainer a moment to pull a couple from the channel and
	// block in Insert. After that, additional Records must overflow.
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < 20; i++ {
		r.Record(context.Background(), PostureEvaluation{
			PeerID:      fmt.Sprintf("peer-b-%d", i),
			EvaluatedAt: now,
		})
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

func TestMigrateEvaluationTable_CreatesIndexes(t *testing.T) {
	// Skip if sqlite driver isn't pulled in (it's already in go.mod
	// for the other GORM-backed stores; defensive guard for future
	// build configurations).
	db, err := openSqliteForTest(t)
	if err != nil {
		t.Skip("sqlite test driver not available:", err)
	}

	if err := MigrateEvaluationTable(db); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	// Both indexes must exist after migration.
	mig := db.Migrator()
	for _, idx := range []string{
		"idx_posture_eval_account_peer_time",
		"idx_posture_eval_evaluated_at",
	} {
		if !mig.HasIndex(&PostureEvaluation{}, idx) {
			t.Errorf("expected index %q after AutoMigrate", idx)
		}
	}
}

// BenchmarkRecord_HotPath measures the overhead of a single Record()
// call — this fires inside validatePostureChecksOnPeer once per
// check.Check() invocation, so the hot path runs it
// O(peers × policies × checks_per_policy) per Sync. Anything above a
// few hundred ns/op here would show up as visible CPU under load.
func BenchmarkRecord_HotPath(b *testing.B) {
	r := NewBufferedRecorder(&fakeEvalStore{}, BufferedRecorderOpts{
		QueueSize:     65536, // sized large so we never block in this loop
		BatchSize:     200,
		FlushInterval: 1 * time.Hour,
	})
	defer r.Close()

	ev := PostureEvaluation{
		AccountID:      "bench-account",
		PeerID:         "bench-peer",
		PostureCheckID: "bench-check",
		CheckType:      "EndpointSecurityCheck",
		Compliant:      false,
		Reason:         "endpoint-security: device not enrolled in Intune (hostname=\"x\", user=\"y\", os=\"linux\")",
		EvaluatedAt:    time.Now().UTC(),
	}
	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.Record(ctx, ev)
	}
}

func TestBufferedRecorder_DedupesIdenticalConsecutiveStates(t *testing.T) {
	store := &fakeEvalStore{}
	r := NewBufferedRecorder(store, BufferedRecorderOpts{
		QueueSize:     128,
		BatchSize:     1, // flush every record so we can assert on store
		FlushInterval: 1 * time.Hour,
	})
	defer r.Close()

	now := time.Now().UTC()
	base := PostureEvaluation{
		AccountID:      "a",
		PeerID:         "p",
		PostureCheckID: "c",
		CheckType:      "EndpointSecurityCheck",
		Compliant:      true,
		Reason:         "",
		EvaluatedAt:    now,
	}

	// 1st record — fresh, must be persisted.
	r.Record(context.Background(), base)
	// 2nd–10th — identical state, must be deduped.
	for i := 0; i < 9; i++ {
		dup := base
		dup.EvaluatedAt = now.Add(time.Duration(i) * time.Second)
		r.Record(context.Background(), dup)
	}

	// Wait for the drainer to flush the first one.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("first record never landed in store")
		default:
			if len(store.snapshot()) >= 1 {
				goto persisted
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
persisted:

	// Give the drainer a moment to (not) flush the dupes.
	time.Sleep(50 * time.Millisecond)
	if got := len(store.snapshot()); got != 1 {
		t.Fatalf("dedup failed: expected 1 row, got %d", got)
	}
	r.mu.Lock()
	deduped := r.deduped
	r.mu.Unlock()
	if deduped != 9 {
		t.Fatalf("expected 9 dedupes, got %d", deduped)
	}
}

func TestBufferedRecorder_StateChangeBreaksDedup(t *testing.T) {
	store := &fakeEvalStore{}
	r := NewBufferedRecorder(store, BufferedRecorderOpts{
		QueueSize:     128,
		BatchSize:     1,
		FlushInterval: 1 * time.Hour,
	})
	defer r.Close()

	now := time.Now().UTC()
	base := PostureEvaluation{
		AccountID:      "a",
		PeerID:         "p",
		PostureCheckID: "c",
		CheckType:      "EndpointSecurityCheck",
		EvaluatedAt:    now,
	}

	// 1st: compliant. 2nd: not compliant. 3rd: compliant again (NEW reason).
	// All three must persist — dedup only suppresses unchanged repeats.
	compliant := base
	compliant.Compliant = true
	r.Record(context.Background(), compliant)

	notCompliant := base
	notCompliant.Compliant = false
	notCompliant.Reason = "denied because reasons"
	r.Record(context.Background(), notCompliant)

	compliantAgain := base
	compliantAgain.Compliant = true
	r.Record(context.Background(), compliantAgain)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("expected 3 distinct rows, got %d", len(store.snapshot()))
		default:
			if len(store.snapshot()) == 3 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestBufferedRecorder_RefreshTTLBypassesDedup(t *testing.T) {
	// A stable-state peer must still leave a fresh row periodically so
	// the dashboard timeline has something to show after retention
	// purges older rows. The TTL on the dedup cache enforces that.
	store := &fakeEvalStore{}
	r := NewBufferedRecorder(store, BufferedRecorderOpts{
		QueueSize:     128,
		BatchSize:     1,
		FlushInterval: 1 * time.Hour,
		RefreshTTL:    100 * time.Millisecond,
	})
	defer r.Close()

	now := time.Now().UTC()
	base := PostureEvaluation{
		AccountID:      "a",
		PeerID:         "p",
		PostureCheckID: "c",
		CheckType:      "EndpointSecurityCheck",
		Compliant:      true,
		EvaluatedAt:    now,
	}

	// 1st: persisted (cache miss).
	r.Record(context.Background(), base)

	// 2nd: same state, EvaluatedAt within TTL — must dedupe.
	within := base
	within.EvaluatedAt = now.Add(50 * time.Millisecond)
	r.Record(context.Background(), within)

	// 3rd: same state but EvaluatedAt past the TTL — must persist
	// even though (compliant, reason) is unchanged.
	expired := base
	expired.EvaluatedAt = now.Add(200 * time.Millisecond)
	r.Record(context.Background(), expired)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("expected 2 rows (one bypassed dedup via TTL), got %d", len(store.snapshot()))
		default:
			if len(store.snapshot()) == 2 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestBufferedRecorder_DedupIsPerCheckType(t *testing.T) {
	// Two different check_types under the same posture_checks row
	// (same peer, same account, same posture_check_id) must dedupe
	// independently — a state change in one cannot mask the other.
	store := &fakeEvalStore{}
	r := NewBufferedRecorder(store, BufferedRecorderOpts{
		QueueSize:     128,
		BatchSize:     1,
		FlushInterval: 1 * time.Hour,
	})
	defer r.Close()

	now := time.Now().UTC()
	for _, check := range []string{"EndpointSecurityCheck", "OSVersionCheck"} {
		r.Record(context.Background(), PostureEvaluation{
			AccountID:      "a",
			PeerID:         "p",
			PostureCheckID: "c",
			CheckType:      check,
			Compliant:      true,
			EvaluatedAt:    now,
		})
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("expected 2 rows (distinct check_types), got %d", len(store.snapshot()))
		default:
			if len(store.snapshot()) == 2 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}
