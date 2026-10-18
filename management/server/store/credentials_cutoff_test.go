package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

const cutoffTestUserID = "f4f6d672-63fb-11ec-90d6-0242ac120003"

func requireCutoff(t *testing.T, s Store, userID string, want time.Time) {
	t.Helper()
	got := getUser(t, s, userID).CredentialsValidAfter
	require.NotNil(t, got, "the credentials cutoff must be set")
	require.True(t, want.Equal(*got), "cutoff: want %s, got %s", want, *got)
}

// ADR-0027 D2: the cutoff is written by a targeted, monotonic UPDATE that is
// scoped to the account.
func TestSqlStore_SetUserCredentialsValidAfter(t *testing.T) {
	runTestForAllEngines(t, "../testdata/store.sql", func(t *testing.T, s Store) {
		ctx := context.Background()
		require.Nil(t, getUser(t, s, cutoffTestUserID).CredentialsValidAfter, "no cutoff by default")

		first := time.Date(2026, 10, 18, 12, 0, 5, 0, time.UTC)
		require.NoError(t, s.SetUserCredentialsValidAfter(ctx, localTestAccountID, cutoffTestUserID, first))
		requireCutoff(t, s, cutoffTestUserID, first)

		// Never moves backwards; an equal value is a no-op, not an error.
		require.NoError(t, s.SetUserCredentialsValidAfter(ctx, localTestAccountID, cutoffTestUserID, first.Add(-time.Hour)))
		require.NoError(t, s.SetUserCredentialsValidAfter(ctx, localTestAccountID, cutoffTestUserID, first))
		requireCutoff(t, s, cutoffTestUserID, first)

		later := first.Add(time.Minute)
		require.NoError(t, s.SetUserCredentialsValidAfter(ctx, localTestAccountID, cutoffTestUserID, later))
		requireCutoff(t, s, cutoffTestUserID, later)

		// Sub-second precision is dropped: JWT iat is whole seconds.
		require.NoError(t, s.SetUserCredentialsValidAfter(ctx, localTestAccountID, cutoffTestUserID,
			later.Add(time.Minute+700*time.Millisecond)))
		requireCutoff(t, s, cutoffTestUserID, later.Add(time.Minute))

		requireStatus(t, s.SetUserCredentialsValidAfter(ctx, localTestAccountID, "no-such-user", later), status.NotFound)
		requireStatus(t, s.SetUserCredentialsValidAfter(ctx, "other-account", cutoffTestUserID, later), status.NotFound)
	})
}

// ADR-0027 D2: a generic save built from a user read before the reset must
// not clobber the cutoff the reset wrote.
func TestSqlStore_GenericSavesKeepCredentialsCutoff(t *testing.T) {
	runTestForAllEngines(t, "../testdata/store.sql", func(t *testing.T, s Store) {
		ctx := context.Background()
		cutoff := time.Date(2026, 10, 18, 12, 0, 5, 0, time.UTC)

		staleUser := getUser(t, s, cutoffTestUserID)
		staleAccount, err := s.GetAccount(ctx, localTestAccountID)
		require.NoError(t, err)
		require.NoError(t, s.SetUserCredentialsValidAfter(ctx, localTestAccountID, cutoffTestUserID, cutoff))

		staleUser.Name = "renamed"
		require.NoError(t, s.SaveUser(ctx, LockingStrengthUpdate, staleUser))
		requireCutoff(t, s, cutoffTestUserID, cutoff)
		require.Equal(t, "renamed", getUser(t, s, cutoffTestUserID).Name, "the rest of the row is still saved")

		require.NoError(t, s.SaveUsers(ctx, LockingStrengthUpdate, []*types.User{staleUser}))
		requireCutoff(t, s, cutoffTestUserID, cutoff)

		require.NoError(t, s.SaveUserLastLogin(ctx, localTestAccountID, cutoffTestUserID, time.Now().UTC()))
		requireCutoff(t, s, cutoffTestUserID, cutoff)

		require.NoError(t, s.SaveAccount(ctx, staleAccount))
		requireCutoff(t, s, cutoffTestUserID, cutoff)

		// Not even a struct that carries an older value moves it back.
		older := cutoff.Add(-time.Hour)
		staleUser.CredentialsValidAfter = &older
		require.NoError(t, s.SaveUser(ctx, LockingStrengthUpdate, staleUser))
		require.NoError(t, s.SaveUsers(ctx, LockingStrengthUpdate, []*types.User{staleUser}))
		staleAccount.Users[cutoffTestUserID].CredentialsValidAfter = &older
		require.NoError(t, s.SaveAccount(ctx, staleAccount))
		requireCutoff(t, s, cutoffTestUserID, cutoff)

		// A brand-new user saved through a generic path starts without one.
		fresh := newLocalUser("fresh")
		fresh.SCIMUserName = ""
		fresh.CredentialsValidAfter = &cutoff
		require.NoError(t, s.SaveUser(ctx, LockingStrengthUpdate, fresh))
		require.Nil(t, getUser(t, s, "fresh").CredentialsValidAfter)
	})
}
