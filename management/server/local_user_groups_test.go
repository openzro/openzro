package server

import (
	"context"
	"errors"
	"testing"

	"github.com/openzro/openzro/management/server/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #217 review: a tombstoned or pending local user is not a SCIM resource,
// so it must not be listed as a group member nor be added to a group.
func TestSCIMGroups_HideLocalUsersThatAreNotResources(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	group, err := m.SCIMCreateGroup(ctx, accountID, userID, "engineering", []string{u.Id})
	require.NoError(t, err)

	require.NoError(t, m.SCIMDeactivateUser(ctx, accountID, userID, u.Id))
	_, members, err := m.SCIMGetGroup(ctx, accountID, userID, group.ID)
	require.NoError(t, err)
	assert.NotContains(t, members, u.Id, "a deleted user must not stay a group member")
	stored, err := m.Store.GetUserByUserID(ctx, store.LockingStrengthShare, u.Id)
	require.NoError(t, err)
	assert.Empty(t, stored.AutoGroups, "DELETE removes group memberships")
	require.Error(t, m.SCIMAddGroupMember(ctx, accountID, userID, group.ID, u.Id), "a tombstone cannot be added")

	// A pending reservation (interrupted create) is hidden the same way.
	creds.listErr = errors.New("dex down")
	in := localInput("bea@example.com")
	in.AutoGroups = []string{group.ID}
	_, err = m.SCIMCreateLocalUser(ctx, accountID, userID, in, goodPassword)
	require.Error(t, err)
	pending, err := m.findSCIMUserByUserName(ctx, accountID, "bea@example.com")
	require.NoError(t, err)
	require.NotNil(t, pending)
	_, members, err = m.SCIMGetGroup(ctx, accountID, userID, group.ID)
	require.NoError(t, err)
	assert.NotContains(t, members, pending.Id)
	require.Error(t, m.SCIMAddGroupMember(ctx, accountID, userID, group.ID, pending.Id))
}
