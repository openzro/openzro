package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ADR-0027 D6: every pass of every reset is its own row, so overlapping
// resets keep each deadline; the due list is ordered and bounded; a pass
// owns its row until it deletes it.
func TestSqlStore_CredentialsRecheckQueue(t *testing.T) {
	runTestForAllEngines(t, "../testdata/store.sql", func(t *testing.T, s Store) {
		ctx := context.Background()
		t0 := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

		require.NoError(t, s.EnqueueCredentialsRecheck(ctx, localTestAccountID, cutoffTestUserID, t0.Add(51*time.Minute), false))
		require.NoError(t, s.EnqueueCredentialsRecheck(ctx, localTestAccountID, cutoffTestUserID, t0.Add(31*time.Minute), false))
		require.NoError(t, s.EnqueueCredentialsRecheck(ctx, localTestAccountID, cutoffTestUserID, t0.Add(32*time.Minute), true))

		due, err := s.ListDueCredentialsRechecks(ctx, t0.Add(30*time.Minute), 10)
		require.NoError(t, err)
		require.Empty(t, due, "nothing is due yet")

		due, err = s.ListDueCredentialsRechecks(ctx, t0.Add(60*time.Minute), 2)
		require.NoError(t, err)
		require.Len(t, due, 2, "the list is bounded")
		require.True(t, t0.Add(31*time.Minute).Equal(due[0].DueAt.UTC()), "oldest first")
		require.False(t, due[0].Fence)
		require.True(t, t0.Add(32*time.Minute).Equal(due[1].DueAt.UTC()))
		require.True(t, due[1].Fence)
		require.Equal(t, localTestAccountID, due[0].AccountID)
		require.Equal(t, cutoffTestUserID, due[0].UserID)

		exists, err := s.CredentialsRecheckExists(ctx, due[0].ID)
		require.NoError(t, err)
		require.True(t, exists)
		require.NoError(t, s.DeleteCredentialsRecheck(ctx, due[0].ID))
		exists, err = s.CredentialsRecheckExists(ctx, due[0].ID)
		require.NoError(t, err)
		require.False(t, exists, "a deleted pass is no longer owned by anyone")
		require.NoError(t, s.DeleteCredentialsRecheck(ctx, due[0].ID), "deleting twice is not an error")

		due, err = s.ListDueCredentialsRechecks(ctx, t0.Add(60*time.Minute), 10)
		require.NoError(t, err)
		require.Len(t, due, 2, "the other reset's pass and the fence remain")
	})
}

// Finishing a sweep and enqueuing its fence is one transaction, owned by
// whoever deletes the sweep row: finishing it again (a retry after a crash
// or another replica) enqueues no second fence, and finishing a fence
// enqueues nothing.
func TestSqlStore_FinishCredentialsRecheck(t *testing.T) {
	runTestForAllEngines(t, "../testdata/store.sql", func(t *testing.T, s Store) {
		ctx := context.Background()
		t0 := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
		require.NoError(t, s.EnqueueCredentialsRecheck(ctx, localTestAccountID, cutoffTestUserID, t0, false))
		due, err := s.ListDueCredentialsRechecks(ctx, t0, 10)
		require.NoError(t, err)
		require.Len(t, due, 1)
		sweep := due[0]

		fenceAt := t0.Add(time.Minute)
		require.NoError(t, s.FinishCredentialsRecheck(ctx, sweep, fenceAt))
		require.NoError(t, s.FinishCredentialsRecheck(ctx, sweep, fenceAt.Add(time.Minute)))
		due, err = s.ListDueCredentialsRechecks(ctx, t0.Add(time.Hour), 10)
		require.NoError(t, err)
		require.Len(t, due, 1, "exactly one fence")
		require.True(t, due[0].Fence)
		require.True(t, fenceAt.Equal(due[0].DueAt.UTC()))

		require.NoError(t, s.FinishCredentialsRecheck(ctx, due[0], fenceAt.Add(time.Hour)))
		due, err = s.ListDueCredentialsRechecks(ctx, t0.Add(2*time.Hour), 10)
		require.NoError(t, err)
		require.Empty(t, due, "a fence enqueues nothing")
	})
}
