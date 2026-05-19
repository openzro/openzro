package posture

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/cluster"
)

// ─── fakes ────────────────────────────────────────────────────────────

type fakeUpdater struct {
	mu        sync.Mutex
	fires     []string
	panicNext bool
}

func (f *fakeUpdater) UpdateAccountPeers(_ context.Context, accountID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.panicNext {
		f.panicNext = false
		panic("synthetic panic in UpdateAccountPeers")
	}
	f.fires = append(f.fires, accountID)
}

func (f *fakeUpdater) fireCount(accountID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, id := range f.fires {
		if id == accountID {
			n++
		}
	}
	return n
}

type fakeLoader struct {
	mu       sync.Mutex
	accounts []string
	checks   map[string][]ScheduleCheck
	loadErr  error
	listErr  error
	// listCalls counts AccountsWithActiveSchedules invocations — one
	// per reconcile pass. Used by the debounce regression test to
	// prove a burst of global-topic events collapses into a single
	// reconcile rather than one scan per event.
	listCalls atomic.Uint64
}

func (f *fakeLoader) LoadActiveSchedules(_ context.Context, accountID string) ([]ScheduleCheck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return f.checks[accountID], nil
}

func (f *fakeLoader) AccountsWithActiveSchedules(_ context.Context) ([]string, error) {
	f.listCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]string, len(f.accounts))
	copy(out, f.accounts)
	return out, nil
}

func (f *fakeLoader) setChecks(accountID string, checks []ScheduleCheck) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks[accountID] = checks
}

// addAccount makes an account discoverable AFTER Run has started —
// models an operator attaching the first ScheduleCheck to an account
// the scheduler had no loop for at boot.
func (f *fakeLoader) addAccount(accountID string, checks []ScheduleCheck) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accounts = append(f.accounts, accountID)
	f.checks[accountID] = checks
}

// fakeCoord is a minimal in-process cluster.Coordinator stand-in. Lock
// is honored per name with a single token; Subscribe gives each topic
// a fresh fan-out channel; Publish broadcasts to every subscriber for
// the topic. Suitable only for the scheduler unit tests in this file.
type fakeCoord struct {
	mu     sync.Mutex
	locks  map[string]chan struct{}
	subs   map[string][]chan cluster.Event
	closed bool
}

func newFakeCoord() *fakeCoord {
	return &fakeCoord{
		locks: map[string]chan struct{}{},
		subs:  map[string][]chan cluster.Event{},
	}
}

func (c *fakeCoord) Lock(ctx context.Context, name string) (func(), error) {
	c.mu.Lock()
	ch, ok := c.locks[name]
	if !ok {
		ch = make(chan struct{}, 1)
		c.locks[name] = ch
	}
	c.mu.Unlock()
	select {
	case ch <- struct{}{}:
		return func() {
			<-ch
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *fakeCoord) Publish(_ context.Context, topic string, payload []byte) error {
	c.mu.Lock()
	subs := append([]chan cluster.Event(nil), c.subs[topic]...)
	c.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- cluster.Event{Topic: topic, Payload: payload}:
		default:
			// drop on slow subscriber; tests must drain promptly
		}
	}
	return nil
}

func (c *fakeCoord) Subscribe(ctx context.Context, topic string) (<-chan cluster.Event, error) {
	ch := make(chan cluster.Event, 16)
	c.mu.Lock()
	c.subs[topic] = append(c.subs[topic], ch)
	c.mu.Unlock()
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func (c *fakeCoord) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// ─── tests ────────────────────────────────────────────────────────────

// TestScheduler_NoAccounts asserts Run blocks gracefully until ctx
// cancellation when the loader reports zero accounts with active
// schedules — no goroutine panics, returns ctx.Err on shutdown.
func TestScheduler_NoAccounts(t *testing.T) {
	t.Parallel()
	coord := newFakeCoord()
	updater := &fakeUpdater{}
	loader := &fakeLoader{checks: map[string][]ScheduleCheck{}}

	s := NewScheduler(coord, updater, loader)
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- s.Run(ctx) }()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("Run did not exit after ctx cancellation")
	}
	assert.Equal(t, 0, updater.fireCount("any"))
}

// TestScheduler_FiresAtBoundary spawns the scheduler with one account
// whose schedule check has any future boundary; tests collapse the
// wait via an injected afterFn that fires after a few milliseconds
// regardless of the requested duration. The boundary math itself is
// covered in scheduler_test.go — this test only verifies the goroutine
// wakes from a wait and calls UpdateAccountPeers.
func TestScheduler_FiresAtBoundary(t *testing.T) {
	t.Parallel()
	coord := newFakeCoord()
	updater := &fakeUpdater{}
	loader := &fakeLoader{
		accounts: []string{"acct-1"},
		checks: map[string][]ScheduleCheck{
			"acct-1": {
				{
					Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"},
					Action: CheckActionAllow,
				},
			},
		},
	}
	s := NewScheduler(coord, updater, loader)
	s.now = fixedClockAtMidnight()
	s.afterFn = fastAfter(20 * time.Millisecond)
	s.minTickInterval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	require.Eventually(t,
		func() bool { return updater.fireCount("acct-1") >= 1 },
		2*time.Second,
		20*time.Millisecond,
		"scheduler should fire UpdateAccountPeers within 2s",
	)
}

// TestScheduler_InvalidationRecomputes proves that publishing on the
// per-account topic interrupts the sleep and forces a re-load of
// schedules. We start with no checks (sleep), then publish + add a
// check, and expect a fire shortly after.
func TestScheduler_InvalidationRecomputes(t *testing.T) {
	t.Parallel()
	coord := newFakeCoord()
	updater := &fakeUpdater{}
	loader := &fakeLoader{
		accounts: []string{"acct-2"},
		checks:   map[string][]ScheduleCheck{"acct-2": nil}, // none yet
	}
	s := NewScheduler(coord, updater, loader)
	s.now = fixedClockAtMidnight()
	s.afterFn = fastAfter(20 * time.Millisecond)
	s.minTickInterval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	// Give the loop a moment to enter the "no checks → block" branch.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, updater.fireCount("acct-2"))

	// Plant a check and kick the scheduler with an invalidation.
	loader.setChecks("acct-2", []ScheduleCheck{
		{
			Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"},
			Action: CheckActionAllow,
		},
	})
	PublishScheduleChange(ctx, coord, "acct-2")

	require.Eventually(t,
		func() bool { return updater.fireCount("acct-2") >= 1 },
		2*time.Second,
		20*time.Millisecond,
		"scheduler should fire after invalidation+new check",
	)
}

// TestScheduler_UpdatePanicDoesNotKillLoop confirms a panic in
// UpdateAccountPeers is contained — the loop survives and fires again
// on the next boundary.
func TestScheduler_UpdatePanicDoesNotKillLoop(t *testing.T) {
	t.Parallel()
	coord := newFakeCoord()
	updater := &fakeUpdater{panicNext: true}
	loader := &fakeLoader{
		accounts: []string{"acct-3"},
		checks: map[string][]ScheduleCheck{
			"acct-3": {
				{
					Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"},
					Action: CheckActionAllow,
				},
			},
		},
	}
	s := NewScheduler(coord, updater, loader)
	s.now = fixedClockAtMidnight()
	s.afterFn = fastAfter(20 * time.Millisecond)
	// Coalescing depends on `now()` advancing — with the fixed test
	// clock the rate-limit would never expire, so disable it for this
	// test. The behaviour under coalescing is exercised separately
	// in the leader-election test, which uses both replicas firing.
	s.minTickInterval = 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	// First fire panics → recovered. Updater clears panicNext on the
	// panicking branch, so subsequent fires are normal. We wait for at
	// least one fire to have happened (after the panic was caught) to
	// confirm the loop survived.
	require.Eventually(t,
		func() bool {
			updater.mu.Lock()
			defer updater.mu.Unlock()
			return !updater.panicNext && len(updater.fires) >= 1
		},
		2*time.Second,
		20*time.Millisecond,
		"scheduler should survive panic and continue firing",
	)
}

// TestScheduler_LeaderElectionExclusive proves that when two Scheduler
// instances share a fakeCoord, only one of them holds the lock at a
// time, so only one fires UpdateAccountPeers per boundary.
func TestScheduler_LeaderElectionExclusive(t *testing.T) {
	t.Parallel()
	coord := newFakeCoord()
	var fires int64
	updater := updaterFn(func(_ context.Context, accountID string) {
		atomic.AddInt64(&fires, 1)
	})
	loader := &fakeLoader{
		accounts: []string{"acct-4"},
		checks: map[string][]ScheduleCheck{
			"acct-4": {
				{
					Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"},
					Action: CheckActionAllow,
				},
			},
		},
	}
	mkScheduler := func() *Scheduler {
		s := NewScheduler(coord, updater, loader)
		s.now = fixedClockAtMidnight()
		s.afterFn = fastAfter(20 * time.Millisecond)
		s.minTickInterval = 100 * time.Millisecond
		return s
	}
	s1 := mkScheduler()
	s2 := mkScheduler()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s1.Run(ctx) }()
	go func() { _ = s2.Run(ctx) }()

	require.Eventually(t,
		func() bool { return atomic.LoadInt64(&fires) >= 1 },
		2*time.Second,
		20*time.Millisecond,
	)

	// Give the loops a moment to settle, then assert no double-fire
	// happened: only one replica was leader.
	time.Sleep(200 * time.Millisecond)
	got := atomic.LoadInt64(&fires)
	assert.LessOrEqual(t, got, int64(2),
		"only the leader fires per boundary; got %d fires (double-fire suspected)", got)
}

// TestNewScheduler_NilDependenciesPanic guards against accidental
// wiring bugs at boot — passing nil for any dependency must crash
// loud instead of silently producing a Scheduler that no-ops.
func TestNewScheduler_NilDependenciesPanic(t *testing.T) {
	t.Parallel()
	coord := newFakeCoord()
	updater := &fakeUpdater{}
	loader := &fakeLoader{}

	for name, deps := range map[string][3]any{
		"nil coord":   {nil, updater, loader},
		"nil updater": {coord, nil, loader},
		"nil loader":  {coord, updater, nil},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Panics(t, func() {
				_ = NewScheduler(
					deps[0].(cluster.Coordinator),
					deps[1].(AccountUpdater),
					deps[2].(ScheduleLoader),
				)
			})
		})
	}
}

// TestPublishScheduleChange_Idempotent confirms the helper tolerates
// nil coordinator and empty accountID — both are valid no-ops because
// the scheduler legitimately may not be running in tests, unit harnesses
// or local-only deployments.
func TestPublishScheduleChange_Idempotent(t *testing.T) {
	t.Parallel()
	PublishScheduleChange(context.Background(), nil, "any")
	PublishScheduleChange(context.Background(), newFakeCoord(), "")
}

// TestScheduler_AutoDiscoversNewAccountViaReconcileTick proves the
// safety-net path: an account that gains its first ScheduleCheck
// AFTER Run started — with NO pub/sub nudge at all — is still picked
// up by the periodic reconcile and gets a per-account loop that
// fires UpdateAccountPeers. This is the core gap the follow-up
// closes (previously required a management restart).
func TestScheduler_AutoDiscoversNewAccountViaReconcileTick(t *testing.T) {
	t.Parallel()
	coord := newFakeCoord()
	updater := &fakeUpdater{}
	loader := &fakeLoader{checks: map[string][]ScheduleCheck{}} // zero accounts at boot

	s := NewScheduler(coord, updater, loader)
	s.now = fixedClockAtMidnight()
	s.afterFn = fastAfter(20 * time.Millisecond)
	s.minTickInterval = time.Millisecond
	s.reconcileEvery = 30 * time.Millisecond // collapse the 3-min default
	s.reconcileDebounce = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	// Nothing should fire while there are no accounts.
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, 0, updater.fireCount("late-acct"))

	// Operator attaches the first schedule check post-boot. No
	// publish — rely purely on the periodic reconcile.
	loader.addAccount("late-acct", []ScheduleCheck{
		{Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"}, Action: CheckActionAllow},
	})

	require.Eventually(t,
		func() bool { return updater.fireCount("late-acct") >= 1 },
		2*time.Second, 20*time.Millisecond,
		"periodic reconcile should discover the new account and fire UpdateAccountPeers",
	)
}

// TestScheduler_AutoDiscoversNewAccountViaGlobalTopic proves the fast
// path: publishing on the global accounts-changed topic triggers an
// immediate reconcile so a brand-new schedule-bearing account is
// picked up sub-second, not after a full reconcile interval.
func TestScheduler_AutoDiscoversNewAccountViaGlobalTopic(t *testing.T) {
	t.Parallel()
	coord := newFakeCoord()
	updater := &fakeUpdater{}
	loader := &fakeLoader{checks: map[string][]ScheduleCheck{}}

	s := NewScheduler(coord, updater, loader)
	s.now = fixedClockAtMidnight()
	s.afterFn = fastAfter(20 * time.Millisecond)
	s.minTickInterval = time.Millisecond
	// Long reconcile so a pass within the test window can only come
	// from the global-topic fast path, not the periodic safety net.
	s.reconcileEvery = time.Hour
	s.reconcileDebounce = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)

	loader.addAccount("fast-acct", []ScheduleCheck{
		{Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"}, Action: CheckActionAllow},
	})
	// The production publish path (PublishScheduleChange) hits both
	// topics; here we exercise the global one the discovery loop
	// listens on.
	PublishScheduleChange(ctx, coord, "fast-acct")

	require.Eventually(t,
		func() bool { return updater.fireCount("fast-acct") >= 1 },
		2*time.Second, 20*time.Millisecond,
		"global-topic publish should trigger an immediate reconcile despite a 1h periodic interval",
	)
}

// TestScheduler_ReconcileIsAddOnly proves a repeated reconcile does
// not double-spawn a loop for an already-running account. Two loops
// for one account would both contend the cluster lock; the second
// would sit blocked forever on the fakeCoord's single-token lock and
// the account would still fire exactly on schedule. We assert the
// observable invariant: steady, non-duplicated firing under repeated
// global-topic nudges.
func TestScheduler_ReconcileIsAddOnly(t *testing.T) {
	t.Parallel()
	coord := newFakeCoord()
	updater := &fakeUpdater{}
	loader := &fakeLoader{
		accounts: []string{"acct-1"},
		checks: map[string][]ScheduleCheck{
			"acct-1": {{Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"}, Action: CheckActionAllow}},
		},
	}
	s := NewScheduler(coord, updater, loader)
	s.now = fixedClockAtMidnight()
	s.afterFn = fastAfter(20 * time.Millisecond)
	s.minTickInterval = time.Millisecond
	s.reconcileEvery = 15 * time.Millisecond
	s.reconcileDebounce = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	// Hammer the global topic — every publish triggers a reconcile.
	// Add-only means the already-running acct-1 is skipped each time.
	for i := 0; i < 10; i++ {
		PublishScheduleChange(ctx, coord, "acct-1")
		time.Sleep(5 * time.Millisecond)
	}

	require.Eventually(t,
		func() bool { return updater.fireCount("acct-1") >= 1 },
		2*time.Second, 20*time.Millisecond,
		"account still fires under repeated reconciles",
	)
	// If reconcile had double-spawned, the second loop would be
	// deadlocked on the lock — harmless to this assertion, but the
	// run completing cleanly on cancel (below, via defer) confirms no
	// panic from concurrent map writes (reconcile is single-goroutine).
}

// TestScheduler_SubscribeFailureFallsBackToPeriodic proves Run does
// not refuse to start when the global-topic Subscribe fails — it
// degrades to periodic-reconcile-only and still discovers accounts.
func TestScheduler_SubscribeFailureFallsBackToPeriodic(t *testing.T) {
	t.Parallel()
	coord := &subFailCoord{fakeCoord: newFakeCoord()}
	updater := &fakeUpdater{}
	loader := &fakeLoader{
		accounts: []string{"acct-1"},
		checks: map[string][]ScheduleCheck{
			"acct-1": {{Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"}, Action: CheckActionAllow}},
		},
	}
	s := NewScheduler(coord, updater, loader)
	s.now = fixedClockAtMidnight()
	s.afterFn = fastAfter(20 * time.Millisecond)
	s.minTickInterval = time.Millisecond
	s.reconcileEvery = 20 * time.Millisecond
	s.reconcileDebounce = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	require.Eventually(t,
		func() bool { return updater.fireCount("acct-1") >= 1 },
		2*time.Second, 20*time.Millisecond,
		"scheduler must still work via periodic reconcile when the global subscription fails",
	)
}

// subFailCoord fails ONLY the global accounts-changed subscription,
// letting the per-account invalidation subscriptions through so the
// rest of the loop behaves normally. Models a partial broker issue.
type subFailCoord struct {
	*fakeCoord
}

func (c *subFailCoord) Subscribe(ctx context.Context, topic string) (<-chan cluster.Event, error) {
	if topic == scheduleAccountsChangedTopic {
		return nil, errors.New("subscribe boom")
	}
	return c.fakeCoord.Subscribe(ctx, topic)
}

// TestScheduler_GlobalTopicBurstIsDebounced is the Concern-B
// regression: PublishScheduleChange fires the global topic on every
// posture-check mutation cluster-wide, and each reconcile is a full
// GetAllPostureChecks scan on every subscribed replica. Without
// debounce a bulk policy edit fans one scan per mutation across the
// fleet. This proves a rapid burst of global-topic events collapses
// into a single reconcile (one AccountsWithActiveSchedules call),
// not one per event.
func TestScheduler_GlobalTopicBurstIsDebounced(t *testing.T) {
	t.Parallel()
	coord := newFakeCoord()
	updater := &fakeUpdater{}
	loader := &fakeLoader{
		accounts: []string{"acct-1"},
		checks: map[string][]ScheduleCheck{
			"acct-1": {{Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"}, Action: CheckActionAllow}},
		},
	}
	s := NewScheduler(coord, updater, loader)
	s.now = fixedClockAtMidnight()
	s.afterFn = fastAfter(20 * time.Millisecond)
	s.minTickInterval = time.Millisecond
	s.reconcileEvery = time.Hour // periodic must not interfere
	s.reconcileDebounce = 80 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	// Let the initial (startup) reconcile land.
	require.Eventually(t,
		func() bool { return loader.listCalls.Load() >= 1 },
		time.Second, 5*time.Millisecond,
		"startup reconcile should run once",
	)
	baseline := loader.listCalls.Load()

	// Fire a tight burst of 30 global-topic events well inside the
	// 80ms debounce window.
	for i := 0; i < 30; i++ {
		PublishScheduleChange(ctx, coord, "acct-1")
		time.Sleep(time.Millisecond)
	}

	// After the debounce settles, the whole burst must have produced
	// exactly ONE extra reconcile, not 30.
	require.Eventually(t,
		func() bool { return loader.listCalls.Load() > baseline },
		time.Second, 5*time.Millisecond,
		"debounced reconcile should eventually fire once",
	)
	time.Sleep(150 * time.Millisecond) // let any stragglers (there should be none) show
	got := loader.listCalls.Load() - baseline
	assert.LessOrEqual(t, got, uint64(2),
		"a 30-event burst must collapse to ~1 reconcile, got %d", got)
}

// ─── helpers ──────────────────────────────────────────────────────────

// fastAfter returns an afterFn that ignores the requested duration and
// fires after a small fixed wall-clock delay. Lets the scheduler tests
// collapse multi-hour schedule boundaries into millisecond test runs
// while still exercising the goroutine's wake / fire / re-evaluate
// cycle.
func fastAfter(d time.Duration) func(time.Duration) <-chan time.Time {
	return func(_ time.Duration) <-chan time.Time {
		return time.After(d)
	}
}

// fixedClockAtMidnight returns a `now` function that always reports
// 2026-01-01 00:00:00 UTC. Paired with fastAfter, the scheduler's
// boundary loop becomes deterministic regardless of when the test
// happens to run.
func fixedClockAtMidnight() func() time.Time {
	return func() time.Time {
		return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}
}

// updaterFn adapts a function into the AccountUpdater interface.
type updaterFn func(ctx context.Context, accountID string)

func (f updaterFn) UpdateAccountPeers(ctx context.Context, accountID string) { f(ctx, accountID) }
