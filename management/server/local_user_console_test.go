package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/idp"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// ADR-0026 D1: the console deactivation is the same terminal saga as SCIM.
func TestDeactivateLocalUser(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")

	got, err := m.DeactivateLocalUser(ctx, accountID, userID, u.Id)
	require.NoError(t, err)
	assert.Equal(t, types.LocalCredentialDeactivated, got.LocalCredentialState)
	assert.True(t, got.Blocked)
	assert.False(t, got.LocalCleanupPending)
	assert.Nil(t, creds.entry("ana@example.com"))

	plain, err := m.SCIMCreateUser(ctx, accountID, userID, account.SCIMUserInput{UserName: "bea@example.com", Active: true})
	require.NoError(t, err)
	_, err = m.DeactivateLocalUser(ctx, accountID, userID, plain.Id)
	requireStatusType(t, err, status.BadRequest)

	stored, err := m.Store.GetUserByUserID(ctx, store.LockingStrengthShare, plain.Id)
	require.NoError(t, err)
	assert.False(t, stored.Blocked, "a non-local user is not touched")
}

func TestLocalUserCreationEnabled(t *testing.T) {
	m, _, creds := setupLocalUsers(t)
	assert.True(t, m.LocalUserCreationEnabled())
	m.localUserCreationEnabled = false
	assert.False(t, m.LocalUserCreationEnabled())

	bare, err := createManager(t)
	require.NoError(t, err)
	assert.False(t, bare.LocalUserCreationEnabled(), "off by default")
	bare.EnableLocalUserCreation()
	assert.False(t, bare.LocalUserCreationEnabled(), "creation needs Dex credentials too")
	bare.UseLocalCredentials(creds)
	assert.True(t, bare.LocalUserCreationEnabled())
}

// ADR-0026 D2: the REST user list learns which users are local.
func TestGetUsersFromAccount_ExposesLocalState(t *testing.T) {
	m, accountID, _ := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")

	infos, err := m.GetUsersFromAccount(ctx, accountID, userID)
	require.NoError(t, err)
	require.Contains(t, infos, u.Id)
	assert.Equal(t, string(types.LocalCredentialActive), infos[u.Id].LocalCredentialState)
	assert.Empty(t, infos[userID].LocalCredentialState)
}

// setLocalRole gives a local user a role the way an ownership transfer or a
// role update would, for the termination guards below.
func setLocalRole(t *testing.T, m *DefaultAccountManager, u *types.User, role types.UserRole) {
	t.Helper()
	stored, err := m.Store.GetUserByUserID(context.Background(), store.LockingStrengthUpdate, u.Id)
	require.NoError(t, err)
	stored.Role = role
	require.NoError(t, m.Store.SaveUser(context.Background(), store.LockingStrengthUpdate, stored))
}

func requireStillActive(t *testing.T, m *DefaultAccountManager, creds *fakeLocalCredentials, u *types.User) {
	t.Helper()
	stored, err := m.Store.GetUserByUserID(context.Background(), store.LockingStrengthShare, u.Id)
	require.NoError(t, err)
	assert.Equal(t, types.LocalCredentialActive, stored.LocalCredentialState)
	assert.NotNil(t, creds.entry(u.SCIMUserName), "the credential is untouched")
}

// The console's irreversible paths keep the protections of the regular
// user paths: nobody terminates their own account, and an owner is never
// terminated (ownership has to move first).
func TestConsoleTermination_RefusesSelfAndOwner(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()

	admin := createLocal(t, m, accountID, "admin@example.com")
	setLocalRole(t, m, admin, types.UserRoleAdmin)
	_, err := m.DeactivateLocalUser(ctx, accountID, admin.Id, admin.Id)
	requireStatusType(t, err, status.PermissionDenied)
	requireStillActive(t, m, creds, admin)

	owner := createLocal(t, m, accountID, "owner@example.com")
	setLocalRole(t, m, owner, types.UserRoleOwner)
	for name, caller := range map[string]string{"owner": userID, "admin": admin.Id} {
		_, err = m.DeactivateLocalUser(ctx, accountID, caller, owner.Id)
		requireStatusType(t, err, status.PermissionDenied)
		require.Error(t, m.DeleteUser(ctx, accountID, caller, owner.Id), name)
		requireStillActive(t, m, creds, owner)
	}

	svc, err := m.CreateUser(ctx, accountID, userID, &types.UserInfo{
		Name: "iga", Role: string(types.UserRoleAdmin), IsServiceUser: true, Issued: types.UserIssuedAPI,
	})
	require.NoError(t, err)
	requireStatusType(t, m.DeleteUser(ctx, accountID, svc.ID, owner.Id), status.PermissionDenied)
	requireStillActive(t, m, creds, owner)
}

// ADR-0026 D1: an administrator deletes a local user from the console. The
// regular rule that only a service user deletes integration-issued users
// does not apply: no external IdP re-provisions a local user.
func TestRESTDeleteLocalUser_ByHumanAdmin(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")

	require.NoError(t, m.DeleteUser(ctx, accountID, userID, u.Id))
	stored, err := m.Store.GetUserByUserID(ctx, store.LockingStrengthShare, u.Id)
	require.NoError(t, err)
	assert.Equal(t, types.LocalCredentialTombstoned, stored.LocalCredentialState)
	assert.Nil(t, creds.entry("ana@example.com"))
}

// recordingUserCache answers every external-cache lookup and records the
// keys, to show which users the list enrichment touched.
type recordingUserCache struct {
	mu   sync.Mutex
	keys []string
}

func (c *recordingUserCache) Get(_ context.Context, key string) (*idp.UserData, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys = append(c.keys, key)
	return &idp.UserData{}, nil
}

func (c *recordingUserCache) Set(context.Context, string, *idp.UserData, time.Duration) error {
	return nil
}

func (c *recordingUserCache) Delete(context.Context, string) error { return nil }

// ADR-0026 D2: pending reservations and tombstones are dropped before the
// list is enriched, so GET /users costs what the visible users cost, not
// the account's permanent delete history.
func TestGetUsersFromAccount_HidesLocalHistoryBeforeEnrichment(t *testing.T) {
	m, accountID, _ := setupLocalUsers(t)
	ctx := context.Background()
	active := createLocal(t, m, accountID, "ana@example.com")
	gone := createLocal(t, m, accountID, "bea@example.com")
	require.NoError(t, m.DeleteUser(ctx, accountID, userID, gone.Id))
	pending := &types.User{Id: "pending-local", AccountID: accountID, Role: types.UserRoleUser,
		Issued: types.UserIssuedIntegration, SCIMUserName: "cid@example.com", Blocked: true,
		LocalCredentialState: types.LocalCredentialPending, AutoGroups: []string{}}
	require.NoError(t, m.Store.CreatePendingLocalUser(ctx, pending))

	cache := &recordingUserCache{}
	m.idpManager = &idp.MockIDP{}
	m.externalCacheManager = cache

	infos, err := m.GetUsersFromAccount(ctx, accountID, userID)
	require.NoError(t, err)
	assert.Contains(t, infos, active.Id)
	assert.NotContains(t, infos, gone.Id)
	assert.NotContains(t, infos, pending.Id)
	for _, key := range cache.keys {
		assert.NotContains(t, key, gone.Id, "a tombstone must not reach the IdP lookup")
		assert.NotContains(t, key, pending.Id, "a reservation must not reach the IdP lookup")
	}
	assert.NotEmpty(t, cache.keys, "the visible local user is still enriched")
}
