package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openzro/openzro/management/server/account"
	activityStore "github.com/openzro/openzro/management/server/activity/store"
	"github.com/openzro/openzro/management/server/mfa"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// SetMFA wires the MFA subsystem (issue #31) onto the account
// manager: derives the token-signing key from the operator's
// DataStoreEncryptionKey and builds the FieldEncrypt used to
// protect the TOTP shared secret at rest. Called from
// management/cmd once after BuildManager, mirroring the
// SetCoordinator pattern.
//
// Returns an error only when the encryption key is missing or
// malformed; in that case MFA endpoints stay disabled (the
// middleware gate fails closed because mfaSigner is nil), but
// the rest of the management daemon keeps working.
func (am *DefaultAccountManager) SetMFA(dataStoreEncryptionKeyB64 string) error {
	signer, err := mfa.NewSigner(dataStoreEncryptionKeyB64)
	if err != nil {
		return fmt.Errorf("mfa: signer: %w", err)
	}
	cipher, err := activityStore.NewFieldEncrypt(dataStoreEncryptionKeyB64)
	if err != nil {
		return fmt.Errorf("mfa: secret cipher: %w", err)
	}
	am.mfaSigner = signer
	am.mfaSecretCipher = cipher
	return nil
}

// MFAEnabled reports whether MFA subsystem initialization succeeded
// and the manager can mint / verify TOTP. The middleware uses this
// to fail closed when an operator turns enforcement ON but MFA
// initialization failed at boot (e.g. missing encryption key) —
// without the check, the gate would silently let users through.
func (am *DefaultAccountManager) MFAEnabled() bool {
	return am.mfaSigner != nil && am.mfaSecretCipher != nil
}

// MFAStatus returns the user's enrollment + lockout state for the
// profile page. Idempotent read. Enrolled=false when no MFA row.
func (am *DefaultAccountManager) MFAStatus(ctx context.Context, userID string) (*account.MFAStatus, error) {
	row, err := am.Store.GetUserMFA(ctx, store.LockingStrengthShare, userID)
	if errors.Is(err, store.ErrUserMFANotFound) {
		return &account.MFAStatus{Enrolled: false}, nil
	}
	if err != nil {
		return nil, err
	}
	st := &account.MFAStatus{
		Enrolled:             true,
		EnrolledAt:           &row.EnrolledAt,
		LastVerifiedAt:       row.LastVerifiedAt,
		BackupCodesRemaining: len(row.BackupCodesHash),
	}
	if row.LockedUntil != nil && row.LockedUntil.After(time.Now().UTC()) {
		st.Locked = true
		st.LockedUntil = row.LockedUntil
	}
	return st, nil
}

// MFAStartEnrollment provisions a fresh TOTP secret for `userID`
// and packs it into a 15min pending_enrollment_token. The secret is
// NOT persisted; it lives only in the token, which the dashboard
// echoes back to /enroll/finish. Stateless across daemon restarts
// AND across HA replicas.
func (am *DefaultAccountManager) MFAStartEnrollment(ctx context.Context, accountID, userID string) (*account.MFAStartEnrollmentResult, error) {
	if !am.MFAEnabled() {
		return nil, status.Errorf(status.Internal, "mfa: subsystem not initialized")
	}
	user, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthShare, userID)
	if err != nil {
		return nil, err
	}
	if user.AccountID != accountID {
		return nil, status.NewUserNotFoundError(userID)
	}
	dnsDomain := am.GetDNSDomain(nil) // settings ignored; dnsDomain is account-level
	if dnsDomain == "" {
		dnsDomain = "openzro"
	}
	key, err := mfa.NewTOTPKey(dnsDomain, user.Email)
	if err != nil {
		return nil, fmt.Errorf("mfa: generate key: %w", err)
	}
	pending, err := am.mfaSigner.IssuePending(userID, key.Secret())
	if err != nil {
		return nil, fmt.Errorf("mfa: issue pending token: %w", err)
	}
	return &account.MFAStartEnrollmentResult{
		OTPAuthURL:   key.URL(),
		Secret:       key.Secret(),
		PendingToken: pending,
	}, nil
}

// MFAFinishEnrollment verifies the pending_enrollment_token, runs
// the supplied 6-digit code through TOTP against the secret it
// carries, persists the user_mfa row (secret encrypted at rest),
// and mints an mfa_session_token bound to `jwtSessionID`. Returns
// the plaintext backup codes (only chance to see them) and the
// session token.
func (am *DefaultAccountManager) MFAFinishEnrollment(ctx context.Context, accountID, userID, pendingToken, code, jwtSessionID string) (*account.MFAFinishEnrollmentResult, error) {
	if !am.MFAEnabled() {
		return nil, status.Errorf(status.Internal, "mfa: subsystem not initialized")
	}
	if jwtSessionID == "" {
		return nil, status.Errorf(status.InvalidArgument, "mfa: missing session binding")
	}
	user, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthShare, userID)
	if err != nil {
		return nil, err
	}
	if user.AccountID != accountID {
		return nil, status.NewUserNotFoundError(userID)
	}
	pending, err := am.mfaSigner.Verify(pendingToken, mfa.PurposePendingEnrollment)
	if err != nil {
		return nil, status.Errorf(status.InvalidArgument, "mfa: pending token invalid — start again")
	}
	if pending.UserID != userID {
		return nil, status.Errorf(status.InvalidArgument, "mfa: pending token subject mismatch")
	}
	if pending.Secret == "" {
		return nil, status.Errorf(status.InvalidArgument, "mfa: pending token missing secret")
	}
	valid, err := mfa.ValidateTOTP(code, pending.Secret)
	if err != nil {
		return nil, fmt.Errorf("mfa: validate: %w", err)
	}
	if !valid {
		return nil, status.Errorf(status.InvalidArgument, "mfa: code does not match")
	}
	plaintext, hashes, err := mfa.GenerateBackupCodes()
	if err != nil {
		return nil, fmt.Errorf("mfa: backup codes: %w", err)
	}
	secretEnc, err := am.mfaSecretCipher.Encrypt(pending.Secret)
	if err != nil {
		return nil, fmt.Errorf("mfa: encrypt secret: %w", err)
	}
	now := time.Now().UTC()
	row := &types.UserMFA{
		UserID:          userID,
		Method:          types.MFAMethodTOTP,
		TOTPSecretEnc:   secretEnc,
		BackupCodesHash: hashes,
		EnrolledAt:      now,
		LastVerifiedAt:  &now,
	}
	if err := am.Store.SaveUserMFA(ctx, store.LockingStrengthUpdate, row); err != nil {
		return nil, err
	}
	sessionToken, err := am.mfaSigner.IssueSession(userID, jwtSessionID)
	if err != nil {
		return nil, fmt.Errorf("mfa: issue session token: %w", err)
	}
	return &account.MFAFinishEnrollmentResult{
		BackupCodes:  plaintext,
		SessionToken: sessionToken,
	}, nil
}

// MFAChallenge verifies a TOTP code OR a backup code against the
// stored user_mfa row. The read-validate-write triplet runs inside
// a single ExecuteInTransaction so two concurrent challenges can
// neither accept the same backup code twice nor lose a failed-
// attempt increment. On success, mints an mfa_session_token bound
// to `jwtSessionID` and attaches it to the outcome.
func (am *DefaultAccountManager) MFAChallenge(ctx context.Context, userID, code, jwtSessionID string) (*account.MFAChallengeOutcome, error) {
	if !am.MFAEnabled() {
		return nil, status.Errorf(status.Internal, "mfa: subsystem not initialized")
	}
	if jwtSessionID == "" {
		return nil, status.Errorf(status.InvalidArgument, "mfa: missing session binding")
	}

	var outcome *account.MFAChallengeOutcome
	txErr := am.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		row, err := tx.GetUserMFA(ctx, store.LockingStrengthUpdate, userID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if row.LockedUntil != nil && row.LockedUntil.After(now) {
			outcome = &account.MFAChallengeOutcome{Locked: true, LockedUntil: row.LockedUntil}
			return nil
		}

		// Try TOTP first. A successful TOTP doesn't burn a backup code.
		secret, err := am.mfaSecretCipher.Decrypt(row.TOTPSecretEnc)
		if err != nil {
			return fmt.Errorf("mfa: decrypt secret: %w", err)
		}
		totpOK, err := mfa.ValidateTOTP(code, secret)
		if err != nil {
			return fmt.Errorf("mfa: validate totp: %w", err)
		}

		usedBackup := false
		if !totpOK {
			// Backup code is consumed in the same transaction as the
			// SaveUserMFA below, so a replay of /challenge with the
			// same backup code from another goroutine is rejected.
			remaining, ok := mfa.ConsumeBackupCode(code, row.BackupCodesHash)
			if ok {
				row.BackupCodesHash = remaining
				usedBackup = true
			}
		}

		if !totpOK && !usedBackup {
			row.FailedAttempts++
			if row.FailedAttempts >= types.MFALockoutThreshold {
				lock := now.Add(types.MFALockoutDuration)
				row.LockedUntil = &lock
			}
			if err := tx.SaveUserMFA(ctx, store.LockingStrengthUpdate, row); err != nil {
				return err
			}
			outcome = &account.MFAChallengeOutcome{OK: false}
			if row.LockedUntil != nil && row.LockedUntil.After(now) {
				outcome.Locked = true
				outcome.LockedUntil = row.LockedUntil
			}
			return nil
		}

		// Success.
		row.FailedAttempts = 0
		row.LockedUntil = nil
		row.LastVerifiedAt = &now
		if err := tx.SaveUserMFA(ctx, store.LockingStrengthUpdate, row); err != nil {
			return err
		}
		outcome = &account.MFAChallengeOutcome{OK: true, UsedBackupCode: usedBackup}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	if outcome != nil && outcome.OK {
		tok, err := am.mfaSigner.IssueSession(userID, jwtSessionID)
		if err != nil {
			return nil, fmt.Errorf("mfa: issue session token: %w", err)
		}
		outcome.SessionToken = tok
	}
	return outcome, nil
}

// MFAVerifyToken validates a challenge_token / enrollment_token /
// pending_enrollment_token / mfa_session_token. Returns the
// subject + JTI binding + carried secret; empty + non-nil error on
// any failure path. The caller maps that to 401 with no detail leak.
func (am *DefaultAccountManager) MFAVerifyToken(raw string, purpose account.MFATokenPurpose) (*account.MFATokenVerifyResult, error) {
	if !am.MFAEnabled() {
		return nil, status.Errorf(status.Internal, "mfa: subsystem not initialized")
	}
	res, err := am.mfaSigner.Verify(raw, mfa.TokenPurpose(purpose))
	if err != nil {
		return nil, err
	}
	return &account.MFATokenVerifyResult{
		UserID:     res.UserID,
		JTIBinding: res.JTIBinding,
		Secret:     res.Secret,
	}, nil
}

// MFAIssueChallengeToken mints a fresh challenge_token bound to
// `jwtSessionID`. The handler-level sensitive-op gate calls this
// when it refuses a request missing an mfa_session_token so the
// dashboard's 403 interceptor can route the user to /mfa/challenge.
// Closes the dead-end where voluntary MFA users (enforcement OFF)
// couldn't step up to disable / regenerate after a tab reset wiped
// sessionStorage (review round 2, finding #2).
func (am *DefaultAccountManager) MFAIssueChallengeToken(userID, jwtSessionID string) (string, error) {
	if !am.MFAEnabled() {
		return "", status.Errorf(status.Internal, "mfa: subsystem not initialized")
	}
	if userID == "" || jwtSessionID == "" {
		return "", status.Errorf(status.InvalidArgument, "mfa: issue challenge token requires userID + jwtSessionID")
	}
	return am.mfaSigner.IssueRedirect(userID, jwtSessionID, mfa.PurposeChallenge)
}

// MFASessionValid reports whether `mfaSessionToken` is a valid
// mfa_session_token for `userID` AND bound to the current
// `jwtSessionID`. Used by handlers gating sensitive operations
// (disenroll, regenerate, re-enroll over existing TOTP) regardless
// of the operator's enforcement flag. Returns false on any error
// without leaking which check failed.
func (am *DefaultAccountManager) MFASessionValid(userID, mfaSessionToken, jwtSessionID string) bool {
	if !am.MFAEnabled() || userID == "" || mfaSessionToken == "" || jwtSessionID == "" {
		return false
	}
	res, err := am.mfaSigner.Verify(mfaSessionToken, mfa.PurposeSession)
	if err != nil {
		return false
	}
	if res.UserID != userID {
		return false
	}
	if res.JTIBinding != jwtSessionID {
		return false
	}
	return true
}

// MFADisenroll removes the user's MFA row. Used by:
//   - admin override: an operator clears a locked-out user
//   - user reset: the user voluntarily disables 2FA from the profile
//     (the handler enforces an MFA-verified session before reaching
//     here, so a stolen JWT alone can't strip the second factor)
//
// Idempotent: deleting a non-enrolled user is a no-op (no error).
func (am *DefaultAccountManager) MFADisenroll(ctx context.Context, userID string) error {
	return am.Store.DeleteUserMFA(ctx, store.LockingStrengthUpdate, userID)
}

// MFARegenerateBackupCodes mints a fresh batch (invalidates all
// previous codes) and returns the plaintext list for one-time
// display. Idempotent against multiple regenerations — each call
// fully replaces the array.
func (am *DefaultAccountManager) MFARegenerateBackupCodes(ctx context.Context, userID string) ([]string, error) {
	if !am.MFAEnabled() {
		return nil, status.Errorf(status.Internal, "mfa: subsystem not initialized")
	}
	row, err := am.Store.GetUserMFA(ctx, store.LockingStrengthUpdate, userID)
	if err != nil {
		return nil, err
	}
	plaintext, hashes, err := mfa.GenerateBackupCodes()
	if err != nil {
		return nil, fmt.Errorf("mfa: backup codes: %w", err)
	}
	row.BackupCodesHash = hashes
	if err := am.Store.SaveUserMFA(ctx, store.LockingStrengthUpdate, row); err != nil {
		return nil, err
	}
	return plaintext, nil
}

// MFAGateForRequest decides what an inbound JWT-authenticated
// request should do, given the account-level enforcement toggles
// and the user's enrollment state. The middleware calls this after
// JWT verification + user lookup; on Pass==true the request
// continues, on Challenge/Enroll==true the middleware returns 403
// + the token in the response body.
//
// Per-session model: when the user is enrolled, the gate now
// requires an `mfa_session_token` (X-MFA-Token header) bound to the
// current JWT session — replacing the per-user
// `user_mfa.last_verified_at` shared across devices. Verifying on
// device A no longer silently elevates device B.
//
// Service users and PAT-authenticated requests bypass MFA — they're
// non-interactive auth that has its own per-token rotation policy.
func (am *DefaultAccountManager) MFAGateForRequest(
	ctx context.Context,
	connectorID string,
	user *types.User,
	settings *types.Settings,
	isPAT bool,
	jwtSessionID, mfaSessionToken string,
) (*account.MFAGateDecision, error) {
	if user == nil {
		return nil, errors.New("mfa: nil user")
	}
	// PATs, service users: pass through.
	if isPAT || user.IsServiceUser {
		return &account.MFAGateDecision{Pass: true}, nil
	}
	// If MFA subsystem failed to initialize, fail CLOSED for any
	// enforcement-on account so the operator's policy intent is
	// honored. Accounts with enforcement OFF still pass — they
	// never wanted MFA in the first place.
	enforce := am.resolveMFAEnforcement(connectorID, settings)
	if !enforce {
		return &account.MFAGateDecision{Pass: true}, nil
	}
	if !am.MFAEnabled() {
		return nil, status.Errorf(status.Internal, "mfa: enforcement on but subsystem disabled (check DataStoreEncryptionKey)")
	}
	if jwtSessionID == "" {
		// Operator enforced MFA but the middleware couldn't compute a
		// per-session id (this means the JWT chain is broken or the
		// middleware was wired without binding support). Fail closed
		// rather than silently degrade to user-wide verification.
		return nil, status.Errorf(status.Internal, "mfa: missing JWT session id")
	}

	_, err := am.Store.GetUserMFA(ctx, store.LockingStrengthShare, user.Id)
	if errors.Is(err, store.ErrUserMFANotFound) {
		// Enforced + not enrolled → forced enrollment redirect.
		token, terr := am.mfaSigner.IssueRedirect(user.Id, jwtSessionID, mfa.PurposeEnrollment)
		if terr != nil {
			return nil, fmt.Errorf("mfa: issue enrollment token: %w", terr)
		}
		return &account.MFAGateDecision{Enroll: true, Token: token}, nil
	}
	if err != nil {
		return nil, err
	}

	// Enrolled — does the current session carry a valid mfa_session_token
	// bound to this JWT?
	if am.MFASessionValid(user.Id, mfaSessionToken, jwtSessionID) {
		return &account.MFAGateDecision{Pass: true}, nil
	}
	token, err := am.mfaSigner.IssueRedirect(user.Id, jwtSessionID, mfa.PurposeChallenge)
	if err != nil {
		return nil, fmt.Errorf("mfa: issue challenge token: %w", err)
	}
	return &account.MFAGateDecision{Challenge: true, Token: token}, nil
}

// resolveMFAEnforcement maps (connector_id, settings) to a yes/no
// enforcement decision. connector_id == "local" follows the
// MFAEnforceLocal toggle; anything else (federated providers)
// follows MFAEnforceFederated.
func (am *DefaultAccountManager) resolveMFAEnforcement(connectorID string, settings *types.Settings) bool {
	if settings == nil {
		return false
	}
	if connectorID == "local" {
		return settings.MFAEnforceLocal
	}
	return settings.MFAEnforceFederated
}
