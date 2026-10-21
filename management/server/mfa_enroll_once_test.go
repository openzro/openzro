package server

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/account"
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

// A re-enrollment token is bound to the factor it replaces. Once any
// enrollment replaces that factor, or a reset removes it, the token is
// refused, so an old token cannot restore an old secret.
func TestMFAEnrollment_ReEnrollmentTokenBoundToFactor(t *testing.T) {
	am := newMFATestManager(t)
	clock := newMFATestClock()
	am.mfaClock = clock.Now
	ctx := context.Background()
	acc, err := am.GetOrCreateAccountByUser(ctx, "hal", "example.com")
	require.NoError(t, err)
	setUserEmail(t, am, "hal", "hal@example.com")
	enrollAt(t, am, clock, acc.Id, "hal")

	reenroll := func(jti string) (*account.MFAStartEnrollmentResult, string) {
		t.Helper()
		start, err := am.MFAStartReEnrollment(ctx, acc.Id, "hal")
		require.NoError(t, err)
		return start, totpCodeAt(t, start.Secret, clock.Now())
	}

	startA, codeA := reenroll("a")
	_, err = am.MFAFinishReEnrollment(ctx, acc.Id, "hal", startA.PendingToken, codeA, "jti-a")
	require.NoError(t, err)
	clock.Advance(30 * time.Second)
	startB, codeB := reenroll("b")
	_, err = am.MFAFinishReEnrollment(ctx, acc.Id, "hal", startB.PendingToken, codeB, "jti-b")
	require.NoError(t, err)
	secretB, backupsB := storedMFA(t, am, "hal")

	// A's token and code again: refused, B stays.
	codeA = totpCodeAt(t, startA.Secret, clock.Now())
	out, err := am.MFAFinishReEnrollment(ctx, acc.Id, "hal", startA.PendingToken, codeA, "jti-a2")
	requireStatus(t, err, status.PreconditionFailed)
	assert.Nil(t, out)
	gotSecret, gotBackups := storedMFA(t, am, "hal")
	assert.Equal(t, secretB, gotSecret)
	assert.Equal(t, backupsB, gotBackups)

	// Two re-enrollments started from the same factor: only the first to
	// finish wins.
	clock.Advance(30 * time.Second)
	pendingC, codeC := reenroll("c")
	pendingD, codeD := reenroll("d")
	_, err = am.MFAFinishReEnrollment(ctx, acc.Id, "hal", pendingC.PendingToken, codeC, "jti-c")
	require.NoError(t, err)
	_, err = am.MFAFinishReEnrollment(ctx, acc.Id, "hal", pendingD.PendingToken, codeD, "jti-d")
	requireStatus(t, err, status.PreconditionFailed)

	// After a reset, a re-enrollment token from before it is refused, by
	// either finish: re-enrolling needs the factor it was started from, and
	// an initial enrollment needs an initial token.
	clock.Advance(30 * time.Second)
	startE, codeE := reenroll("e")
	require.NoError(t, am.MFADisenroll(ctx, acc.Id, "hal", "hal"))
	_, err = am.MFAFinishReEnrollment(ctx, acc.Id, "hal", startE.PendingToken, codeE, "jti-e")
	requireStatus(t, err, status.PreconditionFailed)
	_, err = am.MFAFinishEnrollment(ctx, acc.Id, "hal", startE.PendingToken, codeE, "jti-e2")
	requireStatus(t, err, status.PreconditionFailed)
	_, err = am.Store.GetUserMFA(ctx, store.LockingStrengthNone, "hal")
	require.ErrorIs(t, err, store.ErrUserMFANotFound)
}

// A factor enrolled before factor_id existed has an empty one. A
// re-enrollment token started from it must still not pass as an initial
// token: after a reset, neither finish accepts it.
func TestMFAEnrollment_LegacyFactorReEnrollmentAfterReset(t *testing.T) {
	am := newMFATestManager(t)
	clock := newMFATestClock()
	am.mfaClock = clock.Now
	ctx := context.Background()
	acc, err := am.GetOrCreateAccountByUser(ctx, "ivy", "example.com")
	require.NoError(t, err)
	setUserEmail(t, am, "ivy", "ivy@example.com")
	enrollAt(t, am, clock, acc.Id, "ivy")

	// Make it a pre-migration row.
	row, err := am.Store.GetUserMFA(ctx, store.LockingStrengthNone, "ivy")
	require.NoError(t, err)
	row.FactorID = ""
	require.NoError(t, am.Store.SaveUserMFA(ctx, store.LockingStrengthUpdate, row))

	pending, err := am.MFAStartReEnrollment(ctx, acc.Id, "ivy")
	require.NoError(t, err)
	code := totpCodeAt(t, pending.Secret, clock.Now())
	require.NoError(t, am.MFADisenroll(ctx, acc.Id, "ivy", "ivy"))

	_, err = am.MFAFinishEnrollment(ctx, acc.Id, "ivy", pending.PendingToken, code, "jti-1")
	requireStatus(t, err, status.PreconditionFailed)
	_, err = am.MFAFinishReEnrollment(ctx, acc.Id, "ivy", pending.PendingToken, code, "jti-2")
	requireStatus(t, err, status.PreconditionFailed)
	_, err = am.Store.GetUserMFA(ctx, store.LockingStrengthNone, "ivy")
	require.ErrorIs(t, err, store.ErrUserMFANotFound)
}

// Re-enrolling a pre-migration factor works once and gives it an ID.
func TestMFAEnrollment_LegacyFactorReEnrollsOnce(t *testing.T) {
	am := newMFATestManager(t)
	clock := newMFATestClock()
	am.mfaClock = clock.Now
	ctx := context.Background()
	acc, err := am.GetOrCreateAccountByUser(ctx, "jo", "example.com")
	require.NoError(t, err)
	setUserEmail(t, am, "jo", "jo@example.com")
	enrollAt(t, am, clock, acc.Id, "jo")
	row, err := am.Store.GetUserMFA(ctx, store.LockingStrengthNone, "jo")
	require.NoError(t, err)
	row.FactorID = ""
	require.NoError(t, am.Store.SaveUserMFA(ctx, store.LockingStrengthUpdate, row))

	pending, err := am.MFAStartReEnrollment(ctx, acc.Id, "jo")
	require.NoError(t, err)
	code := totpCodeAt(t, pending.Secret, clock.Now())
	_, err = am.MFAFinishReEnrollment(ctx, acc.Id, "jo", pending.PendingToken, code, "jti-1")
	require.NoError(t, err)
	row, err = am.Store.GetUserMFA(ctx, store.LockingStrengthNone, "jo")
	require.NoError(t, err)
	assert.NotEmpty(t, row.FactorID)

	_, err = am.MFAFinishReEnrollment(ctx, acc.Id, "jo", pending.PendingToken, code, "jti-2")
	requireStatus(t, err, status.PreconditionFailed)
}
