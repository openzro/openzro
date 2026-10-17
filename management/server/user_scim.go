package server

import (
	"context"
	"errors"
	"fmt"
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
	if err := am.checkProvisionedEmailDomain(ctx, accountID, input.UserName); err != nil {
		return nil, err
	}

	existing, err := am.findSCIMUserByUserName(ctx, accountID, input.UserName)
	if err != nil {
		return nil, err
	}
	if existing != nil {
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
	// A local user's deactivation runs its saga first, outside the account
	// write lock (see the lock discipline in local_user_lifecycle.go).
	if !input.Active {
		if err := am.deactivateIfLocal(ctx, accountID, callerID, userID); err != nil {
			return nil, err
		}
	}

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
	if user.AccountID != accountID || isHiddenFromSCIM(user) {
		return nil, status.NewUserNotFoundError(userID)
	}
	if err := gateLocalUserUpdate(user, &input.Active, &input.UserName); err != nil {
		return nil, err
	}
	// SCIM userName has caseExact=false. A case-only re-send is the same
	// identifier and must retain the legacy-user compatibility exception.
	if !strings.EqualFold(input.UserName, user.SCIMUserName) {
		if err := am.checkProvisionedEmailDomain(ctx, accountID, input.UserName); err != nil {
			return nil, err
		}
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
	if err := am.expireBlockedUserPeers(ctx, accountID, user); err != nil {
		return nil, err
	}
	return user, nil
}

// SCIMPatchUser is PATCH /Users/{id}: only the operations the IdP
// sent are applied. Unspecified fields keep their existing values.
func (am *DefaultAccountManager) SCIMPatchUser(ctx context.Context, accountID, callerID, userID string, patch account.SCIMUserPatch) (*types.User, error) {
	if patch.Active != nil && !*patch.Active {
		if err := am.deactivateIfLocal(ctx, accountID, callerID, userID); err != nil {
			return nil, err
		}
	}

	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()

	if err := am.requireSCIMPermission(ctx, accountID, callerID, operations.Update); err != nil {
		return nil, err
	}

	user, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthUpdate, userID)
	if err != nil {
		return nil, err
	}
	if user.AccountID != accountID || isHiddenFromSCIM(user) {
		return nil, status.NewUserNotFoundError(userID)
	}
	if err := gateLocalUserUpdate(user, patch.Active, patch.UserName); err != nil {
		return nil, err
	}

	wasBlocked := user.Blocked

	if patch.UserName != nil {
		if err := validateSCIMUserInput(&account.SCIMUserInput{UserName: *patch.UserName}); err != nil {
			return nil, err
		}
		if !strings.EqualFold(*patch.UserName, user.SCIMUserName) {
			if err := am.checkProvisionedEmailDomain(ctx, accountID, *patch.UserName); err != nil {
				return nil, err
			}
		}
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
	if err := am.expireBlockedUserPeers(ctx, accountID, user); err != nil {
		return nil, err
	}
	return user, nil
}

// SCIMDeactivateUser implements DELETE /Users/{id} per RFC 7644 §3.6
// — Most IdPs expect "soft delete" semantics: the user is blocked
// (active=false) and remains in the directory. Operators wanting a
// hard delete use the existing /api/users/{id} DELETE.
func (am *DefaultAccountManager) SCIMDeactivateUser(ctx context.Context, accountID, callerID, userID string) error {
	// A local user is tombstoned by its saga, outside the account write
	// lock; this also reaches a tombstone whose cleanup is still pending,
	// which the hidden-user check below would otherwise turn into 404.
	if local, err := am.isLocalUserOf(ctx, accountID, userID); err != nil {
		return err
	} else if local {
		if err := am.requireSCIMPermission(ctx, accountID, callerID, operations.Delete); err != nil {
			return err
		}
		return am.terminateLocalUser(ctx, accountID, callerID, userID, types.LocalCredentialTombstoned)
	}

	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()

	if err := am.requireSCIMPermission(ctx, accountID, callerID, operations.Delete); err != nil {
		return err
	}

	user, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthUpdate, userID)
	if err != nil {
		return err
	}
	if user.AccountID != accountID || isHiddenFromSCIM(user) {
		return status.NewUserNotFoundError(userID)
	}
	if user.LocalCredentialState != types.LocalCredentialNone {
		return errLocalUserDeactivationUnsupported()
	}

	if !user.Blocked {
		user.Blocked = true
		if err := am.Store.SaveUser(ctx, store.LockingStrengthUpdate, user); err != nil {
			return err
		}
		am.StoreEvent(ctx, callerID, user.Id, accountID, activity.UserBlocked, map[string]any{
			"scim": "delete",
		})
	}
	// Runs even when the user was already blocked: a repeated DELETE must
	// still converge on cutting off any peer left connected (#211).
	return am.expireBlockedUserPeers(ctx, accountID, user)
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
		if u.NonDeletable || u.IsServiceUser || isHiddenFromSCIM(u) {
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
	if user.AccountID != accountID || isHiddenFromSCIM(user) {
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

// expireBlockedUserPeers cuts off every peer of a blocked user: it marks
// them login-expired, ends their Sync streams on every replica and
// refreshes the account's network map, the same way the REST block path
// does.
// Blocked=true alone only gates new activity, so without this a SCIM
// deprovisioning left already-connected peers on the network (#211).
// It is a no-op for active users and idempotent for blocked ones.
func (am *DefaultAccountManager) expireBlockedUserPeers(ctx context.Context, accountID string, user *types.User) error {
	if !user.Blocked {
		return nil
	}
	peers, err := am.Store.GetUserPeers(ctx, store.LockingStrengthShare, accountID, user.Id)
	if err != nil {
		return fmt.Errorf("get peers of blocked user %s: %w", user.Id, err)
	}

	// expireAndUpdatePeers only disconnects peers it newly expires. A
	// repeated SCIM block is an explicit retry, so also re-send the fast-path
	// disconnect for this user's already-expired peers: the set is bounded
	// by one user's devices, and the reconciler remains the guarantee.
	var alreadyExpired []string
	for _, peer := range peers {
		if peer.Status != nil && peer.Status.LoginExpired {
			alreadyExpired = append(alreadyExpired, peer.ID)
		}
	}
	if err := am.expireAndUpdatePeers(ctx, accountID, peers); err != nil {
		return fmt.Errorf("expire peers of blocked user %s: %w", user.Id, err)
	}
	if err := am.peersUpdateManager.DisconnectPeers(ctx, alreadyExpired); err != nil {
		return fmt.Errorf("disconnect expired peers of blocked user %s: %w", user.Id, err)
	}
	return nil
}

// isHiddenFromSCIM reports local users that are not SCIM resources: a
// pending reservation and a tombstone (ADR-0024 D3).
func isHiddenFromSCIM(u *types.User) bool {
	return u.LocalCredentialState == types.LocalCredentialPending ||
		u.LocalCredentialState == types.LocalCredentialTombstoned
}

// gateLocalUserUpdate refuses, for local users, the SCIM updates that need
// machinery not built yet: deactivation (the cleanup saga) and renaming (Dex
// cannot re-key a credential). active and userName are nil when the request
// does not touch them. Other users pass through unchanged.
func gateLocalUserUpdate(user *types.User, active *bool, userName *string) error {
	if user.LocalCredentialState == types.LocalCredentialNone {
		return nil
	}
	// Deactivation has already run its saga (deactivateIfLocal) by the time
	// the generic update sees the user; an active local user here means it
	// did not, which must not be applied as a plain block.
	if active != nil && !*active && user.LocalCredentialState == types.LocalCredentialActive {
		return errLocalUserDeactivationUnsupported()
	}
	if userName != nil && !strings.EqualFold(*userName, user.SCIMUserName) {
		return status.Errorf(status.BadRequest, "the userName of a local user cannot be changed")
	}
	return nil
}

func errLocalUserDeactivationUnsupported() error {
	return status.Errorf(status.Internal, "a local user can only be deactivated through its cleanup saga")
}

// isLocalUserOf reports whether userID is a local user of the account. A
// missing user is not an error here; the caller's own lookup reports it.
func (am *DefaultAccountManager) isLocalUserOf(ctx context.Context, accountID, userID string) (bool, error) {
	user, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthShare, userID)
	if err != nil {
		if sErr, ok := status.FromError(err); ok && sErr.Type() == status.NotFound {
			return false, nil
		}
		return false, err
	}
	return user.AccountID == accountID && user.LocalCredentialState != types.LocalCredentialNone, nil
}

// deactivateIfLocal runs the terminal deactivation saga when userID is a
// local user; other users are left to the caller's generic update.
func (am *DefaultAccountManager) deactivateIfLocal(ctx context.Context, accountID, callerID, userID string) error {
	local, err := am.isLocalUserOf(ctx, accountID, userID)
	if err != nil || !local {
		return err
	}
	if err := am.requireSCIMPermission(ctx, accountID, callerID, operations.Update); err != nil {
		return err
	}
	return am.terminateLocalUser(ctx, accountID, callerID, userID, types.LocalCredentialDeactivated)
}
