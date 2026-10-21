package server

import (
	"context"
	"fmt"
	"time"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/auth/jwt"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// GetAccountUsersMFAState returns the 2FA state of the account's users that
// have 2FA, keyed by user ID. Users without 2FA are absent. A lockout is
// reported only while it is in effect.
func (am *DefaultAccountManager) GetAccountUsersMFAState(ctx context.Context, accountID, initiatorUserID string) (map[string]account.UserMFAState, error) {
	if err := am.requireAdminFor(ctx, accountID, initiatorUserID, modules.Users); err != nil {
		return nil, err
	}

	rows, err := am.Store.GetAccountUserMFAs(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	states := make(map[string]account.UserMFAState, len(rows))
	for _, row := range rows {
		state := account.UserMFAState{Enrolled: true}
		if row.LockedUntil != nil && now.Before(*row.LockedUntil) {
			lockedUntil := row.LockedUntil.UTC()
			state.LockedUntil = &lockedUntil
		}
		states[row.UserID] = state
	}
	return states, nil
}

// GetMFAEnrollmentGaps counts the account's active users without 2FA,
// split by the connector the 2FA gate classifies them under.
func (am *DefaultAccountManager) GetMFAEnrollmentGaps(ctx context.Context, accountID, initiatorUserID string) (*account.MFAEnrollmentGaps, error) {
	if err := am.requireAdminFor(ctx, accountID, initiatorUserID, modules.Settings); err != nil {
		return nil, err
	}

	users, err := am.Store.GetAccountUsers(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, err
	}
	rows, err := am.Store.GetAccountUserMFAs(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, err
	}
	enrolled := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		enrolled[row.UserID] = struct{}{}
	}

	gaps := countMFAEnrollmentGaps(users, enrolled)
	return &gaps, nil
}

// countMFAEnrollmentGaps counts the users the 2FA gate would apply to that
// have no 2FA. Users who cannot log in are left out: service users,
// blocked users, and local users that are pending, deactivated or
// tombstoned. A user is local when the connector in their Dex subject is
// "local", the same rule resolveMFAEnforcement applies at request time;
// every other user, including non-Dex and malformed subjects, is federated.
func countMFAEnrollmentGaps(users []*types.User, enrolled map[string]struct{}) account.MFAEnrollmentGaps {
	var gaps account.MFAEnrollmentGaps
	for _, user := range users {
		if user.IsServiceUser || user.Blocked || user.LocalCredentialState.MustStayBlocked() {
			continue
		}
		if _, ok := enrolled[user.Id]; ok {
			continue
		}
		if jwt.ParseDexSubConnector(user.Id) == "local" {
			gaps.Local++
		} else {
			gaps.Federated++
		}
	}
	return gaps
}

// requireAdminFor allows only users who may update the given module.
func (am *DefaultAccountManager) requireAdminFor(ctx context.Context, accountID, userID string, module modules.Module) error {
	allowed, err := am.permissionsManager.ValidateUserPermissions(ctx, accountID, userID, module, operations.Update)
	if err != nil {
		return fmt.Errorf("validate user permissions: %w", err)
	}
	if !allowed {
		return status.NewPermissionDeniedError()
	}
	return nil
}
