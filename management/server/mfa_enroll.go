package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/xid"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/mfa"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// errMFAAlreadyEnrolled refuses an initial enrollment for a user who
// already has 2FA. Replacing it is a re-enrollment, which needs a verified
// 2FA session.
var errMFAAlreadyEnrolled = status.Errorf(status.PreconditionFailed,
	"two-factor authentication is already set up for this account")

// errMFAEnrollmentStale refuses a re-enrollment whose token was started
// from a factor that has since been replaced or removed, including by the
// same re-enrollment finishing before.
var errMFAEnrollmentStale = status.Errorf(status.PreconditionFailed,
	"this enrollment is no longer valid; start again")

// MFAStartEnrollment begins an initial enrollment: it provisions a fresh
// TOTP secret and packs it into a pending_enrollment_token. The secret is
// not persisted; it lives only in the token, which the dashboard echoes
// back to finish. Refused when the user already has 2FA. MFAFinishEnrollment
// enforces the same rule under a lock, so this check only saves a round
// trip.
func (am *DefaultAccountManager) MFAStartEnrollment(ctx context.Context, accountID, userID string) (*account.MFAStartEnrollmentResult, error) {
	if !am.MFAEnabled() {
		return nil, status.Errorf(status.Internal, "mfa: subsystem not initialized")
	}
	if _, err := am.Store.GetUserMFA(ctx, store.LockingStrengthNone, userID); err == nil {
		return nil, errMFAAlreadyEnrolled
	} else if !errors.Is(err, store.ErrUserMFANotFound) {
		return nil, err
	}
	return am.startEnrollment(ctx, accountID, userID, "")
}

// MFAStartReEnrollment begins an enrollment that may replace the user's
// current 2FA. The caller must have verified the user's 2FA session. The
// token is bound to the current factor (see MFAFinishReEnrollment).
func (am *DefaultAccountManager) MFAStartReEnrollment(ctx context.Context, accountID, userID string) (*account.MFAStartEnrollmentResult, error) {
	if !am.MFAEnabled() {
		return nil, status.Errorf(status.Internal, "mfa: subsystem not initialized")
	}
	row, err := am.Store.GetUserMFA(ctx, store.LockingStrengthNone, userID)
	if errors.Is(err, store.ErrUserMFANotFound) {
		return nil, errMFAEnrollmentStale
	}
	if err != nil {
		return nil, err
	}
	return am.startEnrollment(ctx, accountID, userID, row.FactorID)
}

func (am *DefaultAccountManager) startEnrollment(ctx context.Context, accountID, userID, replacesFactor string) (*account.MFAStartEnrollmentResult, error) {
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
	pending, err := am.mfaSigner.IssuePending(userID, key.Secret(), replacesFactor)
	if err != nil {
		return nil, fmt.Errorf("mfa: issue pending token: %w", err)
	}
	return &account.MFAStartEnrollmentResult{
		OTPAuthURL:   key.URL(),
		Secret:       key.Secret(),
		PendingToken: pending,
	}, nil
}

// MFAFinishEnrollment completes an initial enrollment: it checks the code
// against the secret in the pending token and creates the user_mfa row,
// with the secret encrypted at rest. It succeeds once: while the user row
// is locked, an existing user_mfa row refuses it, so a repeated or
// concurrent finish, or a stale enrollment token, cannot replace the
// factor or mint new backup codes. Returns the backup codes (the only
// chance to see them) and an mfa_session_token bound to jwtSessionID.
func (am *DefaultAccountManager) MFAFinishEnrollment(ctx context.Context, accountID, userID, pendingToken, code, jwtSessionID string) (*account.MFAFinishEnrollmentResult, error) {
	return am.finishEnrollment(ctx, accountID, userID, pendingToken, code, jwtSessionID, false)
}

// MFAFinishReEnrollment completes an enrollment that replaces the user's
// current 2FA. The caller must have verified the user's 2FA session. The
// token is bound to the factor it was started from: once any enrollment
// replaces that factor, including this one finishing before, or a reset
// removes it, the token is refused.
func (am *DefaultAccountManager) MFAFinishReEnrollment(ctx context.Context, accountID, userID, pendingToken, code, jwtSessionID string) (*account.MFAFinishEnrollmentResult, error) {
	return am.finishEnrollment(ctx, accountID, userID, pendingToken, code, jwtSessionID, true)
}

func (am *DefaultAccountManager) finishEnrollment(ctx context.Context, accountID, userID, pendingToken, code, jwtSessionID string, replace bool) (*account.MFAFinishEnrollmentResult, error) {
	if !am.MFAEnabled() {
		return nil, status.Errorf(status.Internal, "mfa: subsystem not initialized")
	}
	if jwtSessionID == "" {
		return nil, status.Errorf(status.InvalidArgument, "mfa: missing session binding")
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
	now := am.mfaNow()
	step, valid, err := mfa.MatchTOTPStep(code, pending.Secret, now)
	if err != nil {
		return nil, fmt.Errorf("mfa: validate: %w", err)
	}
	if !valid {
		return nil, status.Errorf(status.InvalidArgument, "mfa: code does not match")
	}
	// Hashing and encryption are slow, so they happen before any lock.
	plaintext, hashes, err := mfa.GenerateBackupCodes()
	if err != nil {
		return nil, fmt.Errorf("mfa: backup codes: %w", err)
	}
	secretEnc, err := am.mfaSecretCipher.Encrypt(pending.Secret)
	if err != nil {
		return nil, fmt.Errorf("mfa: encrypt secret: %w", err)
	}

	// The user row exists before any user_mfa row, so locking it serializes
	// enrollments of the same user on every replica (lock order: users, then
	// user_mfa).
	err = am.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		user, err := tx.GetUserByUserID(ctx, store.LockingStrengthUpdate, userID)
		if err != nil {
			return err
		}
		if user.AccountID != accountID {
			return status.NewUserNotFoundError(userID)
		}
		existing, err := tx.GetUserMFA(ctx, store.LockingStrengthUpdate, userID)
		if err != nil && !errors.Is(err, store.ErrUserMFANotFound) {
			return err
		}
		if err := checkEnrollmentBinding(existing, pending.ReplacesFactor, replace); err != nil {
			return err
		}
		// The code that finished enrollment counts as used, so it cannot
		// also pass a challenge within its 30-second window.
		return tx.SaveUserMFA(ctx, store.LockingStrengthUpdate, &types.UserMFA{
			UserID:          userID,
			Method:          types.MFAMethodTOTP,
			TOTPSecretEnc:   secretEnc,
			BackupCodesHash: hashes,
			EnrolledAt:      now,
			LastVerifiedAt:  &now,
			LastUsedStep:    step,
			FactorID:        xid.New().String(),
		})
	})
	if err != nil {
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

// checkEnrollmentBinding decides whether a pending enrollment may write
// over the user's current factor, existing (nil when there is none). An
// initial enrollment needs no factor and an initial token. A re-enrollment
// needs the very factor its token was started from.
func checkEnrollmentBinding(existing *types.UserMFA, replacesFactor string, replace bool) error {
	if !replace {
		if existing != nil {
			return errMFAAlreadyEnrolled
		}
		if replacesFactor != "" {
			return errMFAEnrollmentStale
		}
		return nil
	}
	if existing == nil || existing.FactorID != replacesFactor {
		return errMFAEnrollmentStale
	}
	return nil
}
