package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// TestMFAGateForRequest pins the gate-decision matrix (openZro #31):
// the cartesian product of {connector_id × enforcement flag ×
// enrolled-or-not × PAT/service-user} must produce the documented
// verdict. Without this matrix in tests, a careless edit to the
// resolveMFAEnforcement helper or the IsServiceUser bypass could
// silently flip a security default.
func TestMFAGateForRequest(t *testing.T) {
	am := newMFATestManager(t)
	ctx := context.Background()

	// Shared fixtures used across rows.
	user := &types.User{Id: "user-1", AccountID: "acct-1"}
	patUser := &types.User{Id: "user-pat", AccountID: "acct-1"}
	svcUser := &types.User{Id: "user-svc", AccountID: "acct-1", IsServiceUser: true}

	type want struct{ pass, challenge, enroll bool }

	cases := []struct {
		name        string
		connectorID string
		settings    *types.Settings
		user        *types.User
		isPAT       bool
		want        want
	}{
		// connector_id="local" matrix.
		{
			name:        "local + enforce OFF + not enrolled -> Pass",
			connectorID: "local",
			settings:    &types.Settings{MFAEnforceLocal: false},
			user:        user,
			want:        want{pass: true},
		},
		{
			name:        "local + enforce ON + not enrolled -> Enroll",
			connectorID: "local",
			settings:    &types.Settings{MFAEnforceLocal: true},
			user:        user,
			want:        want{enroll: true},
		},
		// Federated matrix.
		{
			name:        "federated + enforce OFF -> Pass",
			connectorID: "okta-oidc",
			settings:    &types.Settings{MFAEnforceFederated: false},
			user:        user,
			want:        want{pass: true},
		},
		{
			name:        "federated + enforce ON + not enrolled -> Enroll",
			connectorID: "okta-oidc",
			settings:    &types.Settings{MFAEnforceFederated: true},
			user:        user,
			want:        want{enroll: true},
		},
		// Crosswise: MFAEnforceLocal does NOT trigger on federated.
		{
			name:        "federated + ONLY MFAEnforceLocal=true -> Pass",
			connectorID: "okta-oidc",
			settings:    &types.Settings{MFAEnforceLocal: true},
			user:        user,
			want:        want{pass: true},
		},
		// PAT bypass — even with both flags on.
		{
			name:        "PAT user + both flags on -> Pass (PAT bypass)",
			connectorID: "local",
			settings:    &types.Settings{MFAEnforceLocal: true, MFAEnforceFederated: true},
			user:        patUser,
			isPAT:       true,
			want:        want{pass: true},
		},
		// Service-user bypass.
		{
			name:        "IsServiceUser + enforce ON -> Pass",
			connectorID: "local",
			settings:    &types.Settings{MFAEnforceLocal: true},
			user:        svcUser,
			want:        want{pass: true},
		},
		// Nil settings: defensive default = Pass (no enforcement
		// flag set means operator hasn't opted in).
		{
			name:        "nil settings -> Pass",
			connectorID: "local",
			settings:    nil,
			user:        user,
			want:        want{pass: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := am.MFAGateForRequest(ctx, tc.connectorID, tc.user, tc.settings, tc.isPAT, "jti-test", "")
			require.NoError(t, err)
			require.NotNil(t, d)
			require.Equal(t, tc.want.pass, d.Pass)
			require.Equal(t, tc.want.challenge, d.Challenge)
			require.Equal(t, tc.want.enroll, d.Enroll)
			if d.Challenge || d.Enroll {
				require.NotEmpty(t, d.Token, "non-pass verdicts must carry a token")
			}
		})
	}
}

// TestMFAGateForRequest_FailsClosedWithoutSubsystem pins the safety
// net: an operator turns enforcement ON but the encryption key is
// missing at boot — MFAEnabled is false. The gate MUST refuse
// rather than silently passing the request and emitting a
// MFA-required token that no one can verify.
func TestMFAGateForRequest_FailsClosedWithoutSubsystem(t *testing.T) {
	store, err := createStore(t)
	require.NoError(t, err)
	am := &DefaultAccountManager{Store: store}
	// Note: SetMFA NOT called → mfaSigner/mfaSecretCipher both nil
	// → MFAEnabled() returns false.

	d, err := am.MFAGateForRequest(context.Background(), "local",
		&types.User{Id: "u-1", AccountID: "a-1"},
		&types.Settings{MFAEnforceLocal: true},
		false,
		"jti-test", "",
	)
	require.Error(t, err, "MFA OFF + enforcement ON must error (fail-closed)")
	require.Nil(t, d)
}

// TestMFAGateForRequest_RequiresSessionTokenForEnrolled pins the
// per-session contract: even on an enforced account where the user
// IS enrolled, the gate returns Challenge unless a valid
// mfa_session_token bound to the current JWT session is presented.
// Mitigates the review's high/sec finding #3 (user-wide
// last_verified_at silently elevating a separate stolen session).
func TestMFAGateForRequest_RequiresSessionTokenForEnrolled(t *testing.T) {
	am := newMFATestManager(t)
	ctx := context.Background()

	// Set up an enrolled user.
	acc, err := am.GetOrCreateAccountByUser(ctx, "carol", "example.com")
	require.NoError(t, err)
	userID := "carol"
	setUserEmail(t, am, userID, "carol@example.com")
	start, err := am.MFAStartEnrollment(ctx, acc.Id, userID)
	require.NoError(t, err)
	_, err = am.MFAFinishEnrollment(ctx, acc.Id, userID, start.PendingToken, mustTOTPCode(t, start.Secret), "jti-enroll")
	require.NoError(t, err)

	user, err := am.GetUserByID(ctx, userID)
	require.NoError(t, err)
	settings := &types.Settings{MFAEnforceLocal: true}

	t.Run("no session token -> Challenge", func(t *testing.T) {
		d, err := am.MFAGateForRequest(ctx, "local", user, settings, false, "jti-A", "")
		require.NoError(t, err)
		require.True(t, d.Challenge, "missing session token must trigger challenge")
		require.NotEmpty(t, d.Token)
	})

	t.Run("valid session token bound to same jti -> Pass", func(t *testing.T) {
		outcome, err := am.MFAChallenge(ctx, userID, mustTOTPCode(t, start.Secret), "jti-B")
		require.NoError(t, err)
		require.True(t, outcome.OK)
		require.NotEmpty(t, outcome.SessionToken)
		d, err := am.MFAGateForRequest(ctx, "local", user, settings, false, "jti-B", outcome.SessionToken)
		require.NoError(t, err)
		require.True(t, d.Pass, "session token bound to same jti must pass")
	})

	t.Run("session token bound to different jti -> Challenge", func(t *testing.T) {
		outcome, err := am.MFAChallenge(ctx, userID, mustTOTPCode(t, start.Secret), "jti-X")
		require.NoError(t, err)
		require.True(t, outcome.OK)
		require.NotEmpty(t, outcome.SessionToken)
		// Replay the token bound to jti-X against a request from jti-Y.
		d, err := am.MFAGateForRequest(ctx, "local", user, settings, false, "jti-Y", outcome.SessionToken)
		require.NoError(t, err)
		require.True(t, d.Challenge, "session token bound to other jti must NOT elevate this jti")
	})
}

// newMFATestManager wraps the standard createManager with SetMFA
// so the gate methods have a working signer + cipher + pending
// cache to exercise.
func newMFATestManager(t *testing.T) *DefaultAccountManager {
	t.Helper()
	am, err := createManager(t)
	require.NoError(t, err)
	key := make([]byte, 32)
	_, err = rand.Read(key)
	require.NoError(t, err)
	require.NoError(t, am.SetMFA(base64.StdEncoding.EncodeToString(key)))
	return am
}

// TestMFAEnrollmentFlow pins the full happy-path: Start →
// authenticator code → Finish → backup codes returned →
// Challenge with TOTP succeeds → Challenge with consumed backup
// code fails on the second use.
func TestMFAEnrollmentFlow(t *testing.T) {
	am := newMFATestManager(t)
	ctx := context.Background()

	// Use createAccount helper to get a real user with an email.
	acc, err := am.GetOrCreateAccountByUser(ctx, "alice", "example.com")
	require.NoError(t, err)
	userID := "alice"
	// Set user email so MFAStartEnrollment's label builder accepts it.
	setUserEmail(t, am, userID, "alice@example.com")

	// Start enrollment.
	start, err := am.MFAStartEnrollment(ctx, acc.Id, userID)
	require.NoError(t, err)
	require.NotEmpty(t, start.Secret)
	require.NotEmpty(t, start.OTPAuthURL)
	require.NotEmpty(t, start.PendingToken)

	// Compute the current TOTP code from the secret.
	code := mustTOTPCode(t, start.Secret)

	// Finish enrollment with the code + pending token.
	finish, err := am.MFAFinishEnrollment(ctx, acc.Id, userID, start.PendingToken, code, "jti-enroll")
	require.NoError(t, err)
	require.Len(t, finish.BackupCodes, 10, "10 backup codes minted at enrollment")
	require.NotEmpty(t, finish.SessionToken, "enroll-finish must mint an mfa_session_token")
	backups := finish.BackupCodes

	// Status now reports enrolled.
	st, err := am.MFAStatus(ctx, userID)
	require.NoError(t, err)
	require.True(t, st.Enrolled)
	require.Equal(t, 10, st.BackupCodesRemaining)

	// Challenge with a fresh TOTP succeeds.
	code = mustTOTPCode(t, start.Secret)
	outcome, err := am.MFAChallenge(ctx, userID, code, "jti-1")
	require.NoError(t, err)
	require.True(t, outcome.OK)
	require.False(t, outcome.UsedBackupCode)
	require.NotEmpty(t, outcome.SessionToken)

	// Challenge with the first backup code succeeds + marks single-use.
	outcome, err = am.MFAChallenge(ctx, userID, backups[0], "jti-2")
	require.NoError(t, err)
	require.True(t, outcome.OK)
	require.True(t, outcome.UsedBackupCode)

	// Same backup code rejected on the second attempt.
	outcome, err = am.MFAChallenge(ctx, userID, backups[0], "jti-3")
	require.NoError(t, err)
	require.False(t, outcome.OK, "backup code is single-use")
}

// TestMFADisenroll pins the disenrollment path: after Disenroll,
// Status reports not enrolled. The DELETE endpoint is idempotent
// (calling Disenroll on a non-enrolled user is a no-op).
func TestMFADisenroll(t *testing.T) {
	am := newMFATestManager(t)
	ctx := context.Background()
	acc, err := am.GetOrCreateAccountByUser(ctx, "bob", "example.com")
	require.NoError(t, err)
	userID := "bob"
	setUserEmail(t, am, userID, "bob@example.com")

	// Enroll first.
	start, err := am.MFAStartEnrollment(ctx, acc.Id, userID)
	require.NoError(t, err)
	_, err = am.MFAFinishEnrollment(ctx, acc.Id, userID, start.PendingToken, mustTOTPCode(t, start.Secret), "jti-enroll")
	require.NoError(t, err)

	// Disenroll + verify status.
	require.NoError(t, am.MFADisenroll(ctx, userID))
	st, err := am.MFAStatus(ctx, userID)
	require.NoError(t, err)
	require.False(t, st.Enrolled)

	// Idempotent: second Disenroll is a no-op.
	require.NoError(t, am.MFADisenroll(ctx, userID))
}

// TestMFAVerifyToken pins the token round-trip + the cross-purpose
// rejection that the /api/mfa/* handlers depend on.
func TestMFAVerifyToken(t *testing.T) {
	am := newMFATestManager(t)

	tok, err := am.mfaSigner.IssueRedirect("user-x", "jti-A", "mfa_challenge")
	require.NoError(t, err)

	res, err := am.MFAVerifyToken(tok, account.MFATokenPurposeChallenge)
	require.NoError(t, err)
	require.Equal(t, "user-x", res.UserID)
	require.Equal(t, "jti-A", res.JTIBinding)

	_, err = am.MFAVerifyToken(tok, account.MFATokenPurposeEnrollment)
	require.Error(t, err, "cross-purpose must fail")
}

// setUserEmail seeds User.Email so MFAStartEnrollment's label
// builder accepts the request. The test-only GetOrCreateAccountByUser
// path doesn't populate the email automatically; in production the
// JWT extractor + cacheUserClaimsFromAuth do.
func setUserEmail(t *testing.T, am *DefaultAccountManager, userID, email string) {
	t.Helper()
	ctx := context.Background()
	user, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthShare, userID)
	require.NoError(t, err)
	user.Email = email
	require.NoError(t, am.Store.SaveUser(ctx, store.LockingStrengthUpdate, user))
}

// mustTOTPCode is a thin test helper around the underlying TOTP
// library — the tests need it to derive the current-bucket code
// from the secret returned by MFAStartEnrollment.
func mustTOTPCode(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, time.Now().UTC())
	require.NoError(t, err)
	return code
}
