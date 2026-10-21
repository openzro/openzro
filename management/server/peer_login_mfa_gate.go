package server

import (
	"context"

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
