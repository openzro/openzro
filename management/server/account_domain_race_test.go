package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/xid"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// The concurrent paths reach lostPrimaryDomainRace through whichever shape the
// engine happens to raise, and after the signup path moved to CreateAccount
// every measured conflict — both engines, both paths — arrives as the typed
// AlreadyExists. A plain INSERT blocks until the winner commits and then
// reports a duplicate key; the upsert it replaced deadlocked instead.
//
// So the MySQL deadlock branch is a contingency that the cross-replica tests no
// longer exercise. It is kept because InnoDB gap locks under REPEATABLE READ
// are a real mechanism for this predicate and were observed on this path
// earlier in this work, but a branch no test reaches is a branch nobody can
// trust. This pins it directly.
func TestLostPrimaryDomainRace(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "no error",
			err:  nil,
			want: false,
		},
		{
			name: "the typed violation the store classifies, and what both engines actually raise",
			err:  status.NewPrimaryPrivateDomainExistsError("contested.example"),
			want: true,
		},
		{
			name: "the mysql gap-lock deadlock, which arrives untyped",
			err:  errors.New("Error 1213 (40001): Deadlock found when trying to get lock; try restarting transaction"),
			want: true,
		},
		{
			// Postgres has no gap locks on this path, so a deadlock here comes
			// from unrelated work in the same transaction. Reading it as a lost
			// domain race would silently send the caller into another account.
			name: "a postgres deadlock, which is not this race",
			err:  errors.New("ERROR: deadlock detected (SQLSTATE 40P01)"),
			want: false,
		},
		{
			name: "an unrelated failure",
			err:  status.Errorf(status.Internal, "failed to create account in store"),
			want: false,
		},
		{
			name: "a different typed error",
			err:  status.NewAccountNotFoundError("acc-1"),
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, lostPrimaryDomainRace(tc.err))
		})
	}
}

// waitForPrimaryDomainWinner exists for the deadlock shape above: it is the
// only one that tells the loser it lost before the winner has committed, so an
// immediate lookup finds nothing. With CreateAccount the winner is always
// already visible and the loop returns on its first attempt, which is what the
// cross-replica runs measure. This drives the other case.
func TestWaitForPrimaryDomainWinner(t *testing.T) {
	ctx := context.Background()
	const domain = "late-winner.example"

	t.Run("returns the winner once it becomes visible", func(t *testing.T) {
		s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
		t.Cleanup(cleanup)
		require.NoError(t, err)

		am := &DefaultAccountManager{Store: s}

		winner := &types.Account{
			Id:                     xid.New().String(),
			Domain:                 domain,
			DomainCategory:         types.PrivateCategory,
			IsDomainPrimaryAccount: true,
			Network:                types.NewNetwork(),
		}
		// Lands after the first attempt has already missed.
		go func() {
			time.Sleep(20 * time.Millisecond)
			_ = s.CreateAccount(ctx, winner)
		}()

		got, err := am.waitForPrimaryDomainWinner(ctx, domain)
		require.NoError(t, err, "the winner committed inside the budget and must be found")
		require.Equal(t, winner.Id, got)
	})

	t.Run("gives up rather than inventing a winner", func(t *testing.T) {
		s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
		t.Cleanup(cleanup)
		require.NoError(t, err)

		am := &DefaultAccountManager{Store: s}

		start := time.Now()
		_, err = am.waitForPrimaryDomainWinner(ctx, "nobody-owns-this.example")
		require.Error(t, err, "no account holds the domain; the caller has to report its original failure")
		require.Less(t, time.Since(start), time.Second, "the wait must stay under a second in total")
	})

	t.Run("gives up on the caller's context", func(t *testing.T) {
		s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
		t.Cleanup(cleanup)
		require.NoError(t, err)

		am := &DefaultAccountManager{Store: s}

		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()

		_, err = am.waitForPrimaryDomainWinner(canceledCtx, "nobody-owns-this.example")
		require.ErrorIs(t, err, context.Canceled)
	})
}
