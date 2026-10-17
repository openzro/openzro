package server

import (
	"context"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/auth/jwt"
	"github.com/openzro/openzro/management/server/dex_proxy"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// Lock discipline for local-user operations: they take the local identity
// lock (and creation then the account lifecycle lock), and must never be
// called while holding the process-local account write lock
// (AcquireWriteLockByUID), which DeleteAccount takes after the lifecycle
// lock. Legacy paths therefore detect local users before taking it.

// SCIMResetLocalPassword replaces a local user's password (ADR-0024 D6). It
// is an isolated operation: when it comes from a SCIM PUT, replace carries
// the rest of the representation, which must equal the current one. The
// Dex credential is written only after an ownership check, and the user's
// refresh tokens are revoked. It does not cut access tokens already issued
// or connected peers; deactivation does.
func (am *DefaultAccountManager) SCIMResetLocalPassword(ctx context.Context, accountID, callerID, userID, password string,
	replace *account.SCIMUserInput) (*types.User, error) {
	if am.localCreds == nil {
		return nil, status.Errorf(status.BadRequest, "Dex is not configured on this deployment")
	}
	if err := am.requireSCIMPermission(ctx, accountID, callerID, operations.Update); err != nil {
		return nil, err
	}
	user, err := am.getSCIMVisibleUser(ctx, accountID, userID)
	if err != nil {
		return nil, err
	}
	if user.LocalCredentialState == types.LocalCredentialNone {
		return nil, status.Errorf(status.BadRequest, "only SCIM local users have a password in openZro")
	}
	if replace != nil && !representationUnchanged(user, replace) {
		return nil, status.Errorf(status.BadRequest, "a request that sets the password may change nothing else")
	}
	if err := validateLocalPassword(password); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), localPasswordBcryptCost)
	if err != nil {
		return nil, status.Errorf(status.Internal, "hash local user password: %v", err)
	}

	unlock, err := am.lockLocalIdentity(ctx, user.SCIMUserName)
	if err != nil {
		return nil, status.Errorf(status.Internal, "%v", err)
	}
	defer unlock()

	// Re-read under the lock: a concurrent deactivation may have won.
	user, err = am.Store.GetUserByUserID(ctx, store.LockingStrengthShare, userID)
	if err != nil {
		return nil, err
	}
	if user.LocalCredentialState != types.LocalCredentialActive {
		return nil, status.Errorf(status.BadRequest, "the password of a %s local user cannot be set", user.LocalCredentialState)
	}
	uid, err := localDexUserID(user)
	if err != nil {
		return nil, err
	}
	entry, err := am.findOwnedCredential(ctx, user.SCIMUserName, uid)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, status.Errorf(status.Internal, "local user %s has no Dex credential", user.Id)
	}
	if err := am.localCreds.UpdatePassword(ctx, user.SCIMUserName, hash); err != nil {
		return nil, status.Errorf(status.Internal, "update Dex password: %v", err)
	}
	if err := am.localCreds.RevokeAllRefreshTokens(ctx, user.Id); err != nil {
		return nil, status.Errorf(status.Internal, "revoke refresh tokens: %v", err)
	}

	am.StoreEvent(ctx, callerID, user.Id, accountID, activity.UserLocalPasswordReset, map[string]any{"scim": true})
	return user, nil
}

// terminateLocalUser runs the terminal transition of a local user (ADR-0024
// D7) to deactivated (SCIM active=false) or tombstoned (DELETE): mark the
// user blocked with cleanup pending, then expire and disconnect its peers,
// delete its PATs, revoke its refresh tokens, remove its Dex credential
// (after an ownership check), and clear the pending flag. Access is cut by
// the first step; a failure in a later one returns an error with cleanup
// still pending, and calling it again resumes. Re-sending a transition
// that already converged is a no-op, except DELETE on a cleaned tombstone,
// which is not found.
func (am *DefaultAccountManager) terminateLocalUser(ctx context.Context, accountID, callerID, userID string,
	to types.LocalCredentialState) error {
	if am.localCreds == nil {
		return status.Errorf(status.BadRequest, "Dex is not configured on this deployment")
	}
	user, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthShare, userID)
	if err != nil {
		return err
	}
	if user.AccountID != accountID {
		return status.NewUserNotFoundError(userID)
	}

	unlock, err := am.lockLocalIdentity(ctx, user.SCIMUserName)
	if err != nil {
		return status.Errorf(status.Internal, "%v", err)
	}
	defer unlock()

	user, err = am.Store.GetUserByUserID(ctx, store.LockingStrengthShare, userID)
	if err != nil {
		return err
	}
	switch {
	case user.LocalCredentialState == types.LocalCredentialPending:
		return status.NewUserNotFoundError(userID)
	case user.LocalCredentialState == types.LocalCredentialTombstoned && (to != types.LocalCredentialTombstoned || !user.LocalCleanupPending):
		return status.NewUserNotFoundError(userID)
	case user.LocalCredentialState == to && !user.LocalCleanupPending:
		return nil
	case user.LocalCredentialState != to:
		if err := am.Store.MarkLocalUserTerminal(ctx, accountID, userID, to); err != nil {
			if sErr, ok := status.FromError(err); ok && sErr.Type() == status.PermissionDenied {
				return err
			}
			return status.Errorf(status.Internal, "mark local user %s: %v", to, err)
		}
		event := activity.UserBlocked
		if to == types.LocalCredentialTombstoned {
			event = activity.UserDeleted
		}
		am.StoreEvent(ctx, callerID, userID, accountID, event, map[string]any{"scim": true, "local": true})
		if user, err = am.Store.GetUserByUserID(ctx, store.LockingStrengthShare, userID); err != nil {
			return err
		}
	}
	return am.cleanupTerminatedLocalUser(ctx, accountID, user)
}

// cleanupTerminatedLocalUser runs the revocation steps of a terminal
// transition and records their convergence: peers, PATs, refresh tokens,
// the Dex credential and, for a tombstone, group memberships. Each step is
// idempotent.
func (am *DefaultAccountManager) cleanupTerminatedLocalUser(ctx context.Context, accountID string, user *types.User) error {
	if err := am.expireBlockedUserPeers(ctx, accountID, user); err != nil {
		return status.Errorf(status.Internal, "expire peers: %v", err)
	}
	pats, err := am.Store.GetUserPATs(ctx, store.LockingStrengthShare, user.Id)
	if err != nil {
		return status.Errorf(status.Internal, "list PATs: %v", err)
	}
	for _, pat := range pats {
		if err := am.Store.DeletePAT(ctx, store.LockingStrengthUpdate, user.Id, pat.ID); err != nil {
			return status.Errorf(status.Internal, "delete PAT: %v", err)
		}
	}
	if err := am.localCreds.RevokeAllRefreshTokens(ctx, user.Id); err != nil {
		return status.Errorf(status.Internal, "revoke refresh tokens: %v", err)
	}
	uid, err := localDexUserID(user)
	if err != nil {
		return err
	}
	entry, err := am.findOwnedCredential(ctx, user.SCIMUserName, uid)
	if err != nil {
		return err
	}
	if entry != nil {
		if err := am.localCreds.DeletePassword(ctx, user.SCIMUserName); err != nil && !errors.Is(err, dex_proxy.ErrNotFound) {
			return status.Errorf(status.Internal, "delete Dex password: %v", err)
		}
	}
	// A tombstone is not a SCIM resource, so it must not stay a group
	// member (GET /Groups would reference a /Users/{id} that is a 404).
	if user.LocalCredentialState == types.LocalCredentialTombstoned && len(user.AutoGroups) > 0 {
		user.AutoGroups = []string{}
		if err := am.Store.SaveUser(ctx, store.LockingStrengthUpdate, user); err != nil {
			return status.Errorf(status.Internal, "remove group memberships: %v", err)
		}
	}
	if err := am.Store.ClearLocalUserCleanup(ctx, accountID, user.Id, user.LocalCredentialState); err != nil {
		return status.Errorf(status.Internal, "clear local user cleanup: %v", err)
	}
	return nil
}

// findOwnedCredential returns the Dex entry for email if it belongs to
// uid, nil if there is none, and an Internal error if an entry exists
// under another owner: Dex writes by email only, so a mismatch means
// out-of-band tampering and must not be "fixed" automatically (ADR-0024 D5).
func (am *DefaultAccountManager) findOwnedCredential(ctx context.Context, email, uid string) (*dex_proxy.PasswordEntry, error) {
	entries, err := am.localCreds.ListPasswords(ctx)
	if err != nil {
		return nil, status.Errorf(status.Internal, "list Dex passwords: %v", err)
	}
	for i := range entries {
		if !strings.EqualFold(entries[i].Email, email) {
			continue
		}
		if entries[i].UserID != uid {
			return nil, status.Errorf(status.Internal, "the Dex credential for this email is owned by someone else")
		}
		return &entries[i], nil
	}
	return nil, nil //nolint:nilnil // nil entry, nil error: no credential exists, which callers handle.
}

// localDexUserID recovers the Dex user_id from a local user's openZro ID.
func localDexUserID(user *types.User) (string, error) {
	uid, conn, ok := jwt.DecodeDexSubject(user.Id)
	if !ok || conn != dexLocalConnector {
		return "", status.Errorf(status.Internal, "local user %s has no Dex identity", user.Id)
	}
	return uid, nil
}

// getSCIMVisibleUser loads a user of the account that SCIM can address.
func (am *DefaultAccountManager) getSCIMVisibleUser(ctx context.Context, accountID, userID string) (*types.User, error) {
	user, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthShare, userID)
	if err != nil {
		return nil, err
	}
	if user.AccountID != accountID || isHiddenFromSCIM(user) {
		return nil, status.NewUserNotFoundError(userID)
	}
	return user, nil
}

// representationUnchanged reports whether a SCIM PUT body matches the
// user's current representation apart from the password.
func representationUnchanged(user *types.User, in *account.SCIMUserInput) bool {
	return strings.EqualFold(in.UserName, user.SCIMUserName) &&
		in.DisplayName == user.SCIMDisplayName &&
		in.ExternalID == user.SCIMExternalID &&
		in.Active == !user.Blocked &&
		(in.AutoGroups == nil || sameStringSet(in.AutoGroups, user.AutoGroups))
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, v := range a {
		seen[v]++
	}
	for _, v := range b {
		if seen[v] == 0 {
			return false
		}
		seen[v]--
	}
	return true
}

// deleteLocalUserViaREST applies the REST delete's authorization to a local
// user and then tombstones it.
func (am *DefaultAccountManager) deleteLocalUserViaREST(ctx context.Context, accountID, initiatorUserID, targetUserID string) error {
	allowed, err := am.permissionsManager.ValidateUserPermissions(ctx, accountID, initiatorUserID, modules.Users, operations.Delete)
	if err != nil {
		return status.NewPermissionValidationError(err)
	}
	if !allowed {
		return status.NewPermissionDeniedError()
	}
	// Unlike other integration-issued users, a local user is not restricted
	// to service-user deletes: no external IdP re-provisions it, and the
	// console manages it (ADR-0026 D1).
	target, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthShare, targetUserID)
	if err != nil {
		return err
	}
	if err := checkLocalTermination(initiatorUserID, target); err != nil {
		return err
	}
	return am.terminateLocalUser(ctx, accountID, initiatorUserID, targetUserID, types.LocalCredentialTombstoned)
}

// DeactivateLocalUser permanently deactivates a local user from the console
// (ADR-0026): the same terminal saga as SCIM active=false. It is called
// without the account write lock, per the lock discipline above.
func (am *DefaultAccountManager) DeactivateLocalUser(ctx context.Context, accountID, callerID, userID string) (*types.User, error) {
	if err := am.requireSCIMPermission(ctx, accountID, callerID, operations.Update); err != nil {
		return nil, err
	}
	user, err := am.getSCIMVisibleUser(ctx, accountID, userID)
	if err != nil {
		return nil, err
	}
	if user.LocalCredentialState == types.LocalCredentialNone {
		return nil, status.Errorf(status.BadRequest, "only local users can be deactivated this way")
	}
	if err := checkLocalTermination(callerID, user); err != nil {
		return nil, err
	}
	if err := am.terminateLocalUser(ctx, accountID, callerID, userID, types.LocalCredentialDeactivated); err != nil {
		return nil, err
	}
	return am.Store.GetUserByUserID(ctx, store.LockingStrengthShare, userID)
}

// checkLocalTermination refuses a console deactivation or delete of the
// caller's own account, which cannot be undone. The owner rule is not
// checked here: MarkLocalUserTerminal enforces it atomically on every
// channel, which a read before the saga could not (ADR-0026 D1).
func checkLocalTermination(initiatorUserID string, target *types.User) error {
	if initiatorUserID == target.Id {
		return status.Errorf(status.PermissionDenied, "users can't deactivate or delete themselves")
	}
	return nil
}

// LocalUserCreationEnabled reports whether this deployment can create local
// users: Dex is wired and the operator opted in (ADR-0024 D10).
func (am *DefaultAccountManager) LocalUserCreationEnabled() bool {
	return am.localCreds != nil && am.localUserCreationEnabled
}
