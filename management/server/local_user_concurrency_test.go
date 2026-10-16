package server

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// blockedFor is how long a contender must stay blocked to count as waiting
// on the other replica's lock rather than racing past it.
const blockedFor = 500 * time.Millisecond

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

// requireBlockedThenResult asserts the contender is still waiting after
// blockedFor, releases the holder, and returns the contender's result.
func requireBlockedThenResult(t *testing.T, contender <-chan error, release chan struct{}, holder <-chan error) error {
	t.Helper()
	select {
	case err := <-contender:
		close(release)
		t.Fatalf("contender did not wait for the holder's lock (returned %v)", err)
	case <-time.After(blockedFor):
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

	require.NoError(t, requireBlockedThenResult(t, contender, release, holder),
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

	err = requireBlockedThenResult(t, contender, release, holder)
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

	err = requireBlockedThenResult(t, contender, release, holder)
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
