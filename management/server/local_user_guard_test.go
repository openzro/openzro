package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// ADR-0024 D3: the REST user update (dashboard) must not unblock a
// deactivated local user. It writes through SaveUsers, so this exercises
// the store guard on the path the dashboard actually uses.
func TestSaveOrAddUser_CannotReactivateLocalUser(t *testing.T) {
	manager, err := createManager(t)
	require.NoError(t, err)
	ctx := context.Background()
	accountID, err := manager.GetAccountIDByUserID(ctx, userID, "")
	require.NoError(t, err)

	local := types.NewRegularUser("local-ana")
	local.AccountID = accountID
	local.SCIMUserName = "ana@example.com"
	require.NoError(t, manager.Store.CreatePendingLocalUser(ctx, local))
	require.NoError(t, manager.Store.FinalizeLocalUser(ctx, accountID, local.Id))
	require.NoError(t, manager.Store.MarkLocalUserTerminal(ctx, accountID, local.Id, types.LocalCredentialDeactivated))

	update, err := manager.Store.GetUserByUserID(ctx, store.LockingStrengthShare, local.Id)
	require.NoError(t, err)
	update = update.Copy()
	update.Blocked = false

	_, err = manager.SaveOrAddUser(ctx, accountID, userID, update, false)
	require.Error(t, err)
	sErr, ok := status.FromError(err)
	require.True(t, ok, "expected a status error, got %v", err)
	assert.Equal(t, status.BadRequest, sErr.Type())

	stored, err := manager.Store.GetUserByUserID(ctx, store.LockingStrengthShare, local.Id)
	require.NoError(t, err)
	assert.True(t, stored.Blocked)
	assert.Equal(t, types.LocalCredentialDeactivated, stored.LocalCredentialState)
}
