package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/auth/jwt"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// Users the 2FA gate applies to are counted under the connector the gate
// would see in their Dex subject. Users who cannot log in are left out.
func TestCountMFAEnrollmentGaps(t *testing.T) {
	local := func(id string) string { return jwt.EncodeDexSubject(id, "local") }
	google := func(id string) string { return jwt.EncodeDexSubject(id, "google") }

	tests := []struct {
		name string
		user *types.User
		has  bool
		want account.MFAEnrollmentGaps
	}{
		{"local without 2FA", &types.User{Id: local("a")}, false, account.MFAEnrollmentGaps{Local: 1}},
		{"local with 2FA", &types.User{Id: local("b")}, true, account.MFAEnrollmentGaps{}},
		{"federated without 2FA", &types.User{Id: google("c")}, false, account.MFAEnrollmentGaps{Federated: 1}},
		{"non-Dex subject is federated", &types.User{Id: "auth0|d"}, false, account.MFAEnrollmentGaps{Federated: 1}},
		{"malformed subject is federated", &types.User{Id: "!!not-base64"}, false, account.MFAEnrollmentGaps{Federated: 1}},
		{"service user", &types.User{Id: local("e"), IsServiceUser: true}, false, account.MFAEnrollmentGaps{}},
		{"blocked", &types.User{Id: local("f"), Blocked: true}, false, account.MFAEnrollmentGaps{}},
		{"local pending", &types.User{Id: local("g"), LocalCredentialState: types.LocalCredentialPending}, false, account.MFAEnrollmentGaps{}},
		{"local deactivated", &types.User{Id: local("h"), LocalCredentialState: types.LocalCredentialDeactivated}, false, account.MFAEnrollmentGaps{}},
		{"local tombstoned", &types.User{Id: local("i"), LocalCredentialState: types.LocalCredentialTombstoned}, false, account.MFAEnrollmentGaps{}},
		{"local active", &types.User{Id: local("j"), LocalCredentialState: types.LocalCredentialActive}, false, account.MFAEnrollmentGaps{Local: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			enrolled := map[string]struct{}{}
			if tc.has {
				enrolled[tc.user.Id] = struct{}{}
			}
			assert.Equal(t, tc.want, countMFAEnrollmentGaps([]*types.User{tc.user}, enrolled))
		})
	}
}

func seedMFAUser(t *testing.T, am *DefaultAccountManager, accountID, id string, role types.UserRole) {
	t.Helper()
	require.NoError(t, am.Store.SaveUser(context.Background(), store.LockingStrengthUpdate, &types.User{
		Id: id, AccountID: accountID, Role: role, Issued: types.UserIssuedAPI,
	}))
}

func seedMFARow(t *testing.T, am *DefaultAccountManager, userID string, lockedUntil *time.Time) {
	t.Helper()
	require.NoError(t, am.Store.SaveUserMFA(context.Background(), store.LockingStrengthUpdate, &types.UserMFA{
		UserID: userID, Method: types.MFAMethodTOTP, TOTPSecretEnc: "enc", EnrolledAt: time.Now().UTC(),
		LockedUntil: lockedUntil,
	}))
}

// The admin sees each user's 2FA state; a lockout that already ended is
// not reported. Regular users cannot read it.
func TestGetAccountUsersMFAState(t *testing.T) {
	am := newMFATestManager(t)
	ctx := context.Background()
	acc, err := am.GetOrCreateAccountByUser(ctx, "owner", "example.com")
	require.NoError(t, err)

	seedMFAUser(t, am, acc.Id, "enrolled", types.UserRoleUser)
	seedMFAUser(t, am, acc.Id, "locked", types.UserRoleUser)
	seedMFAUser(t, am, acc.Id, "lock-ended", types.UserRoleUser)
	seedMFAUser(t, am, acc.Id, "none", types.UserRoleUser)
	future, past := time.Now().Add(10*time.Minute).UTC(), time.Now().Add(-time.Minute).UTC()
	seedMFARow(t, am, "enrolled", nil)
	seedMFARow(t, am, "locked", &future)
	seedMFARow(t, am, "lock-ended", &past)

	states, err := am.GetAccountUsersMFAState(ctx, acc.Id, "owner")
	require.NoError(t, err)
	assert.Equal(t, account.UserMFAState{Enrolled: true}, states["enrolled"])
	require.True(t, states["locked"].Enrolled)
	require.NotNil(t, states["locked"].LockedUntil)
	assert.WithinDuration(t, future, *states["locked"].LockedUntil, time.Second)
	assert.Equal(t, account.UserMFAState{Enrolled: true}, states["lock-ended"])
	assert.NotContains(t, states, "none")

	_, err = am.GetAccountUsersMFAState(ctx, acc.Id, "none")
	sErr, ok := status.FromError(err)
	require.True(t, ok, "expected a status error, got %v", err)
	assert.Equal(t, status.PermissionDenied, sErr.Type())
}

// The counter covers the account's users with the shared filter, and only
// admins can read it.
func TestGetMFAEnrollmentGaps(t *testing.T) {
	am := newMFATestManager(t)
	ctx := context.Background()
	ownerID := jwt.EncodeDexSubject("owner", "local")
	acc, err := am.GetOrCreateAccountByUser(ctx, ownerID, "example.com")
	require.NoError(t, err)

	seedMFAUser(t, am, acc.Id, jwt.EncodeDexSubject("third-party", "local"), types.UserRoleUser)
	seedMFAUser(t, am, acc.Id, jwt.EncodeDexSubject("staff", "google"), types.UserRoleUser)
	seedMFARow(t, am, ownerID, nil)

	gaps, err := am.GetMFAEnrollmentGaps(ctx, acc.Id, ownerID)
	require.NoError(t, err)
	assert.Equal(t, account.MFAEnrollmentGaps{Local: 1, Federated: 1}, *gaps)

	_, err = am.GetMFAEnrollmentGaps(ctx, acc.Id, jwt.EncodeDexSubject("third-party", "local"))
	sErr, ok := status.FromError(err)
	require.True(t, ok, "expected a status error, got %v", err)
	assert.Equal(t, status.PermissionDenied, sErr.Type())
}

func mfaEvents(t *testing.T, am *DefaultAccountManager, accountID string, code activity.Activity) []*activity.Event {
	t.Helper()
	events, err := am.eventStore.Get(context.Background(), accountID, 0, 200, false)
	require.NoError(t, err)
	var out []*activity.Event
	for _, e := range events {
		if e.Activity == code {
			out = append(out, e)
		}
	}
	return out
}

// A reset by an admin and a user turning off their own 2FA are audited
// with the account, the initiator and the target. Removing 2FA from a user
// who has none changes nothing and is not audited.
func TestMFADisenroll_Audit(t *testing.T) {
	am := newMFATestManager(t)
	ctx := context.Background()
	acc, err := am.GetOrCreateAccountByUser(ctx, "admin", "example.com")
	require.NoError(t, err)
	seedMFAUser(t, am, acc.Id, "member", types.UserRoleUser)

	seedMFARow(t, am, "member", nil)
	require.NoError(t, am.MFADisenroll(ctx, acc.Id, "admin", "member"))
	requireEventually(t, am, acc.Id, activity.UserMFAReset)
	resets := mfaEvents(t, am, acc.Id, activity.UserMFAReset)
	require.Len(t, resets, 1)
	assert.Equal(t, "admin", resets[0].InitiatorID)
	assert.Equal(t, "member", resets[0].TargetID)
	assert.Equal(t, acc.Id, resets[0].AccountID)

	require.NoError(t, am.MFADisenroll(ctx, acc.Id, "admin", "member"), "removing absent 2FA is a no-op")
	assert.Never(t, func() bool { return len(mfaEvents(t, am, acc.Id, activity.UserMFAReset)) != 1 },
		300*time.Millisecond, 20*time.Millisecond, "a no-op is not audited")

	seedMFARow(t, am, "member", nil)
	require.NoError(t, am.MFADisenroll(ctx, acc.Id, "member", "member"))
	requireEventually(t, am, acc.Id, activity.UserMFADisabled)
	disabled := mfaEvents(t, am, acc.Id, activity.UserMFADisabled)
	require.Len(t, disabled, 1)
	assert.Equal(t, "member", disabled[0].InitiatorID)
	assert.Equal(t, "member", disabled[0].TargetID)

	st, err := am.MFAStatus(ctx, "member")
	require.NoError(t, err)
	assert.False(t, st.Enrolled)
}
