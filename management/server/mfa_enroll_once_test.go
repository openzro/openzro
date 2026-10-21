package server

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
)

func requireStatus(t *testing.T, err error, want status.Type) {
	t.Helper()
	require.Error(t, err)
	sErr, ok := status.FromError(err)
	require.True(t, ok, "expected a status error, got %v", err)
	assert.Equal(t, want, sErr.Type(), err.Error())
}

func storedMFA(t *testing.T, am *DefaultAccountManager, userID string) (secretEnc string, backups []string) {
	t.Helper()
	row, err := am.Store.GetUserMFA(context.Background(), store.LockingStrengthNone, userID)
	require.NoError(t, err)
	return row.TOTPSecretEnc, row.BackupCodesHash
}

// Initial enrollment creates the 2FA once. Finishing again with the same
// pending token and code, or starting over with any token once 2FA exists,
// is refused and leaves the stored factor and backup codes alone.
func TestMFAEnrollment_InitialOnlyOnce(t *testing.T) {
	am := newMFATestManager(t)
	clock := newMFATestClock()
	am.mfaClock = clock.Now
	ctx := context.Background()
	acc, err := am.GetOrCreateAccountByUser(ctx, "fran", "example.com")
	require.NoError(t, err)
	setUserEmail(t, am, "fran", "fran@example.com")

	start, err := am.MFAStartEnrollment(ctx, acc.Id, "fran")
	require.NoError(t, err)
	code := totpCodeAt(t, start.Secret, clock.Now())
	_, err = am.MFAFinishEnrollment(ctx, acc.Id, "fran", start.PendingToken, code, "jti-1")
	require.NoError(t, err)
	secret, backups := storedMFA(t, am, "fran")

	out, err := am.MFAFinishEnrollment(ctx, acc.Id, "fran", start.PendingToken, code, "jti-2")
	requireStatus(t, err, status.PreconditionFailed)
	assert.Nil(t, out, "no new backup codes or session token")

	_, err = am.MFAStartEnrollment(ctx, acc.Id, "fran")
	requireStatus(t, err, status.PreconditionFailed)

	gotSecret, gotBackups := storedMFA(t, am, "fran")
	assert.Equal(t, secret, gotSecret)
	assert.Equal(t, backups, gotBackups)
}

// Re-enrollment, which the handler allows only with a verified 2FA
// session, replaces the factor with a new secret. The same pending secret
// cannot be finished twice.
func TestMFAEnrollment_ReEnrollmentReplacesOnce(t *testing.T) {
	am := newMFATestManager(t)
	clock := newMFATestClock()
	am.mfaClock = clock.Now
	ctx := context.Background()
	acc, err := am.GetOrCreateAccountByUser(ctx, "gil", "example.com")
	require.NoError(t, err)
	setUserEmail(t, am, "gil", "gil@example.com")
	enrollAt(t, am, clock, acc.Id, "gil")
	oldSecret, _ := storedMFA(t, am, "gil")

	start, err := am.MFAStartReEnrollment(ctx, acc.Id, "gil")
	require.NoError(t, err)
	code := totpCodeAt(t, start.Secret, clock.Now())
	_, err = am.MFAFinishReEnrollment(ctx, acc.Id, "gil", start.PendingToken, code, "jti-1")
	require.NoError(t, err)
	newSecret, newBackups := storedMFA(t, am, "gil")
	assert.NotEqual(t, oldSecret, newSecret, "re-enrollment replaces the secret")

	out, err := am.MFAFinishReEnrollment(ctx, acc.Id, "gil", start.PendingToken, code, "jti-2")
	requireStatus(t, err, status.PreconditionFailed)
	assert.Nil(t, out)
	gotSecret, gotBackups := storedMFA(t, am, "gil")
	assert.Equal(t, newSecret, gotSecret)
	assert.Equal(t, newBackups, gotBackups)

	// The re-enrollment code counts as used.
	outcome, err := am.MFAChallenge(ctx, "gil", code, "jti-3")
	require.NoError(t, err)
	assert.True(t, outcome.CodeReused)
}

// The same pending token and code finished on two replicas at once
// enroll once.
func TestMFAEnrollment_ConcurrentDuplicateFinish(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	encKey := base64.StdEncoding.EncodeToString(key)
	require.NoError(t, r.A.SetMFA(encKey))
	require.NoError(t, r.B.SetMFA(encKey))

	const accountID, userID = "mfa-enroll-account", "mfa-enroll-user"
	acc := newAccountWithId(ctx, accountID, userID, "mfa-enroll.example", false)
	acc.Users[userID].Email = "enroll@example.com"
	require.NoError(t, r.A.Store.SaveAccount(ctx, acc))

	start, err := r.A.MFAStartEnrollment(ctx, accountID, userID)
	require.NoError(t, err)
	code := totpCodeAt(t, start.Secret, time.Now())

	align := newBarrier()
	errs := make(chan error, 2)
	for _, am := range []*DefaultAccountManager{r.A, r.B} {
		go func(am *DefaultAccountManager) {
			align.wait(t)
			_, err := am.MFAFinishEnrollment(ctx, accountID, userID, start.PendingToken, code, "jti")
			errs <- err
		}(am)
	}
	succeeded := 0
	for range 2 {
		select {
		case err := <-errs:
			if err == nil {
				succeeded++
				continue
			}
			requireStatus(t, err, status.PreconditionFailed)
		case <-time.After(30 * time.Second):
			t.Fatal("a finish never returned")
		}
	}
	assert.Equal(t, 1, succeeded, "one pending enrollment, one success")
}
