package server

import (
	"context"
	"errors"
	"fmt"

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

// errMFAEnrollmentUsed refuses finishing an enrollment whose pending
// secret is already the stored one: the same enrollment cannot complete
// twice.
var errMFAEnrollmentUsed = status.Errorf(status.PreconditionFailed,
	"this enrollment was already completed; start again")

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
	return am.startEnrollment(ctx, accountID, userID)
}

// MFAStartReEnrollment begins an enrollment that may replace the user's
// current 2FA. The caller must have verified the user's 2FA session.
func (am *DefaultAccountManager) MFAStartReEnrollment(ctx context.Context, accountID, userID string) (*account.MFAStartEnrollmentResult, error) {
	if !am.MFAEnabled() {
		return nil, status.Errorf(status.Internal, "mfa: subsystem not initialized")
	}
	return am.startEnrollment(ctx, accountID, userID)
}

func (am *DefaultAccountManager) startEnrollment(ctx context.Context, accountID, userID string) (*account.MFAStartEnrollmentResult, error) {
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
// current 2FA, or creates it if there is none. The caller must have
// verified the user's 2FA session. Finishing the same pending secret
// twice is refused.
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
		switch {
		case errors.Is(err, store.ErrUserMFANotFound):
		case err != nil:
			return err
		case !replace:
			return errMFAAlreadyEnrolled
		default:
			current, err := am.mfaSecretCipher.Decrypt(existing.TOTPSecretEnc)
			if err != nil {
				return fmt.Errorf("mfa: decrypt secret: %w", err)
			}
			if current == pending.Secret {
				return errMFAEnrollmentUsed
			}
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
