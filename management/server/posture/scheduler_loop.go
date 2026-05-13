package posture

import (
	"context"
	"errors"
	"fmt"
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

// schedulerLockKeyPrefix scopes the cluster lock to the schedule loop
// so it cannot collide with locks used elsewhere (account-update
// buffer, peer-update fan-out, etc.).
const schedulerLockKeyPrefix = "posture-scheduler:"

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
	}
}

// Run blocks until ctx is cancelled. At startup it discovers every
// account with active ScheduleChecks and spawns a per-account
// goroutine. New accounts that gain a ScheduleCheck after Run started
// are not auto-discovered in this v1 — operators restart the management
// to pick them up, which is the same cadence as adding any new
// component to a long-running service.
func (s *Scheduler) Run(ctx context.Context) error {
	accounts, err := s.loader.AccountsWithActiveSchedules(ctx)
	if err != nil {
		return fmt.Errorf("posture scheduler: initial account list: %w", err)
	}
	if len(accounts) == 0 {
		log.WithContext(ctx).Infof("posture scheduler: no accounts with active schedule checks; blocking until ctx cancel")
		<-ctx.Done()
		return ctx.Err()
	}
	log.WithContext(ctx).Infof("posture scheduler: starting per-account loops for %d account(s)", len(accounts))

	var wg sync.WaitGroup
	for _, accountID := range accounts {
		wg.Add(1)
		id := accountID
		go func() {
			defer wg.Done()
			s.runForAccount(ctx, id)
		}()
	}
	wg.Wait()
	return ctx.Err()
}

// runForAccount is the outer loop that re-acquires the cluster lock
// whenever it is lost (leader rotation, replica restart, transient
// broker outage). The inner tickWhileLeader loop only runs while THIS
// replica is the elected leader for the account.
func (s *Scheduler) runForAccount(ctx context.Context, accountID string) {
	defer func() {
		// A panic anywhere in the loop must not bring down the
		// scheduler — other accounts continue uninterrupted.
		if r := recover(); r != nil {
			log.WithContext(ctx).Errorf("posture scheduler: panic in account %s: %v", accountID, r)
		}
	}()

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
		s.tickWhileLeader(ctx, accountID, invalidations)
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
func PublishScheduleChange(ctx context.Context, coord cluster.Coordinator, accountID string) {
	if coord == nil || accountID == "" {
		return
	}
	if err := coord.Publish(ctx, scheduleChangedTopicPrefix+accountID, nil); err != nil {
		log.WithContext(ctx).Warnf("posture scheduler: publish invalidation for %s: %v", accountID, err)
	}
}
