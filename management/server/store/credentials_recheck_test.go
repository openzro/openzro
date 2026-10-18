package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

func requireRecheck(t *testing.T, s Store, userID string, want *time.Time) {
	t.Helper()
	got := getUser(t, s, userID).CredentialsRecheckAt
	if want == nil {
		require.Nil(t, got, "no recheck expected")
		return
	}
	require.NotNil(t, got, "a recheck must be scheduled")
	require.True(t, want.Equal(*got), "recheck: want %s, got %s", *want, *got)
}

// ADR-0027 D6: a reset schedules a second pass. The schedule only moves
// later, a due list finds it, and clearing it is conditional on the value
// the pass ran for, so a newer reset's schedule survives an older pass.
func TestSqlStore_CredentialsRecheck(t *testing.T) {
	runTestForAllEngines(t, "../testdata/store.sql", func(t *testing.T, s Store) {
		ctx := context.Background()
		requireRecheck(t, s, cutoffTestUserID, nil)

		at := time.Date(2026, 10, 18, 12, 31, 5, 0, time.UTC)
		require.NoError(t, s.ScheduleUserCredentialsRecheck(ctx, localTestAccountID, cutoffTestUserID, at))
		require.NoError(t, s.ScheduleUserCredentialsRecheck(ctx, localTestAccountID, cutoffTestUserID, at.Add(-time.Hour)))
		requireRecheck(t, s, cutoffTestUserID, &at)
		requireStatus(t, s.ScheduleUserCredentialsRecheck(ctx, localTestAccountID, "no-such-user", at), status.NotFound)

		due, err := s.ListUserCredentialsRechecksDue(ctx, at.Add(-time.Second))
		require.NoError(t, err)
		require.Empty(t, due, "not due yet")
		due, err = s.ListUserCredentialsRechecksDue(ctx, at)
		require.NoError(t, err)
		require.Len(t, due, 1)
		require.Equal(t, CredentialsRecheck{AccountID: localTestAccountID, UserID: cutoffTestUserID, At: at},
			CredentialsRecheck{AccountID: due[0].AccountID, UserID: due[0].UserID, At: due[0].At.UTC()})

		// A newer reset moved the schedule: the older pass must not clear it.
		later := at.Add(10 * time.Minute)
		require.NoError(t, s.ScheduleUserCredentialsRecheck(ctx, localTestAccountID, cutoffTestUserID, later))
		require.NoError(t, s.ClearUserCredentialsRecheck(ctx, localTestAccountID, cutoffTestUserID, at))
		requireRecheck(t, s, cutoffTestUserID, &later)
		require.NoError(t, s.ClearUserCredentialsRecheck(ctx, localTestAccountID, cutoffTestUserID, later))
		requireRecheck(t, s, cutoffTestUserID, nil)
	})
}

// Generic saves built from reads taken before a reset leave the schedule
// alone, like the cutoff.
func TestSqlStore_GenericSavesKeepCredentialsRecheck(t *testing.T) {
	runTestForAllEngines(t, "../testdata/store.sql", func(t *testing.T, s Store) {
		ctx := context.Background()
		staleUser := getUser(t, s, cutoffTestUserID)
		staleAccount, err := s.GetAccount(ctx, localTestAccountID)
		require.NoError(t, err)
		at := time.Date(2026, 10, 18, 12, 31, 5, 0, time.UTC)
		require.NoError(t, s.ScheduleUserCredentialsRecheck(ctx, localTestAccountID, cutoffTestUserID, at))

		require.NoError(t, s.SaveUser(ctx, LockingStrengthUpdate, staleUser))
		requireRecheck(t, s, cutoffTestUserID, &at)
		require.NoError(t, s.SaveUsers(ctx, LockingStrengthUpdate, []*types.User{staleUser}))
		requireRecheck(t, s, cutoffTestUserID, &at)
		require.NoError(t, s.SaveAccount(ctx, staleAccount))
		requireRecheck(t, s, cutoffTestUserID, &at)
	})
}
