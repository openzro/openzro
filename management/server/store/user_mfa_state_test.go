package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/types"
)

// The account-wide 2FA read backs the admin's users list. It returns only
// the account's users, and never the secret or the backup code hashes.
func TestSqlStore_GetAccountUserMFAs(t *testing.T) {
	runTestForAllEngines(t, "../testdata/store.sql", func(t *testing.T, s Store) {
		ctx := context.Background()
		const accountID = "bf1c8084-ba50-4ce7-9439-34653001fc3b"
		enrolled := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
		locked := enrolled.Add(15 * time.Minute)

		require.NoError(t, s.SaveUserMFA(ctx, LockingStrengthUpdate, &types.UserMFA{
			UserID: "a23efe53-63fb-11ec-90d6-0242ac120003", Method: types.MFAMethodTOTP,
			TOTPSecretEnc: "secret-a", BackupCodesHash: []string{"hash-a"}, EnrolledAt: enrolled,
		}))
		require.NoError(t, s.SaveUserMFA(ctx, LockingStrengthUpdate, &types.UserMFA{
			UserID: "edafee4e-63fb-11ec-90d6-0242ac120003", Method: types.MFAMethodTOTP,
			TOTPSecretEnc: "secret-b", EnrolledAt: enrolled, FailedAttempts: types.MFALockoutThreshold,
			LockedUntil: &locked,
		}))
		require.NoError(t, s.SaveUserMFA(ctx, LockingStrengthUpdate, &types.UserMFA{
			UserID: "user-of-another-account", Method: types.MFAMethodTOTP,
			TOTPSecretEnc: "secret-c", EnrolledAt: enrolled,
		}))

		rows, err := s.GetAccountUserMFAs(ctx, LockingStrengthNone, accountID)
		require.NoError(t, err)
		require.Len(t, rows, 2)
		byUser := make(map[string]*types.UserMFA, len(rows))
		for _, r := range rows {
			require.Empty(t, r.TOTPSecretEnc, "the secret must not be read")
			require.Empty(t, r.BackupCodesHash, "backup code hashes must not be read")
			byUser[r.UserID] = r
		}
		require.Contains(t, byUser, "a23efe53-63fb-11ec-90d6-0242ac120003")
		require.Nil(t, byUser["a23efe53-63fb-11ec-90d6-0242ac120003"].LockedUntil)
		b := byUser["edafee4e-63fb-11ec-90d6-0242ac120003"]
		require.NotNil(t, b)
		require.NotNil(t, b.LockedUntil)
		require.True(t, locked.Equal(b.LockedUntil.UTC()))
		require.True(t, enrolled.Equal(b.EnrolledAt.UTC()))

		rows, err = s.GetAccountUserMFAs(ctx, LockingStrengthNone, "no-such-account")
		require.NoError(t, err)
		require.Empty(t, rows)
	})
}
