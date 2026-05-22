package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// SCIMCreateGroup persists a Group provisioned via SCIM and seeds
// each listed member's AutoGroups with the new group ID. Membership
// in SCIM is user-centric, but openZro's Group model is peer-centric;
// the bridge is User.AutoGroups, which gets propagated to peers on
// registration. Adding a user to a SCIM group therefore means "every
// peer this user owns gets added to this group".
func (am *DefaultAccountManager) SCIMCreateGroup(ctx context.Context, accountID, callerID, displayName string, memberUserIDs []string) (*types.Group, error) {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()

	if err := am.requireSCIMGroupPermission(ctx, accountID, callerID, operations.Create); err != nil {
		return nil, err
	}
	if strings.TrimSpace(displayName) == "" {
		return nil, status.Errorf(status.InvalidArgument, "displayName is required")
	}

	if existing, _ := am.Store.GetGroupByName(ctx, store.LockingStrengthShare, displayName, accountID); existing != nil {
		return nil, ErrSCIMGroupAlreadyExists
	}

	group := &types.Group{
		AccountID: accountID,
		Name:      displayName,
		Issued:    types.GroupIssuedIntegration,
		Peers:     []string{},
	}
	if err := am.SaveGroup(ctx, accountID, callerID, group, true); err != nil {
		return nil, err
	}
	changed, err := am.applyGroupMembers(ctx, accountID, group.ID, memberUserIDs, nil)
	if err != nil {
		return nil, err
	}
	// Fan out NetworkMap when any membership actually moved (#104).
	// The SaveGroup above only fans out when the new (empty) group
	// is already referenced by a policy / route, which is impossible
	// for a fresh group — so this is the only path that can produce
	// a fan-out on SCIMCreateGroup with pre-seeded members.
	if changed {
		am.UpdateAccountPeers(ctx, accountID)
	}
	return group, nil
}

// SCIMReplaceGroup is PUT /Groups/{id}. Members are reset to exactly
// the supplied list — anyone previously in the group whose user is
// not in memberUserIDs gets removed.
func (am *DefaultAccountManager) SCIMReplaceGroup(ctx context.Context, accountID, callerID, groupID, displayName string, memberUserIDs []string) (*types.Group, error) {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()

	if err := am.requireSCIMGroupPermission(ctx, accountID, callerID, operations.Update); err != nil {
		return nil, err
	}

	group, err := am.Store.GetGroupByID(ctx, store.LockingStrengthUpdate, accountID, groupID)
	if err != nil {
		return nil, ErrSCIMGroupNotFound
	}
	if displayName != "" {
		group.Name = displayName
	}
	if err := am.SaveGroup(ctx, accountID, callerID, group, false); err != nil {
		return nil, err
	}
	prev, err := am.usersInGroup(ctx, accountID, groupID)
	if err != nil {
		return nil, err
	}
	changed, err := am.applyGroupMembers(ctx, accountID, groupID, memberUserIDs, prev)
	if err != nil {
		return nil, err
	}
	// Fan out NetworkMap when any membership actually moved (#104).
	// SaveGroup above already fans out for a rename if the group
	// affects peers, but the membership diff is a separate axis —
	// a PUT can leave the display name untouched while moving 50
	// users in or out, and the peer reach must reflect both.
	if changed {
		am.UpdateAccountPeers(ctx, accountID)
	}
	return group, nil
}

// SCIMAddGroupMember and SCIMRemoveGroupMember support PATCH ops.
// Both trigger NetworkMap fan-out when membership actually moves
// (openzro #104): a user added to / removed from a group via the IdP
// must have their peers' reach recomputed in the same Sync cycle as
// any dashboard-side group edit — without it an offboarding via the
// IdP would silently leave the user's peers reaching the group's
// destinations until some other account mutation bumped the serial.
func (am *DefaultAccountManager) SCIMAddGroupMember(ctx context.Context, accountID, callerID, groupID, userID string) error {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()
	if err := am.requireSCIMGroupPermission(ctx, accountID, callerID, operations.Update); err != nil {
		return err
	}
	changed, err := am.modifyUserAutoGroup(ctx, accountID, userID, groupID, true)
	if err != nil {
		return err
	}
	if changed {
		am.UpdateAccountPeers(ctx, accountID)
	}
	return nil
}

func (am *DefaultAccountManager) SCIMRemoveGroupMember(ctx context.Context, accountID, callerID, groupID, userID string) error {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()
	if err := am.requireSCIMGroupPermission(ctx, accountID, callerID, operations.Update); err != nil {
		return err
	}
	changed, err := am.modifyUserAutoGroup(ctx, accountID, userID, groupID, false)
	if err != nil {
		return err
	}
	if changed {
		am.UpdateAccountPeers(ctx, accountID)
	}
	return nil
}

// SCIMRenameGroup updates only the display name. Used by PATCH
// `replace path=displayName`.
func (am *DefaultAccountManager) SCIMRenameGroup(ctx context.Context, accountID, callerID, groupID, newName string) (*types.Group, error) {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()
	if err := am.requireSCIMGroupPermission(ctx, accountID, callerID, operations.Update); err != nil {
		return nil, err
	}
	group, err := am.Store.GetGroupByID(ctx, store.LockingStrengthUpdate, accountID, groupID)
	if err != nil {
		return nil, ErrSCIMGroupNotFound
	}
	if newName != "" {
		group.Name = newName
	}
	if err := am.SaveGroup(ctx, accountID, callerID, group, false); err != nil {
		return nil, err
	}
	return group, nil
}

// SCIMDeleteGroup removes the Group and clears its ID from every
// member's AutoGroups list.
func (am *DefaultAccountManager) SCIMDeleteGroup(ctx context.Context, accountID, callerID, groupID string) error {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()
	if err := am.requireSCIMGroupPermission(ctx, accountID, callerID, operations.Delete); err != nil {
		return err
	}
	if _, err := am.Store.GetGroupByID(ctx, store.LockingStrengthShare, accountID, groupID); err != nil {
		return ErrSCIMGroupNotFound
	}
	prev, err := am.usersInGroup(ctx, accountID, groupID)
	if err != nil {
		return err
	}
	for _, uid := range prev {
		// Errors here are intentionally swallowed (same as before
		// #104): one user's AutoGroups corruption shouldn't block the
		// rest of the cleanup. The DeleteGroup below already triggers
		// fan-out when the group affects peers, so the per-user
		// SaveUser changes will be visible in the next NetworkMap.
		_, _ = am.modifyUserAutoGroup(ctx, accountID, uid, groupID, false)
	}
	return am.DeleteGroup(ctx, accountID, callerID, groupID)
}

// SCIMListGroups returns groups in the account, filtered by display
// name when filter is non-empty.
func (am *DefaultAccountManager) SCIMListGroups(ctx context.Context, accountID, callerID, displayNameFilter string, startIndex, count int) ([]*types.Group, int, error) {
	if err := am.requireSCIMGroupPermission(ctx, accountID, callerID, operations.Read); err != nil {
		return nil, 0, err
	}
	groups, err := am.Store.GetAccountGroups(ctx, store.LockingStrengthShare, accountID)
	if err != nil {
		return nil, 0, err
	}
	filtered := make([]*types.Group, 0, len(groups))
	for _, g := range groups {
		if displayNameFilter != "" && !strings.EqualFold(g.Name, displayNameFilter) {
			continue
		}
		filtered = append(filtered, g)
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

// SCIMGetGroup is GET /Groups/{id}.
func (am *DefaultAccountManager) SCIMGetGroup(ctx context.Context, accountID, callerID, groupID string) (*types.Group, []string, error) {
	if err := am.requireSCIMGroupPermission(ctx, accountID, callerID, operations.Read); err != nil {
		return nil, nil, err
	}
	group, err := am.Store.GetGroupByID(ctx, store.LockingStrengthShare, accountID, groupID)
	if err != nil {
		return nil, nil, ErrSCIMGroupNotFound
	}
	members, err := am.usersInGroup(ctx, accountID, groupID)
	if err != nil {
		return nil, nil, err
	}
	return group, members, nil
}

// usersInGroup returns user IDs whose AutoGroups list contains the
// given group ID — the SCIM-side definition of "membership".
func (am *DefaultAccountManager) usersInGroup(ctx context.Context, accountID, groupID string) ([]string, error) {
	users, err := am.Store.GetAccountUsers(ctx, store.LockingStrengthShare, accountID)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, u := range users {
		for _, g := range u.AutoGroups {
			if g == groupID {
				out = append(out, u.Id)
				break
			}
		}
	}
	return out, nil
}

// applyGroupMembers reconciles members from the desired list against
// previous list. Users in `next` not in `prev` get the group added;
// users in `prev` not in `next` get it removed. Bulk-friendly — one
// SaveUser per affected user, no work for unchanged users.
//
// Returns changed=true when at least one user's AutoGroups actually
// moved; the caller can use that to fire a SINGLE NetworkMap fan-out
// after the whole batch (rather than once per modified user, which
// would amplify the fan-out cost on big SCIM PUT operations).
func (am *DefaultAccountManager) applyGroupMembers(ctx context.Context, accountID, groupID string, next, prev []string) (changed bool, err error) {
	want := stringSet(next)
	have := stringSet(prev)
	for uid := range want {
		if _, ok := have[uid]; ok {
			continue
		}
		moved, err := am.modifyUserAutoGroup(ctx, accountID, uid, groupID, true)
		if err != nil {
			return changed, err
		}
		changed = changed || moved
	}
	for uid := range have {
		if _, ok := want[uid]; ok {
			continue
		}
		moved, err := am.modifyUserAutoGroup(ctx, accountID, uid, groupID, false)
		if err != nil {
			return changed, err
		}
		changed = changed || moved
	}
	return changed, nil
}

// modifyUserAutoGroup adds or removes a single group ID from a user's
// AutoGroups list AND propagates the membership to the affected
// group's Peers list when GroupsPropagationEnabled. Returns
// propagated=true when the change is meaningful to peer reach (User
// row was actually moved, propagation is enabled, and the user has
// at least one peer); the SCIM entrypoints fire UpdateAccountPeers
// on propagated=true so the rest of the account's NetworkMap
// reflects the IdP's new picture in the same Sync cycle.
//
// Mirrors the dashboard's processUserUpdate path (user.go:697-707):
// SaveUser, then if GroupsPropagationEnabled, updateUserPeersInGroups
// + SaveGroups. Closes openzro #104 — without the Group.Peers sync
// here, NetworkMap recomputation reads stale group membership
// because the policy resolver iterates Group.Peers, not
// User.AutoGroups (see GetPeerNetworkMap in account.go).
func (am *DefaultAccountManager) modifyUserAutoGroup(ctx context.Context, accountID, userID, groupID string, add bool) (propagated bool, err error) {
	user, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthUpdate, userID)
	if err != nil {
		return false, err
	}
	if user.AccountID != accountID {
		return false, status.NewUserNotFoundError(userID)
	}
	have := false
	out := make([]string, 0, len(user.AutoGroups))
	for _, g := range user.AutoGroups {
		if g == groupID {
			have = true
			if add {
				out = append(out, g)
			}
			continue
		}
		out = append(out, g)
	}
	if add && !have {
		out = append(out, groupID)
	}
	// No-op short-circuit: the AutoGroups slice would be identical
	// after the rewrite. Avoid the SaveUser write AND signal the
	// caller that no fan-out is needed.
	if add == have {
		return false, nil
	}
	user.AutoGroups = out
	if err := am.Store.SaveUser(ctx, store.LockingStrengthUpdate, user); err != nil {
		return false, err
	}

	// Propagate the AutoGroups delta into Group.Peers so the next
	// NetworkMap recomputation reflects the IdP-driven change.
	// Mirrors the gate the dashboard user-update uses at
	// user.go:614 + the propagation block at user.go:697-707 —
	// settings.GroupsPropagationEnabled is honored verbatim so an
	// operator who opted out keeps their previous behavior.
	settings, err := am.Store.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
	if err != nil {
		return false, err
	}
	if !settings.GroupsPropagationEnabled {
		return false, nil
	}
	userPeers, err := am.Store.GetUserPeers(ctx, store.LockingStrengthShare, accountID, userID)
	if err != nil {
		return false, err
	}
	if len(userPeers) == 0 {
		return false, nil
	}
	accountGroups, err := am.Store.GetAccountGroups(ctx, store.LockingStrengthShare, accountID)
	if err != nil {
		return false, err
	}
	groupsMap := make(map[string]*types.Group, len(accountGroups))
	for _, g := range accountGroups {
		groupsMap[g.ID] = g
	}
	var addGroups, removeGroups []string
	if add {
		addGroups = []string{groupID}
	} else {
		removeGroups = []string{groupID}
	}
	updatedGroups, err := updateUserPeersInGroups(groupsMap, userPeers, addGroups, removeGroups)
	if err != nil {
		return false, fmt.Errorf("modify user peers in groups: %w", err)
	}
	if len(updatedGroups) == 0 {
		// SaveUser already flipped User.AutoGroups; the user's peers
		// were just not in the affected group (e.g. operator removed
		// a user whose peers had never joined the group's Peers list
		// for some reason). Nothing for the fan-out to do — same
		// terminal state.
		return false, nil
	}
	if err := am.Store.SaveGroups(ctx, store.LockingStrengthUpdate, accountID, updatedGroups); err != nil {
		return false, fmt.Errorf("save groups: %w", err)
	}
	return true, nil
}

func (am *DefaultAccountManager) requireSCIMGroupPermission(ctx context.Context, accountID, callerID string, op operations.Operation) error {
	allowed, err := am.permissionsManager.ValidateUserPermissions(ctx, accountID, callerID, modules.Groups, op)
	if err != nil {
		return status.NewPermissionValidationError(err)
	}
	if !allowed {
		return status.NewPermissionDeniedError()
	}
	return nil
}

func stringSet(xs []string) map[string]struct{} {
	out := make(map[string]struct{}, len(xs))
	for _, s := range xs {
		out[s] = struct{}{}
	}
	return out
}

// _ ensures account package is referenced so the import linter does
// not strip it; the SCIMUserPatch type is used elsewhere in this
// package via account.SCIMUserPatch.
var _ = account.SCIMUserInput{}

// Sentinel errors mapped to HTTP statuses by the SCIM handler layer.
var (
	ErrSCIMGroupAlreadyExists = errors.New("scim: group with this displayName already exists")
	ErrSCIMGroupNotFound      = errors.New("scim: group not found")
)
