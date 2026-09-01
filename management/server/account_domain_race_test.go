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

// The concurrent login paths reach lostPrimaryDomainRace when the database
// reports that another account already owns the primary private domain. Under
// READ COMMITTED the create/update waits for the winner and then returns the
// typed unique-index violation; broader deadlock strings are deliberately not
// classified as this race, because that could send a user into the wrong
// account after an unrelated failure.
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
			name: "a mysql deadlock, which is not specific enough to move a user into another account",
			err:  errors.New("Error 1213 (40001): Deadlock found when trying to get lock; try restarting transaction"),
			want: false,
		},
		{
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

// waitForPrimaryDomainWinner is intentionally bounded. A typed conflict should
// usually arrive after the winner commits, but the caller still must not invent
// a winner if the lookup cannot find one.
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

		var budget time.Duration
		for _, d := range primaryDomainWinnerBackoff {
			budget += d
		}
		require.Less(t, budget, time.Second, "the backoff schedule must stay under a second in total")

		start := time.Now()
		_, err = am.waitForPrimaryDomainWinner(ctx, "nobody-owns-this.example")
		elapsed := time.Since(start)
		require.Error(t, err, "no account holds the domain; the caller has to report its original failure")
		// Derived from the schedule rather than a wall-clock ceiling: asserting
		// it finished "within a second" would measure the machine, and the
		// budget itself is checked above. This checks it actually waited out
		// every attempt instead of giving up early.
		require.GreaterOrEqual(t, elapsed, budget, "gave up before exhausting the backoff")
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
