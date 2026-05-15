package posture

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/cluster"
)

// scheduleChangedTopicPrefix is the cluster pub/sub topic prefix used to
// invalidate a per-account scheduler loop after the operator creates,
// edits, deletes or toggles a posture check that carries a
// ScheduleCheck. Append the accountID to form the full topic name.
//
// Any management replica may publish; the scheduler leader for that
// account is the one that consumes. Other replicas drop the event on
// the floor (they're not running the loop because they don't hold the
// account's lock). Best-effort delivery is fine: a missed invalidation
// just means the leader doesn't wake until its next scheduled boundary,
// which is the worst case the operator already accepted by using a
// distributed lock primitive in the first place.
const scheduleChangedTopicPrefix = "posture-schedule-changed:"

// scheduleAccountsChangedTopic is the single global pub/sub topic the
// discovery loop subscribes to. Any replica that mutates a posture
// check carrying a ScheduleCheck publishes here (in addition to the
// per-account invalidation topic above). It is the fast path for the
// auto-discovery gap: an account that had ZERO schedule checks at
// scheduler boot has no per-account loop and therefore nobody
// listening on its scheduleChangedTopicPrefix topic — only the
// global discovery loop can notice it gained one and spawn the loop.
//
// Best-effort like every other coordinator topic: a dropped event
// just defers the new account's pickup to the next periodic
// reconcile tick, never loses it.
const scheduleAccountsChangedTopic = "posture-schedule-accounts-changed"

// schedulerLockKeyPrefix scopes the cluster lock to the schedule loop
// so it cannot collide with locks used elsewhere (account-update
// buffer, peer-update fan-out, etc.).
const schedulerLockKeyPrefix = "posture-scheduler:"

// reconcileInterval is the safety-net cadence for the discovery loop.
// The global pub/sub topic is the fast path (sub-second pickup of a
// newly schedule-bearing account); this periodic reconcile is the
// backstop that heals a missed best-effort event or an account that
// gained a schedule on a replica whose publish failed. A few minutes
// is well within the latency an operator expects when first attaching
// a schedule, and the reconcile query is a single GetAllPostureChecks
// (see the N+1 fix) so running it on a timer is cheap.
const reconcileInterval = 3 * time.Minute

// AccountUpdater is the subset of the account-manager surface the
// Scheduler depends on. Defining it here keeps the scheduler package
// importable from management/server without creating a cycle —
// management/server's *DefaultAccountManager already implements
// UpdateAccountPeers with the right signature, so wiring is a one-line
// assignment at the call site.
type AccountUpdater interface {
	UpdateAccountPeers(ctx context.Context, accountID string)
}

// ScheduleLoader fetches the ScheduleChecks active for an account
// (active == attached to at least one enabled policy). Implementations
// are expected to be cheap to call — the loop invokes this once per
// boundary wake. Returns an empty slice for accounts without any
// schedule checks; never nil + nil error.
type ScheduleLoader interface {
	// LoadActiveSchedules returns every ScheduleCheck attached to the
	// account, regardless of policy attachment. The scheduler conservatively
	// schedules a wake even if a check is attached to no policy — operators
	// who detach a check expect the scheduler to stop firing, but the
	// re-attach case is more common and a wake with no peer effect is
	// cheap (UpdateAccountPeers is a no-op when nothing changed).
	LoadActiveSchedules(ctx context.Context, accountID string) ([]ScheduleCheck, error)

	// AccountsWithActiveSchedules returns the IDs of every account that
	// currently owns at least one posture-check record containing a
	// ScheduleCheck. Called once at scheduler startup to seed the
	// per-account goroutines.
	AccountsWithActiveSchedules(ctx context.Context) ([]string, error)
}

// Scheduler wakes management replicas at posture-check window
// boundaries so peers that stay connected across a boundary lose (or
// gain) permissions immediately, instead of waiting for the natural
// Sync poll (~30 s) to refresh their network map.
//
// One goroutine per account that has at least one active ScheduleCheck.
// In HA deployments only ONE management replica is the active leader
// for each account — the cluster.Coordinator.Lock primitive picks the
// winner; other replicas block on Lock() until the leader releases or
// its lock TTL expires. This bounds the per-boundary update fan-out to
// a single UpdateAccountPeers call regardless of replica count.
type Scheduler struct {
	coord   cluster.Coordinator
	updater AccountUpdater
	loader  ScheduleLoader

	// now is injectable so tests can drive the clock without touching
	// global time. Defaults to time.Now in NewScheduler.
	now func() time.Time

	// afterFn is the timer factory the leader loop uses to wait until
	// the next boundary. Injectable so tests can collapse minute-
	// granular HH:MM windows into millisecond test runtimes. Defaults
	// to time.After in NewScheduler.
	afterFn func(d time.Duration) <-chan time.Time

	// minTickInterval is the cheapest defense against pathological
	// schedule configurations (e.g. two checks landing within microseconds
	// of one another). Back-to-back wakes are coalesced into a single
	// UpdateAccountPeers call.
	minTickInterval time.Duration

	// backoff is the wait window before retrying a failed lock
	// acquisition. Jittered to spread herd reacquisition across
	// replicas after a leader crash.
	backoff time.Duration

	// reconcileEvery is the discovery loop's safety-net cadence.
	// Injectable so tests can collapse the 3-minute production
	// default into milliseconds; defaults to reconcileInterval in
	// NewScheduler.
	reconcileEvery time.Duration
}

// NewScheduler wires the dependencies. Callers must call Run on the
// returned scheduler to start the goroutines. Run blocks until ctx is
// cancelled and returns nil for a graceful shutdown or the error that
// caused premature exit.
func NewScheduler(coord cluster.Coordinator, updater AccountUpdater, loader ScheduleLoader) *Scheduler {
	if coord == nil || updater == nil || loader == nil {
		// Hard error rather than silently returning a broken scheduler.
		// Wiring bugs surface at boot, not at the first boundary cross.
		panic("posture.NewScheduler: nil dependency")
	}
	return &Scheduler{
		coord:           coord,
		updater:         updater,
		loader:          loader,
		now:             time.Now,
		afterFn:         time.After,
		minTickInterval: 100 * time.Millisecond,
		backoff:         time.Second,
		reconcileEvery:  reconcileInterval,
	}
}

// Run blocks until ctx is cancelled. It maintains a per-account
// scheduler loop for every account that has an active ScheduleCheck,
// discovering new accounts WITHOUT a management restart via:
//
//   - an initial reconcile at startup,
//   - a global pub/sub subscription (fast path: an account that just
//     gained its first schedule check is picked up sub-second), and
//   - a periodic reconcile tick (safety net for missed best-effort
//     events).
//
// Reconcile is add-only: it spawns loops for newly-seen accounts but
// does NOT tear down loops for accounts that dropped off the list.
// That is deliberate — tickWhileLeader already parks gracefully on
// the per-account invalidation channel when an account has no active
// checks, so a stale loop is a cheap parked goroutine, not lost work.
// Tearing down on a transient/empty discovery result would risk
// killing a live account's scheduler on a flaky read; the asymmetry
// keeps the blast radius minimal.
//
// All reconcile calls happen on this single goroutine, so the
// `running` map needs no mutex.
func (s *Scheduler) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	running := make(map[string]struct{})

	reconcile := func() {
		accounts, err := s.loader.AccountsWithActiveSchedules(ctx)
		if err != nil {
			log.WithContext(ctx).Warnf("posture scheduler: reconcile account list failed: %v", err)
			return
		}
		spawned := 0
		for _, accountID := range accounts {
			if _, ok := running[accountID]; ok {
				continue
			}
			running[accountID] = struct{}{}
			spawned++
			wg.Add(1)
			id := accountID
			go func() {
				defer wg.Done()
				s.runForAccount(ctx, id)
			}()
		}
		if spawned > 0 {
			log.WithContext(ctx).Infof("posture scheduler: started %d new per-account loop(s) (%d total)", spawned, len(running))
		}
	}

	// Fast path: any replica mutating a schedule-bearing posture check
	// publishes to the global topic. Subscribe BEFORE the first
	// reconcile so a change racing startup isn't missed.
	changed, err := s.coord.Subscribe(ctx, scheduleAccountsChangedTopic)
	if err != nil {
		// Non-fatal: the periodic reconcile still discovers new
		// accounts, just with up-to-reconcileInterval latency instead
		// of sub-second. Degrade rather than refuse to start.
		log.WithContext(ctx).Warnf("posture scheduler: subscribe to %s failed: %v — falling back to periodic reconcile only", scheduleAccountsChangedTopic, err)
		changed = nil
	}

	reconcile()

	ticker := time.NewTicker(s.reconcileEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Children derive from ctx and unwind on their own; wait
			// so Run only returns once they have actually stopped.
			wg.Wait()
			return ctx.Err()
		case <-ticker.C:
			reconcile()
		case _, ok := <-changed:
			if !ok {
				// Subscription channel closed (broker outage / Close).
				// Drop to periodic-only; do not spin on a closed chan.
				changed = nil
				continue
			}
			reconcile()
		}
	}
}

// runForAccount is the outer loop that re-acquires the cluster lock
// whenever it is lost (leader rotation, replica restart, transient
// broker outage). The inner tickWhileLeader loop only runs while THIS
// replica is the elected leader for the account.
func (s *Scheduler) runForAccount(ctx context.Context, accountID string) {
	lockKey := schedulerLockKeyPrefix + accountID
	invalidations, err := s.coord.Subscribe(ctx, scheduleChangedTopicPrefix+accountID)
	if err != nil {
		log.WithContext(ctx).Errorf("posture scheduler: subscribe failed for %s: %v", accountID, err)
		return
	}

	for {
		if ctx.Err() != nil {
			return
		}
		release, err := s.coord.Lock(ctx, lockKey)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			log.WithContext(ctx).Warnf("posture scheduler: lock %s failed: %v", lockKey, err)
			if !s.sleepBackoff(ctx) {
				return
			}
			continue
		}
		// Per-iteration recover so a panic inside tickWhileLeader (or
		// anything it transitively calls) logs and falls through to
		// re-acquire the lock instead of killing the goroutine. A
		// function-scope recover would catch the panic too, but then
		// the account would lose its scheduler entirely until the next
		// management restart — defeating the whole point of HA.
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.WithContext(ctx).Errorf("posture scheduler: panic in tick for %s: %v", accountID, r)
				}
			}()
			s.tickWhileLeader(ctx, accountID, invalidations)
		}()
		release()
	}
}

// tickWhileLeader is the boundary-firing loop. It exits when ctx is
// cancelled, the schedule-loader returns no active checks (so the
// account no longer has any work) — in that case it waits on the
// invalidation channel for a new schedule to arrive before exiting.
// Returning hands the lock back to the outer loop which re-acquires
// (or steps aside if another replica grabs it).
func (s *Scheduler) tickWhileLeader(
	ctx context.Context,
	accountID string,
	invalidations <-chan cluster.Event,
) {
	var lastFire time.Time
	for {
		if ctx.Err() != nil {
			return
		}
		checks, err := s.loader.LoadActiveSchedules(ctx, accountID)
		if err != nil {
			log.WithContext(ctx).Warnf("posture scheduler: load %s: %v", accountID, err)
			if !s.sleepBackoff(ctx) {
				return
			}
			continue
		}
		if len(checks) == 0 {
			// Account has no active schedules right now. Wait for an
			// invalidation (operator added a new schedule) or ctx
			// cancel. We deliberately don't return — exiting would
			// release the lock and another replica would grab it
			// just to find the same empty state. Holding the lock
			// here costs nothing and removes the lock-thrash.
			select {
			case <-ctx.Done():
				return
			case <-invalidations:
				continue
			}
		}
		next, ok := NextBoundary(s.now(), checks)
		if !ok {
			// Defensive: NextBoundary said no boundary, but len(checks)
			// > 0 — could happen if every check has a malformed
			// timezone or HH:MM (Validate would have rejected, but
			// the DB might have been hand-edited). Wait for next
			// invalidation rather than spin.
			select {
			case <-ctx.Done():
				return
			case <-invalidations:
				continue
			}
		}
		wait := next.Sub(s.now())
		if wait < 0 {
			// Boundary in the past — recompute immediately. Should be
			// impossible because NextBoundary filters with After(),
			// but keep the guard for clock-skew or test races.
			wait = 0
		}
		select {
		case <-ctx.Done():
			return
		case <-s.afterFn(wait):
			if !lastFire.IsZero() && s.now().Sub(lastFire) < s.minTickInterval {
				continue
			}
			lastFire = s.now()
			s.fireUpdate(ctx, accountID)
		case <-invalidations:
			// Loop to recompute against the freshly-changed schedules.
		}
	}
}

// fireUpdate calls UpdateAccountPeers with a panic guard. The account
// manager's update path is itself defensive, but the boundary loop is
// long-lived enough that a single peer-edge bug shouldn't take it
// down.
func (s *Scheduler) fireUpdate(ctx context.Context, accountID string) {
	defer func() {
		if r := recover(); r != nil {
			log.WithContext(ctx).Errorf("posture scheduler: UpdateAccountPeers panic for %s: %v", accountID, r)
		}
	}()
	log.WithContext(ctx).Debugf("posture scheduler: boundary cross fired for account %s", accountID)
	s.updater.UpdateAccountPeers(ctx, accountID)
}

// sleepBackoff sleeps for backoff + 0–backoff/2 jitter. Returns false
// when ctx was cancelled during the sleep (caller should bail).
func (s *Scheduler) sleepBackoff(ctx context.Context) bool {
	d := s.backoff + time.Duration(rand.Int63n(int64(s.backoff/2+1)))
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// PublishScheduleChange is the helper external callers (the account
// manager's posture-check save path) use to nudge the scheduler when
// an operator creates / edits / deletes a posture check with a
// ScheduleCheck. The publish is best-effort: failures are logged but
// do not block the save itself.
//
// Two topics, both required:
//
//   - the per-account invalidation topic, consumed by an already-
//     running per-account loop so it recomputes its next boundary
//     immediately instead of waiting out a stale timer;
//   - the global accounts-changed topic, consumed by the discovery
//     loop so an account that had NO loop (its first schedule check)
//     gets one spawned sub-second instead of waiting for the periodic
//     reconcile.
//
// The first nudge is a no-op for a brand-new account (nobody is
// subscribed to its per-account topic yet); the second is a no-op for
// an account that already has a loop (reconcile is add-only and skips
// it). Publishing both covers both transitions with one call.
func PublishScheduleChange(ctx context.Context, coord cluster.Coordinator, accountID string) {
	if coord == nil || accountID == "" {
		return
	}
	if err := coord.Publish(ctx, scheduleChangedTopicPrefix+accountID, nil); err != nil {
		log.WithContext(ctx).Warnf("posture scheduler: publish invalidation for %s: %v", accountID, err)
	}
	if err := coord.Publish(ctx, scheduleAccountsChangedTopic, nil); err != nil {
		log.WithContext(ctx).Warnf("posture scheduler: publish accounts-changed for %s: %v", accountID, err)
	}
}
