package server

import (
	"context"
	"errors"

	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// checkPeerLoginMFAActivation refuses a settings change that switches the
// device-login gate on, or widens the users it applies to while it is on,
// while an active user it would then apply to has no 2FA (ADR-0028 D10).
// It guards the activation only: users created or reset later are allowed,
// and peer approval covers them (D9).
func checkPeerLoginMFAActivation(ctx context.Context, tx store.Store, accountID string,
	oldSettings, newSettings *types.Settings) error {
	if !newSettings.MFAEnforcePeerLogin {
		return nil
	}
	widened := !oldSettings.MFAEnforcePeerLogin ||
		(newSettings.MFAEnforceLocal && !oldSettings.MFAEnforceLocal) ||
		(newSettings.MFAEnforceFederated && !oldSettings.MFAEnforceFederated)
	if !widened || (!newSettings.MFAEnforceLocal && !newSettings.MFAEnforceFederated) {
		return nil
	}

	users, err := tx.GetAccountUsers(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return err
	}
	rows, err := tx.GetAccountUserMFAs(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return err
	}
	enrolled := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		enrolled[row.UserID] = struct{}{}
	}
	gaps := countMFAEnrollmentGaps(users, enrolled)

	missing := 0
	if newSettings.MFAEnforceLocal {
		missing += gaps.Local
	}
	if newSettings.MFAEnforceFederated {
		missing += gaps.Federated
	}
	if missing > 0 {
		return status.Errorf(status.PreconditionFailed,
			"device login 2FA needs every user it applies to enrolled first: %d active users have no 2FA", missing)
	}
	return nil
}

// handlePeerLoginMFASettings audits switching the device-login gate.
func (am *DefaultAccountManager) handlePeerLoginMFASettings(ctx context.Context, oldSettings, newSettings *types.Settings,
	userID, accountID string) {
	if oldSettings.MFAEnforcePeerLogin == newSettings.MFAEnforcePeerLogin {
		return
	}
	event := activity.AccountMFAPeerLoginDisabled
	if newSettings.MFAEnforcePeerLogin {
		event = activity.AccountMFAPeerLoginEnabled
	}
	am.StoreEvent(ctx, userID, accountID, accountID, event, nil)
}

// PeerLoginMFARequiredError refuses a device login until the user approves
// it with their second factor. Its text carries no token: the gRPC layer
// puts the challenge in the refusal's details only.
type PeerLoginMFARequiredError struct {
	Challenge *PeerLoginMFAChallenge
}

func (e *PeerLoginMFARequiredError) Error() string {
	return "device login waiting for 2FA approval"
}

// PeerLoginMFAEnrollmentRequiredError refuses a device login from a user
// the gate applies to who has no 2FA yet.
type PeerLoginMFAEnrollmentRequiredError struct{}

func (e *PeerLoginMFAEnrollmentRequiredError) Error() string {
	return "device login needs 2FA set up first"
}

// errPeerLoginMFA rolls a login back when the gate refuses it; LoginPeer
// then creates the approval outside the login's transaction (D3).
type errPeerLoginMFA struct {
	enrollment bool
	newPeer    bool
}

func (e *errPeerLoginMFA) Error() string {
	if e.enrollment {
		return "device login needs 2FA set up first"
	}
	return "device login waiting for 2FA approval"
}

// peerLoginMFARequired says whether a user's device login must be
// approved with the second factor (D2).
func (am *DefaultAccountManager) peerLoginMFARequired(settings *types.Settings, connectorID string) bool {
	return settings != nil && settings.MFAEnforcePeerLogin && am.resolveMFAEnforcement(connectorID, settings)
}

// checkPeerLoginMFA runs in the login's transaction, after the user row is
// locked. It uses the key's approval if there is one; otherwise the login
// rolls back with errPeerLoginMFA.
func (am *DefaultAccountManager) checkPeerLoginMFA(ctx context.Context, tx store.Store, user *types.User,
	wgPubKey string, newPeer bool) error {
	if user.IsServiceUser {
		return nil
	}
	if !am.MFAEnabled() {
		return status.Errorf(status.Internal, "mfa: device login 2FA is on but the subsystem is disabled")
	}
	ok, err := tx.ConsumePeerLoginMFAApproval(ctx, user.AccountID, user.Id, wgPubKey, am.mfaNow())
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	_, err = tx.GetUserMFA(ctx, store.LockingStrengthNone, user.Id)
	if errors.Is(err, store.ErrUserMFANotFound) {
		return &errPeerLoginMFA{enrollment: true, newPeer: newPeer}
	}
	if err != nil {
		return err
	}
	return &errPeerLoginMFA{newPeer: newPeer}
}

// peerLoginMFARefusal turns a login the gate rolled back into the refusal
// the client acts on, creating or reusing the approval it must wait for.
func (am *DefaultAccountManager) peerLoginMFARefusal(ctx context.Context, login types.PeerLogin,
	accountID string, gate *errPeerLoginMFA) error {
	if gate.enrollment {
		return &PeerLoginMFAEnrollmentRequiredError{}
	}
	req := PeerLoginMFARequest{
		AccountID: accountID, UserID: login.UserID, WGPubKey: login.WireGuardPubKey,
		Hostname: login.Meta.Hostname, OS: login.Meta.OS, NewPeer: gate.newPeer, JWTIssuedAt: login.JWTIssuedAt,
	}
	if login.ConnectionIP != nil {
		req.IP = login.ConnectionIP.String()
		if am.geo != nil {
			if loc, err := am.geo.Lookup(login.ConnectionIP); err == nil {
				req.GeoCountry, req.GeoCity = loc.Country.ISOCode, loc.City.Names.En
			}
		}
	}
	challenge, err := am.RequestPeerLoginMFAApproval(ctx, req)
	if err != nil {
		return err
	}
	return &PeerLoginMFARequiredError{Challenge: challenge}
}

// peerLoginMFARefusalReason names a gate refusal for the metrics.
func peerLoginMFARefusalReason(err error) string {
	var (
		required  *PeerLoginMFARequiredError
		enroll    *PeerLoginMFAEnrollmentRequiredError
		denied    *PeerLoginMFADeniedError
		throttled *PeerLoginMFARateLimitedError
	)
	switch {
	case errors.As(err, &required):
		return "required"
	case errors.As(err, &enroll):
		return "enrollment_required"
	case errors.As(err, &denied):
		return "denied"
	case errors.As(err, &throttled):
		return "rate_limited"
	default:
		return "error"
	}
}
