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
	propagated, err := am.applySCIMGroupMembers(ctx, accountID, group.ID, memberUserIDs, nil)
	if err != nil {
		return nil, err
	}
	// SaveGroup above only fans out for a group already referenced
	// by a policy / route — impossible for a fresh group — so this
	// is the only path that can produce a fan-out on SCIMCreateGroup
	// with pre-seeded members.
	if propagated {
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
	propagated, err := am.applySCIMGroupMembers(ctx, accountID, groupID, memberUserIDs, prev)
	if err != nil {
		return nil, err
	}
	// SaveGroup above already fans out for a rename if the group
	// affects peers, but the membership diff is a separate axis —
	// a PUT can leave the display name untouched while moving 50
	// users in or out, and the peer reach must reflect both.
	if propagated {
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
	return am.scimSingleUserMembership(ctx, accountID, callerID, groupID, userID, true)
}

func (am *DefaultAccountManager) SCIMRemoveGroupMember(ctx context.Context, accountID, callerID, groupID, userID string) error {
	return am.scimSingleUserMembership(ctx, accountID, callerID, groupID, userID, false)
}

// scimSingleUserMembership is the shared body of SCIMAddGroupMember
// + SCIMRemoveGroupMember. Wraps the User + Group.Peers + serial
// writes in a single transaction so a partial-write failure leaves
// no torn state (#104 review-1: SaveUser succeeding while a later
// Group.Peers / serial save fails used to leave the user "removed"
// in their AutoGroups while the group still listed their peers,
// because the retry path no-op'd on User.AutoGroups already being
// correct). IncrementNetworkSerial runs inside the same transaction
// so concurrent SCIM membership changes can't emit different
// NetworkMaps under the same serial (#104 review-2).
func (am *DefaultAccountManager) scimSingleUserMembership(ctx context.Context, accountID, callerID, groupID, userID string, add bool) error {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()
	if err := am.requireSCIMGroupPermission(ctx, accountID, callerID, operations.Update); err != nil {
		return err
	}

	var propagated bool
	err := am.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		ctx := ctx
		settings, err := tx.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
		if err != nil {
			return err
		}
		groupsMap, err := loadGroupsMap(ctx, tx, accountID)
		if err != nil {
			return err
		}
		groupsTouched := newGroupSet()
		if err := am.applyUserAutoGroupInTx(ctx, tx, accountID, settings, groupsMap, groupsTouched, userID, groupID, add); err != nil {
			return err
		}
		return am.commitSCIMGroupChanges(ctx, tx, accountID, groupsMap, groupsTouched, &propagated)
	})
	if err != nil {
		return err
	}
	if propagated {
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
// member's AutoGroups list — in a single transaction so a failure
// mid-way doesn't leave half-cleaned User.AutoGroups records that a
// retry's no-op check would skip (#104 review-1).
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

	var propagated bool
	err = am.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		ctx := ctx
		settings, err := tx.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
		if err != nil {
			return err
		}
		groupsMap, err := loadGroupsMap(ctx, tx, accountID)
		if err != nil {
			return err
		}
		groupsTouched := newGroupSet()
		for _, uid := range prev {
			if err := am.applyUserAutoGroupInTx(ctx, tx, accountID, settings, groupsMap, groupsTouched, uid, groupID, false); err != nil {
				return err
			}
		}
		return am.commitSCIMGroupChanges(ctx, tx, accountID, groupsMap, groupsTouched, &propagated)
	})
	if err != nil {
		return err
	}
	if propagated {
		am.UpdateAccountPeers(ctx, accountID)
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

// applySCIMGroupMembers reconciles members from the desired list
// against the previous list inside a single transaction. Users in
// `next` not in `prev` get the group added; users in `prev` not in
// `next` get it removed. Loads account settings + groups ONCE for
// the whole batch (#104 review-3), mutates the in-memory groupsMap
// across users, and writes SaveGroups + IncrementNetworkSerial
// exactly once at the end. Returns propagated=true when any
// User.AutoGroups change actually moved Group.Peers (the signal the
// caller uses to fire one UpdateAccountPeers per SCIM operation).
func (am *DefaultAccountManager) applySCIMGroupMembers(ctx context.Context, accountID, groupID string, next, prev []string) (bool, error) {
	want := stringSet(next)
	have := stringSet(prev)

	var propagated bool
	err := am.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		ctx := ctx
		settings, err := tx.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
		if err != nil {
			return err
		}
		groupsMap, err := loadGroupsMap(ctx, tx, accountID)
		if err != nil {
			return err
		}
		groupsTouched := newGroupSet()

		for uid := range want {
			if _, ok := have[uid]; ok {
				continue
			}
			if err := am.applyUserAutoGroupInTx(ctx, tx, accountID, settings, groupsMap, groupsTouched, uid, groupID, true); err != nil {
				return err
			}
		}
		for uid := range have {
			if _, ok := want[uid]; ok {
				continue
			}
			if err := am.applyUserAutoGroupInTx(ctx, tx, accountID, settings, groupsMap, groupsTouched, uid, groupID, false); err != nil {
				return err
			}
		}
		return am.commitSCIMGroupChanges(ctx, tx, accountID, groupsMap, groupsTouched, &propagated)
	})
	return propagated, err
}

// applyUserAutoGroupInTx performs one user's AutoGroups delta inside
// an existing transaction. Adds/removes the group ID from User.
// AutoGroups, persists the user when actually changed, and — when
// GroupsPropagationEnabled — runs updateUserPeersInGroups against
// the shared groupsMap to keep Group.Peers in sync. The shared
// groupsTouched set records which group IDs the caller must
// persist (the caller batches the SaveGroups + IncrementNetwork-
// Serial at commit time so the whole transaction emits ONE serial
// bump regardless of how many users moved).
//
// Crucially this routine does NOT short-circuit when User.AutoGroups
// is already in the desired state — it still runs the Group.Peers
// reconciliation (idempotent via updateUserPeersInGroups). That
// closes the partial-write recovery hole #104 review-1 flagged: a
// retry must repair Group.Peers even when User.AutoGroups was
// already correct from an earlier half-completed attempt.
func (am *DefaultAccountManager) applyUserAutoGroupInTx(
	ctx context.Context,
	tx store.Store,
	accountID string,
	settings *types.Settings,
	groupsMap map[string]*types.Group,
	groupsTouched groupSet,
	userID, groupID string,
	add bool,
) error {
	user, err := tx.GetUserByUserID(ctx, store.LockingStrengthUpdate, userID)
	if err != nil {
		return err
	}
	if user.AccountID != accountID {
		return status.NewUserNotFoundError(userID)
	}

	next, userChanged := mutateAutoGroups(user.AutoGroups, groupID, add)
	if userChanged {
		user.AutoGroups = next
		if err := tx.SaveUser(ctx, store.LockingStrengthUpdate, user); err != nil {
			return err
		}
	}

	if !settings.GroupsPropagationEnabled {
		return nil
	}
	userPeers, err := tx.GetUserPeers(ctx, store.LockingStrengthShare, accountID, userID)
	if err != nil {
		return err
	}
	if len(userPeers) == 0 {
		return nil
	}

	var addGroups, removeGroups []string
	if add {
		addGroups = []string{groupID}
	} else {
		removeGroups = []string{groupID}
	}
	updatedGroups, err := updateUserPeersInGroups(groupsMap, userPeers, addGroups, removeGroups)
	if err != nil {
		return fmt.Errorf("update user peers in groups: %w", err)
	}
	for _, g := range updatedGroups {
		groupsTouched[g.ID] = struct{}{}
	}
	return nil
}

// commitSCIMGroupChanges writes the groups touched in this
// transaction and bumps the network serial — both inside the
// transaction so concurrent SCIM operations can't sneak conflicting
// NetworkMaps under the same serial (#104 review-2). Sets *propagated
// to true when at least one group was touched so the caller knows
// to fire UpdateAccountPeers after the transaction commits.
func (am *DefaultAccountManager) commitSCIMGroupChanges(
	ctx context.Context,
	tx store.Store,
	accountID string,
	groupsMap map[string]*types.Group,
	groupsTouched groupSet,
	propagated *bool,
) error {
	if len(groupsTouched) == 0 {
		return nil
	}
	groupsToSave := make([]*types.Group, 0, len(groupsTouched))
	for gid := range groupsTouched {
		groupsToSave = append(groupsToSave, groupsMap[gid])
	}
	if err := tx.SaveGroups(ctx, store.LockingStrengthUpdate, accountID, groupsToSave); err != nil {
		return fmt.Errorf("save groups: %w", err)
	}
	if err := tx.IncrementNetworkSerial(ctx, store.LockingStrengthUpdate, accountID); err != nil {
		return fmt.Errorf("increment network serial: %w", err)
	}
	*propagated = true
	return nil
}

// mutateAutoGroups builds the new AutoGroups list after adding or
// removing groupID. Pure (no store access) so it stays trivially
// testable. Returns changed=false when the input already matched
// the target state.
func mutateAutoGroups(curr []string, groupID string, add bool) (next []string, changed bool) {
	have := false
	out := make([]string, 0, len(curr))
	for _, g := range curr {
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
	return out, add != have
}

// groupSet is a tiny alias for the set of group IDs touched in a
// SCIM transaction. Named so the helper signatures stay readable.
type groupSet map[string]struct{}

func newGroupSet() groupSet { return groupSet{} }

// loadGroupsMap reads every group in the account and returns it
// keyed by ID. Used once per SCIM transaction so the per-user
// Group.Peers reconciliation can mutate the same map across all
// modified users.
func loadGroupsMap(ctx context.Context, tx store.Store, accountID string) (map[string]*types.Group, error) {
	groups, err := tx.GetAccountGroups(ctx, store.LockingStrengthShare, accountID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*types.Group, len(groups))
	for _, g := range groups {
		out[g.ID] = g
	}
	return out, nil
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
