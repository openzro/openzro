package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/account"
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
