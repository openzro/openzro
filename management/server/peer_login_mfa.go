package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/xid"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/mfa"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// PeerLoginMFARequest describes a device login that needs the user's second
// factor (ADR-0028 D2, D3). Hostname and OS are what the device reported;
// IP and location are what the server observed.
type PeerLoginMFARequest struct {
	AccountID  string
	UserID     string
	WGPubKey   string
	Hostname   string
	OS         string
	IP         string
	GeoCountry string
	GeoCity    string
	NewPeer    bool
}

// PeerLoginMFAChallenge is the approval a refused login points the client
// to: the record, the page token (valid until the record's deadline) and
// when the record stops being approvable.
type PeerLoginMFAChallenge struct {
	ApprovalID string
	Token      string
	ExpiresAt  time.Time
}

// PeerLoginMFADeniedError refuses a login whose key was denied through the
// approval link, until that record expires.
type PeerLoginMFADeniedError struct {
	RetryAfter time.Duration
}

func (e *PeerLoginMFADeniedError) Error() string {
	return fmt.Sprintf("device login denied; retry after %s", e.RetryAfter)
}

// PeerLoginMFARateLimitedError refuses a login when the user already holds
// the maximum of unexpired approval records.
type PeerLoginMFARateLimitedError struct {
	RetryAfter time.Duration
}

func (e *PeerLoginMFARateLimitedError) Error() string {
	return fmt.Sprintf("too many device logins waiting for approval; retry after %s", e.RetryAfter)
}

// RequestPeerLoginMFAApproval returns the approval a login must wait for.
// It runs under the user row lock, so on any replica a key's pending
// record is reused rather than duplicated and the per-user cap holds:
// a denied key stays denied, and at most PeerLoginMFAMaxPerUser unexpired
// records exist per user, counting every state.
func (am *DefaultAccountManager) RequestPeerLoginMFAApproval(ctx context.Context,
	req PeerLoginMFARequest) (*PeerLoginMFAChallenge, error) {
	if !am.MFAEnabled() {
		return nil, status.Errorf(status.Internal, "mfa: subsystem not initialized")
	}
	now := am.mfaNow()
	var approval *types.PeerLoginMFAApproval
	var created bool
	err := am.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		if err := lockMFAUser(ctx, tx, req.AccountID, req.UserID); err != nil {
			return err
		}
		live, err := tx.GetUserPeerLoginMFAApprovals(ctx, store.LockingStrengthUpdate, req.AccountID, req.UserID, now)
		if err != nil {
			return err
		}
		for _, a := range live {
			if a.WGPubKey != req.WGPubKey {
				continue
			}
			if a.DeniedAt != nil {
				return &PeerLoginMFADeniedError{RetryAfter: a.ExpiresAt.Sub(now)}
			}
			if a.Pending(now) {
				approval = a
				return nil
			}
		}
		if len(live) >= types.PeerLoginMFAMaxPerUser {
			earliest := live[0].ExpiresAt
			for _, a := range live[1:] {
				if a.ExpiresAt.Before(earliest) {
					earliest = a.ExpiresAt
				}
			}
			return &PeerLoginMFARateLimitedError{RetryAfter: earliest.Sub(now)}
		}
		approval = &types.PeerLoginMFAApproval{
			ID: xid.New().String(), AccountID: req.AccountID, UserID: req.UserID, WGPubKey: req.WGPubKey,
			Hostname: req.Hostname, OS: req.OS, IP: req.IP, GeoCountry: req.GeoCountry, GeoCity: req.GeoCity,
			NewPeer: req.NewPeer, CreatedAt: now,
			ExpiresAt: now.Add(types.PeerLoginMFAWindow), Deadline: now.Add(types.PeerLoginMFADeadline),
		}
		created = true
		return tx.CreatePeerLoginMFAApproval(ctx, approval)
	})
	if err != nil {
		return nil, err
	}

	token, err := am.mfaSigner.IssuePeerLogin(req.UserID, approval.ID, approval.Deadline)
	if err != nil {
		return nil, fmt.Errorf("mfa: issue peer login token: %w", err)
	}
	if created {
		am.StoreEvent(ctx, req.UserID, req.UserID, req.AccountID, activity.PeerLoginMFARequired, map[string]any{
			"approval_id": approval.ID, "wg_pub_key": keyFingerprint(req.WGPubKey),
			"ip": req.IP, "new_peer": req.NewPeer,
		})
	}
	return &PeerLoginMFAChallenge{ApprovalID: approval.ID, Token: token, ExpiresAt: approval.ExpiresAt}, nil
}

// GetPeerLoginMFAApproval returns what the approval page shows.
func (am *DefaultAccountManager) GetPeerLoginMFAApproval(ctx context.Context, token string) (*account.PeerLoginMFAView, error) {
	claims, err := am.verifyPeerLoginToken(token)
	if err != nil {
		return nil, err
	}
	approval, err := am.peerLoginApproval(ctx, am.Store, store.LockingStrengthNone, claims)
	if err != nil {
		return nil, err
	}
	return am.peerLoginView(ctx, approval, am.mfaNow())
}

// ApprovePeerLoginMFA checks the user's code with the dashboard
// challenge's verifier, under the same lock order as every approval
// write (users, approval, user_mfa), and approves the pending login. The
// approval extends the record by PeerLoginMFAApprovedGrace, never past its
// deadline. A retry of an approval that already happened succeeds without
// a code, since its response may have been lost. A wrong code is counted
// toward the user's lockout and committed.
func (am *DefaultAccountManager) ApprovePeerLoginMFA(ctx context.Context, token, code string) (*account.PeerLoginMFAApproveOutcome, error) {
	if !am.MFAEnabled() {
		return nil, status.Errorf(status.Internal, "mfa: subsystem not initialized")
	}
	claims, err := am.verifyPeerLoginToken(token)
	if err != nil {
		return nil, err
	}
	now := am.mfaNow()
	var approval *types.PeerLoginMFAApproval
	var outcome *account.PeerLoginMFAApproveOutcome
	var verified bool
	err = am.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		approval, err = am.lockPeerLoginApproval(ctx, tx, claims)
		if err != nil {
			return err
		}
		if approval.Usable(now) {
			outcome = &account.PeerLoginMFAApproveOutcome{Approved: true}
			return nil
		}
		if !approval.Pending(now) {
			return status.Errorf(status.PreconditionFailed, "this device login can no longer be approved")
		}
		check, err := am.verifySecondFactorTx(ctx, tx, claims.UserID, code, now)
		if errors.Is(err, store.ErrUserMFANotFound) {
			return status.Errorf(status.PreconditionFailed, "two-factor authentication is not set up")
		}
		if err != nil {
			return err
		}
		verified = true
		outcome = &account.PeerLoginMFAApproveOutcome{
			CodeReused: check.CodeReused, Locked: check.Locked, LockedUntil: check.LockedUntil,
		}
		if !check.OK {
			return nil
		}
		expiresAt := now.Add(types.PeerLoginMFAApprovedGrace)
		if approval.ExpiresAt.After(expiresAt) {
			expiresAt = approval.ExpiresAt
		}
		if expiresAt.After(approval.Deadline) {
			expiresAt = approval.Deadline
		}
		ok, err := tx.ApprovePeerLoginMFAApproval(ctx, approval.ID, now, expiresAt)
		if err != nil {
			return err
		}
		if !ok {
			return status.Errorf(status.Internal, "approval changed under its lock")
		}
		outcome.Approved = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if verified {
		event, meta := activity.PeerLoginMFAApproved, map[string]any{"approval_id": approval.ID, "via": "approval link"}
		if !outcome.Approved {
			event = activity.PeerLoginMFAFailed
			meta["reason"] = approveFailureReason(outcome)
		}
		am.StoreEvent(ctx, activity.SystemInitiator, approval.UserID, approval.AccountID, event, meta)
	}
	return outcome, nil
}

// DenyPeerLoginMFA records "Not me" for the pending login. The key stays
// denied until the record expires. Denying again is harmless; an attempt
// that was already approved cannot be denied.
func (am *DefaultAccountManager) DenyPeerLoginMFA(ctx context.Context, token string) (*account.PeerLoginMFAView, error) {
	claims, err := am.verifyPeerLoginToken(token)
	if err != nil {
		return nil, err
	}
	now := am.mfaNow()
	var approval *types.PeerLoginMFAApproval
	var denied bool
	err = am.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		approval, err = am.lockPeerLoginApproval(ctx, tx, claims)
		if err != nil {
			return err
		}
		if approval.DeniedAt != nil {
			return nil
		}
		ok, err := tx.DenyPeerLoginMFAApproval(ctx, approval.ID, now)
		if err != nil {
			return err
		}
		if !ok {
			return status.Errorf(status.PreconditionFailed, "this device login can no longer be denied")
		}
		denied = true
		approval.DeniedAt = &now
		return nil
	})
	if err != nil {
		return nil, err
	}
	if denied {
		am.StoreEvent(ctx, activity.SystemInitiator, approval.UserID, approval.AccountID, activity.PeerLoginMFADenied,
			map[string]any{"approval_id": approval.ID, "note": "denied through the approval link"})
	}
	return am.peerLoginView(ctx, approval, now)
}

// verifyPeerLoginToken checks the page token. Any failure is Unauthorized,
// with no detail.
func (am *DefaultAccountManager) verifyPeerLoginToken(token string) (mfa.VerifyResult, error) {
	if !am.MFAEnabled() {
		return mfa.VerifyResult{}, status.Errorf(status.Internal, "mfa: subsystem not initialized")
	}
	claims, err := am.mfaSigner.Verify(token, mfa.PurposePeerLogin)
	if err != nil || claims.ApprovalID == "" {
		return mfa.VerifyResult{}, status.Errorf(status.Unauthorized, "invalid approval link")
	}
	return claims, nil
}

// lockPeerLoginApproval takes the user row lock and then the record's, the
// order every approval write uses.
func (am *DefaultAccountManager) lockPeerLoginApproval(ctx context.Context, tx store.Store,
	claims mfa.VerifyResult) (*types.PeerLoginMFAApproval, error) {
	user, err := tx.GetUserByUserID(ctx, store.LockingStrengthUpdate, claims.UserID)
	if err != nil {
		return nil, status.Errorf(status.Unauthorized, "invalid approval link")
	}
	approval, err := am.peerLoginApproval(ctx, tx, store.LockingStrengthUpdate, claims)
	if err != nil {
		return nil, err
	}
	if approval.AccountID != user.AccountID {
		return nil, status.Errorf(status.Unauthorized, "invalid approval link")
	}
	return approval, nil
}

// peerLoginApproval loads the token's record and checks it belongs to the
// token's user. A record already swept reads as expired.
func (am *DefaultAccountManager) peerLoginApproval(ctx context.Context, s store.Store, lock store.LockingStrength,
	claims mfa.VerifyResult) (*types.PeerLoginMFAApproval, error) {
	approval, err := s.GetPeerLoginMFAApprovalByID(ctx, lock, claims.ApprovalID)
	if errors.Is(err, store.ErrPeerLoginMFAApprovalNotFound) {
		return nil, status.Errorf(status.PreconditionFailed, "this device login has expired")
	}
	if err != nil {
		return nil, err
	}
	if approval.UserID != claims.UserID {
		return nil, status.Errorf(status.Unauthorized, "invalid approval link")
	}
	return approval, nil
}

func (am *DefaultAccountManager) peerLoginView(ctx context.Context, a *types.PeerLoginMFAApproval,
	now time.Time) (*account.PeerLoginMFAView, error) {
	user, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthNone, a.UserID)
	if err != nil {
		return nil, err
	}
	meta, err := am.Store.GetAccountMeta(ctx, store.LockingStrengthNone, a.AccountID)
	if err != nil {
		return nil, err
	}
	return &account.PeerLoginMFAView{
		State: peerLoginState(a, now), Hostname: a.Hostname, OS: a.OS, IP: a.IP,
		GeoCountry: a.GeoCountry, GeoCity: a.GeoCity, NewPeer: a.NewPeer,
		KeyFingerprint: keyFingerprint(a.WGPubKey), MaskedEmail: maskEmail(user.Email),
		AccountDomain: meta.Domain, CreatedAt: a.CreatedAt, ExpiresAt: a.ExpiresAt,
	}, nil
}

func peerLoginState(a *types.PeerLoginMFAApproval, now time.Time) account.PeerLoginMFAState {
	switch {
	case a.DeniedAt != nil:
		return account.PeerLoginMFADenied
	case a.ConsumedAt != nil:
		return account.PeerLoginMFAConsumed
	case !now.Before(a.ExpiresAt):
		return account.PeerLoginMFAExpired
	case a.ApprovedAt != nil:
		return account.PeerLoginMFAApproved
	default:
		return account.PeerLoginMFAPending
	}
}

func approveFailureReason(o *account.PeerLoginMFAApproveOutcome) string {
	switch {
	case o.CodeReused:
		return "code already used"
	case o.Locked:
		return "locked"
	default:
		return "wrong code"
	}
}

// lockMFAUser locks the user row, the first lock of every approval write,
// and checks the user belongs to the account.
func lockMFAUser(ctx context.Context, tx store.Store, accountID, userID string) error {
	user, err := tx.GetUserByUserID(ctx, store.LockingStrengthUpdate, userID)
	if err != nil {
		return err
	}
	if user.AccountID != accountID {
		return status.NewUserNotFoundError(userID)
	}
	return nil
}

// keyFingerprint shortens a WireGuard key for display: enough to tell keys
// apart, not to reproduce one.
func keyFingerprint(key string) string {
	if len(key) <= 10 {
		return key
	}
	return key[:6] + "…" + key[len(key)-4:]
}

// maskEmail keeps the first letter of the local part and the domain.
func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 1 {
		return ""
	}
	return email[:1] + "***" + email[at:]
}
