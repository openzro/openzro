package server

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/auth/jwt"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// holdInTx runs op inside a transaction on s, then keeps the transaction (and
// every lock op took) open until release is closed. started is closed once op
// has returned.
func holdInTx(t *testing.T, s store.Store, op func(tx store.Store) error) (started, release chan struct{}, done <-chan error) {
	t.Helper()
	started, release = make(chan struct{}), make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.ExecuteInTransaction(context.Background(), func(tx store.Store) error {
			if err := op(tx); err != nil {
				close(started)
				return err
			}
			close(started)
			<-release
			return nil
		})
	}()
	return started, release, errCh
}

// lockWaiters counts transactions currently blocked on a row lock, as the
// database itself reports it.
func lockWaiters(t *testing.T, s store.Store) int64 {
	t.Helper()
	db := s.(*store.SqlStore).GetDB()
	var n int64
	var err error
	switch s.GetStoreEngine() {
	case types.PostgresStoreEngine:
		err = db.Raw("SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND datname = current_database()").Scan(&n).Error
	case types.MysqlStoreEngine:
		// performance_schema reports every InnoDB lock wait, including an
		// INSERT ... ON DUPLICATE KEY waiting on a row lock, which
		// information_schema.innodb_trx does not show as LOCK WAIT.
		err = db.Raw("SELECT count(*) FROM performance_schema.data_lock_waits").Scan(&n).Error
	default:
		t.Fatalf("lock-wait probe not supported on %s", s.GetStoreEngine())
	}
	require.NoError(t, err)
	return n
}

// requireBlockedThenResult waits until the database reports the contender
// blocked on a lock (not a timing guess), asserts it has not returned,
// releases the holder, and returns the contender's result.
func requireBlockedThenResult(t *testing.T, probe store.Store, contender <-chan error, release chan struct{}, holder <-chan error) error {
	t.Helper()
	deadline := time.After(barrierTimeout)
	for lockWaiters(t, probe) == 0 {
		select {
		case err := <-contender:
			close(release)
			t.Fatalf("contender did not wait for the holder's lock (returned %v)", err)
		case <-deadline:
			close(release)
			t.Fatal("the database never reported the contender waiting on a lock")
		case <-time.After(20 * time.Millisecond):
		}
	}
	close(release)
	require.NoError(t, <-holder)
	select {
	case err := <-contender:
		return err
	case <-time.After(barrierTimeout):
		t.Fatal("contender never returned after the holder committed")
		return nil
	}
}

func localUserFor(accountID, id string) *types.User {
	u := types.NewRegularUser(id)
	u.AccountID = accountID
	u.SCIMUserName = id + "@example.com"
	return u
}

// #213 review: SaveAccount deletes and re-inserts the account row it locked.
// A reservation waiting on that lock wakes to a deleted tuple; under Postgres
// READ COMMITTED its SELECT ... FOR UPDATE then returns no row even though
// the account exists again. The reservation must still succeed.
func TestLocalUser_ReservationWaitsForConcurrentSaveAccount(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()
	accountID, err := r.A.GetAccountIDByUserID(ctx, "owner", "")
	require.NoError(t, err)
	snapshot, err := r.A.Store.GetAccount(ctx, accountID)
	require.NoError(t, err)

	started, release, holder := holdInTx(t, r.A.Store, func(tx store.Store) error {
		return tx.SaveAccount(ctx, snapshot)
	})
	<-started

	contender := make(chan error, 1)
	go func() { contender <- r.B.Store.CreatePendingLocalUser(ctx, localUserFor(accountID, "ana")) }()

	require.NoError(t, requireBlockedThenResult(t, r.A.Store, contender, release, holder),
		"a reservation racing SaveAccount must not fail with a spurious account-not-found")
	u, err := r.B.Store.GetUserByUserID(ctx, store.LockingStrengthShare, "ana")
	require.NoError(t, err)
	require.Equal(t, types.LocalCredentialPending, u.LocalCredentialState)
}

// The other acquisition order: a reservation holds the account lock, and a
// SaveAccount whose snapshot predates it must wait, then refuse instead of
// deleting the reservation.
func TestLocalUser_StaleSaveAccountWaitsForReservation(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()
	accountID, err := r.A.GetAccountIDByUserID(ctx, "owner", "")
	require.NoError(t, err)
	stale, err := r.A.Store.GetAccount(ctx, accountID)
	require.NoError(t, err)

	started, release, holder := holdInTx(t, r.B.Store, func(tx store.Store) error {
		return tx.CreatePendingLocalUser(ctx, localUserFor(accountID, "ana"))
	})
	<-started

	contender := make(chan error, 1)
	go func() { contender <- r.A.Store.SaveAccount(ctx, stale) }()

	err = requireBlockedThenResult(t, r.A.Store, contender, release, holder)
	sErr, ok := status.FromError(err)
	require.True(t, ok && err != nil, "expected a status error, got %v", err)
	require.Equal(t, status.PreconditionFailed, sErr.Type())

	u, err := r.A.Store.GetUserByUserID(ctx, store.LockingStrengthShare, "ana")
	require.NoError(t, err)
	require.Equal(t, types.LocalCredentialPending, u.LocalCredentialState)
}

// The dangerous variant: a second SaveAccount waiting on the first one wakes
// to the deleted account row. If that were read as "new account", the local
// user check would be skipped and the stale snapshot would delete the user.
func TestLocalUser_ConcurrentSaveAccountsKeepLocalUser(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()
	accountID, err := r.A.GetAccountIDByUserID(ctx, "owner", "")
	require.NoError(t, err)
	stale, err := r.B.Store.GetAccount(ctx, accountID)
	require.NoError(t, err)
	require.NoError(t, r.A.Store.CreatePendingLocalUser(ctx, localUserFor(accountID, "ana")))
	fresh, err := r.A.Store.GetAccount(ctx, accountID)
	require.NoError(t, err)

	started, release, holder := holdInTx(t, r.A.Store, func(tx store.Store) error {
		return tx.SaveAccount(ctx, fresh)
	})
	<-started

	contender := make(chan error, 1)
	go func() { contender <- r.B.Store.SaveAccount(ctx, stale) }()

	err = requireBlockedThenResult(t, r.A.Store, contender, release, holder)
	sErr, ok := status.FromError(err)
	require.True(t, ok && err != nil, "expected a status error, got %v", err)
	require.Equal(t, status.PreconditionFailed, sErr.Type())

	u, err := r.B.Store.GetUserByUserID(ctx, store.LockingStrengthShare, "ana")
	require.NoError(t, err, "the stale snapshot must not have deleted the local user")
	require.Equal(t, types.LocalCredentialPending, u.LocalCredentialState)
}

// #213 review: under heavy contention a bounded retry on the account row
// eventually let SaveAccount proceed unlocked. The lock now lives in a row
// SaveAccount never deletes, so this asserts the invariants under churn:
// every reservation survives and succeeds, and every SaveAccount either
// succeeds or is refused as stale — never anything else.
func TestLocalUser_ReservationsSurviveSaveAccountChurn(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()
	accountID, err := r.A.GetAccountIDByUserID(ctx, "owner", "")
	require.NoError(t, err)

	const savers, rounds, reservations = 4, 8, 12
	var wg sync.WaitGroup
	saveErrs := make(chan error, savers*rounds)
	reserveErrs := make(chan error, reservations)

	for i := 0; i < savers; i++ {
		replica := []*DefaultAccountManager{r.A, r.B}[i%2]
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rounds; j++ {
				snapshot, err := replica.Store.GetAccount(ctx, accountID)
				if err != nil {
					saveErrs <- err
					continue
				}
				saveErrs <- replica.Store.SaveAccount(ctx, snapshot)
			}
		}()
	}
	for i := 0; i < reservations; i++ {
		replica := []*DefaultAccountManager{r.A, r.B}[i%2]
		id := fmt.Sprintf("churn-%d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			reserveErrs <- replica.Store.CreatePendingLocalUser(ctx, localUserFor(accountID, id))
		}()
	}
	wg.Wait()
	close(saveErrs)
	close(reserveErrs)

	for err := range reserveErrs {
		require.NoError(t, err, "a reservation must not fail because SaveAccount churned the account row")
	}
	for err := range saveErrs {
		if err == nil {
			continue
		}
		sErr, ok := status.FromError(err)
		require.True(t, ok && sErr.Type() == status.PreconditionFailed,
			"SaveAccount may only succeed or be refused as stale, got %v", err)
	}
	for i := 0; i < reservations; i++ {
		u, err := r.A.Store.GetUserByUserID(ctx, store.LockingStrengthShare, fmt.Sprintf("churn-%d", i))
		require.NoError(t, err, "a stale snapshot deleted reservation churn-%d", i)
		require.Equal(t, types.LocalCredentialPending, u.LocalCredentialState)
	}
}

// A stale SaveAccount must not undo a concurrent terminal transition: the
// snapshot still says "active", the database says "deactivated". Every
// local-state operation serializes on the same account lock as SaveAccount,
// which then refuses the stale snapshot.
func TestLocalUser_SaveAccountCannotRevertConcurrentTermination(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()
	accountID, err := r.A.GetAccountIDByUserID(ctx, "owner", "")
	require.NoError(t, err)
	require.NoError(t, r.A.Store.CreatePendingLocalUser(ctx, localUserFor(accountID, "ana")))
	require.NoError(t, r.A.Store.FinalizeLocalUser(ctx, accountID, "ana"))
	stale, err := r.B.Store.GetAccount(ctx, accountID)
	require.NoError(t, err)

	started, release, holder := holdInTx(t, r.A.Store, func(tx store.Store) error {
		return tx.MarkLocalUserTerminal(ctx, accountID, "ana", types.LocalCredentialDeactivated)
	})
	<-started

	contender := make(chan error, 1)
	go func() { contender <- r.B.Store.SaveAccount(ctx, stale) }()

	require.Error(t, requireBlockedThenResult(t, r.A.Store, contender, release, holder),
		"a snapshot taken before the deactivation must be refused")
	u, err := r.A.Store.GetUserByUserID(ctx, store.LockingStrengthShare, "ana")
	require.NoError(t, err)
	require.Equal(t, types.LocalCredentialDeactivated, u.LocalCredentialState)
	require.True(t, u.Blocked)
}

// #216 review: DeleteAccount counted local users without the account mutex,
// so a reservation could commit between that count and the account-wide
// deletes; the delete then removed the new user while its Dex credential
// stayed behind. A third transaction holding a policy row lock pauses the
// delete right after its count, while the other replica runs the full SCIM
// create. Invariant: no Dex credential may outlive its user record.
func TestLocalUser_DeleteAccountRaceLeavesNoOrphanCredential(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()
	accountID, err := r.A.GetAccountIDByUserID(ctx, "owner", "")
	require.NoError(t, err)
	policies, err := r.A.Store.GetAccountPolicies(ctx, store.LockingStrengthShare, accountID)
	require.NoError(t, err)
	require.NotEmpty(t, policies, "the default policy anchors the pause")
	snapshot, err := r.B.Store.GetAccount(ctx, accountID)
	require.NoError(t, err)

	// C holds a policy row: DeleteAccount's first delete waits on it.
	started, releaseC, holderC := holdInTx(t, r.A.Store, func(tx store.Store) error {
		_, err := tx.GetPolicyByID(ctx, store.LockingStrengthUpdate, accountID, policies[0].ID)
		return err
	})
	<-started

	deleteDone := make(chan error, 1)
	go func() { deleteDone <- r.B.Store.DeleteAccount(ctx, snapshot) }()
	waitForLockWaiters(t, r.A.Store, 1)

	creds := newFakeLocalCredentials()
	r.A.EnableLocalUsers(creds)
	createDone := make(chan error, 1)
	go func() {
		_, err := r.A.SCIMCreateLocalUser(ctx, accountID, "owner", localInput("ana@example.com"), goodPassword)
		createDone <- err
	}()
	// Either the create waits for the delete's account mutex (a second lock
	// waiter) or, without the mutex, it runs to completion first.
	var createFinished bool
	deadline := time.After(barrierTimeout)
	for !createFinished && lockWaiters(t, r.A.Store) < 2 {
		select {
		case err := <-createDone:
			createFinished = true
			createDone <- err
		case <-deadline:
			t.Fatal("the create neither finished nor waited on a lock")
		case <-time.After(20 * time.Millisecond):
		}
	}

	close(releaseC)
	require.NoError(t, <-holderC)
	require.NoError(t, <-deleteDone)
	<-createDone

	for email, entry := range creds.entries {
		sub := jwt.EncodeDexSubject(entry.userID, dexLocalConnector)
		_, err := r.A.Store.GetUserByUserID(ctx, store.LockingStrengthShare, sub)
		require.NoError(t, err, "Dex credential for %s outlived its openZro user record", email)
	}
}

// waitForLockWaiters blocks until the database reports at least n
// transactions waiting on a lock.
func waitForLockWaiters(t *testing.T, probe store.Store, n int64) {
	t.Helper()
	deadline := time.After(barrierTimeout)
	for lockWaiters(t, probe) < n {
		select {
		case <-deadline:
			t.Fatalf("the database never reported %d lock waiter(s)", n)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// lockingCoordinator is a cluster.Coordinator whose Lock really excludes,
// shared by the managers of both replicas to stand in for the broker. It
// counts waiters per key so a test can observe a contender blocking on a
// lock instead of guessing with a sleep.
type lockingCoordinator struct {
	*fakeCoordinator
	mu      sync.Mutex
	locks   map[string]*sync.Mutex
	waiting map[string]int
}

func newLockingCoordinator() *lockingCoordinator {
	return &lockingCoordinator{
		fakeCoordinator: newFakeCoordinator(),
		locks:           map[string]*sync.Mutex{},
		waiting:         map[string]int{},
	}
}

func (c *lockingCoordinator) Lock(_ context.Context, name string) (func(), error) {
	c.mu.Lock()
	m, ok := c.locks[name]
	if !ok {
		m = &sync.Mutex{}
		c.locks[name] = m
	}
	c.waiting[name]++
	c.mu.Unlock()

	m.Lock()

	c.mu.Lock()
	c.waiting[name]--
	c.mu.Unlock()
	return m.Unlock, nil
}

func (c *lockingCoordinator) waiters(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.waiting[name]
}

// pausingStore pauses the first GetAccount right after it returned its
// snapshot, until released: the moment a stale-snapshot race opens.
type pausingStore struct {
	store.Store
	once     sync.Once
	snapshot chan struct{}
	proceed  chan struct{}
}

func (p *pausingStore) GetAccount(ctx context.Context, accountID string) (*types.Account, error) {
	account, err := p.Store.GetAccount(ctx, accountID)
	p.once.Do(func() {
		close(p.snapshot)
		<-p.proceed
	})
	return account, err
}

// #216 review: the manager's DeleteAccount read its snapshot before any lock
// shared with local-user creation. A local user created after that read
// slipped past the preflight; the manager then deleted the owner and only
// the store refused at the end, leaving an ownerless account. The two must
// serialize before the snapshot, with one of two linear outcomes: the
// delete wins and the create fails, or the create wins and the delete is
// refused before deleting anyone.
//
// The delete is paused right after its GetAccount snapshot, with no table
// locks involved, and the create runs meanwhile on the other replica.
func TestLocalUser_DeleteAccountAndCreateAreLinearizable(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()
	coord := newLockingCoordinator()
	r.A.SetCoordinator(coord)
	r.B.SetCoordinator(coord)
	creds := newFakeLocalCredentials()
	r.A.EnableLocalUsers(creds)

	accountID, err := r.A.GetAccountIDByUserID(ctx, "owner", "")
	require.NoError(t, err)
	_, err = r.A.SCIMCreateUser(ctx, accountID, "owner", account.SCIMUserInput{UserName: "bea@example.com", Active: true})
	require.NoError(t, err)

	paused := &pausingStore{Store: r.B.Store, snapshot: make(chan struct{}), proceed: make(chan struct{})}
	r.B.Store = paused

	deleteDone := make(chan error, 1)
	go func() { deleteDone <- r.B.DeleteAccount(ctx, accountID, "owner") }()
	<-paused.snapshot

	createDone := make(chan error, 1)
	go func() {
		_, err := r.A.SCIMCreateLocalUser(ctx, accountID, "owner", localInput("ana@example.com"), goodPassword)
		createDone <- err
	}()
	// Wait until the create either finished (no serialization) or is
	// waiting on the account lifecycle lock the paused delete holds.
	lifecycleKey := accountLifecycleLockPrefix + accountID
	deadline := time.After(barrierTimeout)
	for waiting := false; !waiting; {
		select {
		case err := <-createDone:
			createDone <- err
			waiting = true
		case <-deadline:
			t.Fatal("the create neither finished nor waited on the account lifecycle lock")
		case <-time.After(10 * time.Millisecond):
			waiting = coord.waiters(lifecycleKey) > 0
		}
	}

	close(paused.proceed)
	deleteErr := <-deleteDone
	createErr := <-createDone

	_, accountErr := r.A.Store.GetAccount(ctx, accountID)
	if accountErr == nil {
		// The account survived: the create must have won, and the delete
		// must have been refused before removing the owner.
		require.Error(t, deleteErr)
		require.NoError(t, createErr)
		_, err := r.A.Store.GetUserByUserID(ctx, store.LockingStrengthShare, "owner")
		require.NoError(t, err, "a refused account delete must not have removed the owner")
	} else {
		// The account is gone: the delete won and the create failed.
		require.NoError(t, deleteErr)
		require.Error(t, createErr)
	}
	for email, entry := range creds.entries {
		_, err := r.A.Store.GetUserByUserID(ctx, store.LockingStrengthShare, jwt.EncodeDexSubject(entry.userID, dexLocalConnector))
		require.NoError(t, err, "Dex credential for %s outlived its openZro user record", email)
	}
}
