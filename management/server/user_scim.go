package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// SCIMCreateUser persists a user provisioned by an external IdP
// without round-tripping through the IdP itself. The IdP is the
// caller (via SCIM); the management's job is to remember the user.
//
// Returns ErrSCIMUserAlreadyExists when a user with the same
// userName already exists in the account — SCIM clients expect a
// HTTP 409 in that case.
func (am *DefaultAccountManager) SCIMCreateUser(ctx context.Context, accountID, callerID string, input account.SCIMUserInput) (*types.User, error) {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()

	if err := am.requireSCIMPermission(ctx, accountID, callerID, operations.Create); err != nil {
		return nil, err
	}
	if err := validateSCIMUserInput(&input); err != nil {
		return nil, err
	}

	if existing, err := am.findSCIMUserByUserName(ctx, accountID, input.UserName); err == nil && existing != nil {
		return nil, ErrSCIMUserAlreadyExists
	}

	user := &types.User{
		Id:              uuid.NewString(),
		AccountID:       accountID,
		Role:            types.UserRoleUser,
		AutoGroups:      input.AutoGroups,
		Issued:          types.UserIssuedIntegration,
		CreatedAt:       time.Now().UTC(),
		Blocked:         !input.Active,
		SCIMUserName:    input.UserName,
		SCIMDisplayName: input.DisplayName,
		SCIMExternalID:  input.ExternalID,
	}

	if err := am.Store.SaveUser(ctx, store.LockingStrengthUpdate, user); err != nil {
		return nil, err
	}

	am.StoreEvent(ctx, callerID, user.Id, accountID, activity.UserInvited, map[string]any{
		"scim":       true,
		"username":   input.UserName,
		"name":       input.DisplayName,
		"externalId": input.ExternalID,
	})
	return user, nil
}

// SCIMReplaceUser is PUT /Users/{id}: caller sends the full new
// representation. Fields not present in the input revert to their
// defaults.
func (am *DefaultAccountManager) SCIMReplaceUser(ctx context.Context, accountID, callerID, userID string, input account.SCIMUserInput) (*types.User, error) {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()

	if err := am.requireSCIMPermission(ctx, accountID, callerID, operations.Update); err != nil {
		return nil, err
	}
	if err := validateSCIMUserInput(&input); err != nil {
		return nil, err
	}

	user, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthUpdate, userID)
	if err != nil {
		return nil, err
	}
	if user.AccountID != accountID {
		return nil, status.NewUserNotFoundError(userID)
	}

	user.SCIMUserName = input.UserName
	user.SCIMDisplayName = input.DisplayName
	user.SCIMExternalID = input.ExternalID
	user.Blocked = !input.Active
	if input.AutoGroups != nil {
		user.AutoGroups = input.AutoGroups
	}

	if err := am.Store.SaveUser(ctx, store.LockingStrengthUpdate, user); err != nil {
		return nil, err
	}
	am.StoreEvent(ctx, callerID, user.Id, accountID, activity.UserBlocked, map[string]any{
		"scim": "replace", "username": input.UserName,
	})
	return user, nil
}

// SCIMPatchUser is PATCH /Users/{id}: only the operations the IdP
// sent are applied. Unspecified fields keep their existing values.
func (am *DefaultAccountManager) SCIMPatchUser(ctx context.Context, accountID, callerID, userID string, patch account.SCIMUserPatch) (*types.User, error) {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()

	if err := am.requireSCIMPermission(ctx, accountID, callerID, operations.Update); err != nil {
		return nil, err
	}

	user, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthUpdate, userID)
	if err != nil {
		return nil, err
	}
	if user.AccountID != accountID {
		return nil, status.NewUserNotFoundError(userID)
	}

	wasBlocked := user.Blocked

	if patch.UserName != nil {
		user.SCIMUserName = *patch.UserName
	}
	if patch.DisplayName != nil {
		user.SCIMDisplayName = *patch.DisplayName
	}
	if patch.Active != nil {
		user.Blocked = !*patch.Active
	}
	if patch.AutoGroups != nil {
		user.AutoGroups = *patch.AutoGroups
	}

	if err := am.Store.SaveUser(ctx, store.LockingStrengthUpdate, user); err != nil {
		return nil, err
	}

	// Activity events for the most operationally-significant change:
	// (de)activation. Other field edits are still persisted but logged
	// only at Debug — adding a new activity code per attribute is more
	// noise than signal.
	if patch.Active != nil && wasBlocked != user.Blocked {
		event := activity.UserUnblocked
		if user.Blocked {
			event = activity.UserBlocked
		}
		am.StoreEvent(ctx, callerID, user.Id, accountID, event, map[string]any{
			"scim": "patch",
		})
	}
	return user, nil
}

// SCIMDeactivateUser implements DELETE /Users/{id} per RFC 7644 §3.6
// — Most IdPs expect "soft delete" semantics: the user is blocked
// (active=false) and remains in the directory. Operators wanting a
// hard delete use the existing /api/users/{id} DELETE.
func (am *DefaultAccountManager) SCIMDeactivateUser(ctx context.Context, accountID, callerID, userID string) error {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()

	if err := am.requireSCIMPermission(ctx, accountID, callerID, operations.Delete); err != nil {
		return err
	}

	user, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthUpdate, userID)
	if err != nil {
		return err
	}
	if user.AccountID != accountID {
		return status.NewUserNotFoundError(userID)
	}

	if user.Blocked {
		return nil // already deactivated; idempotent
	}
	user.Blocked = true
	if err := am.Store.SaveUser(ctx, store.LockingStrengthUpdate, user); err != nil {
		return err
	}
	am.StoreEvent(ctx, callerID, user.Id, accountID, activity.UserBlocked, map[string]any{
		"scim": "delete",
	})
	return nil
}

// SCIMListUsers returns SCIM-provisioned users in the account.
// Filtering by userName is supported via the `userNameFilter`
// parameter (empty means "no filter"). Pagination uses 1-based
// startIndex per RFC 7644 §3.4.2.
func (am *DefaultAccountManager) SCIMListUsers(ctx context.Context, accountID, callerID, userNameFilter string, startIndex, count int) ([]*types.User, int, error) {
	if err := am.requireSCIMPermission(ctx, accountID, callerID, operations.Read); err != nil {
		return nil, 0, err
	}

	users, err := am.Store.GetAccountUsers(ctx, store.LockingStrengthShare, accountID)
	if err != nil {
		return nil, 0, err
	}

	filtered := make([]*types.User, 0, len(users))
	for _, u := range users {
		if u.NonDeletable || u.IsServiceUser {
			continue
		}
		if userNameFilter != "" && !strings.EqualFold(u.SCIMUserName, userNameFilter) {
			continue
		}
		filtered = append(filtered, u)
	}

	total := len(filtered)
	if startIndex < 1 {
		startIndex = 1
	}
	if count <= 0 {
		count = 100
	}
	from := startIndex - 1
	if from > total {
		from = total
	}
	to := from + count
	if to > total {
		to = total
	}
	return filtered[from:to], total, nil
}

// SCIMGetUser is GET /Users/{id} — fetches a single user, scoped
// to the caller's account. Returns ErrSCIMUserNotFound on miss.
func (am *DefaultAccountManager) SCIMGetUser(ctx context.Context, accountID, callerID, userID string) (*types.User, error) {
	if err := am.requireSCIMPermission(ctx, accountID, callerID, operations.Read); err != nil {
		return nil, err
	}
	user, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthShare, userID)
	if err != nil {
		return nil, ErrSCIMUserNotFound
	}
	if user.AccountID != accountID {
		return nil, ErrSCIMUserNotFound
	}
	return user, nil
}

// findSCIMUserByUserName scans the account's user set for a match.
// Returns nil, nil when no user has this userName (a non-error case
// in the create flow).
func (am *DefaultAccountManager) findSCIMUserByUserName(ctx context.Context, accountID, userName string) (*types.User, error) {
	users, err := am.Store.GetAccountUsers(ctx, store.LockingStrengthShare, accountID)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		if strings.EqualFold(u.SCIMUserName, userName) {
			return u, nil
		}
	}
	// Documented contract: "not found" is a non-error case in the SCIM
	// create flow; the caller (SCIMCreateUser) checks err == nil &&
	// existing != nil to decide whether the userName collides.
	return nil, nil //nolint:nilnil // see comment above.
}

func (am *DefaultAccountManager) requireSCIMPermission(ctx context.Context, accountID, callerID string, op operations.Operation) error {
	allowed, err := am.permissionsManager.ValidateUserPermissions(ctx, accountID, callerID, modules.Users, op)
	if err != nil {
		return status.NewPermissionValidationError(err)
	}
	if !allowed {
		return status.NewPermissionDeniedError()
	}
	return nil
}

func validateSCIMUserInput(in *account.SCIMUserInput) error {
	if strings.TrimSpace(in.UserName) == "" {
		return status.Errorf(status.InvalidArgument, "userName is required")
	}
	return nil
}

// Sentinel errors translated to SCIM-specific HTTP statuses by the
// handler layer.
var (
	ErrSCIMUserAlreadyExists = errors.New("scim: user with this userName already exists")
	ErrSCIMUserNotFound      = errors.New("scim: user not found")
)
