package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
)

// #205 prerequisites: until the deactivation/delete saga lands, the legacy
// SCIM paths refuse to block a local user (which would skip the Dex and
// session cleanup) or rename it (Dex cannot re-key a credential), with an
// explicit message. Other edits keep working.
func TestSCIMLegacyPathsGateLocalUsers(t *testing.T) {
	m, accountID, _ := setupLocalUsers(t)
	ctx := context.Background()
	user, err := m.SCIMCreateLocalUser(ctx, accountID, userID, localInput("ana@example.com"), goodPassword)
	require.NoError(t, err)

	requireStatusType(t, m.SCIMDeactivateUser(ctx, accountID, userID, user.Id), status.BadRequest)

	_, err = m.SCIMReplaceUser(ctx, accountID, userID, user.Id,
		account.SCIMUserInput{UserName: "ana@example.com", Active: false})
	requireStatusType(t, err, status.BadRequest)
	_, err = m.SCIMReplaceUser(ctx, accountID, userID, user.Id,
		account.SCIMUserInput{UserName: "ana2@example.com", Active: true})
	requireStatusType(t, err, status.BadRequest)

	inactive := false
	_, err = m.SCIMPatchUser(ctx, accountID, userID, user.Id, account.SCIMUserPatch{Active: &inactive})
	requireStatusType(t, err, status.BadRequest)
	renamed := "ana2@example.com"
	_, err = m.SCIMPatchUser(ctx, accountID, userID, user.Id, account.SCIMUserPatch{UserName: &renamed})
	requireStatusType(t, err, status.BadRequest)

	stored, err := m.Store.GetUserByUserID(ctx, store.LockingStrengthShare, user.Id)
	require.NoError(t, err)
	require.False(t, stored.Blocked)
	require.Equal(t, "ana@example.com", stored.SCIMUserName)

	// A display-name change and a case-only re-send of userName still work.
	display := "Ana B."
	_, err = m.SCIMPatchUser(ctx, accountID, userID, user.Id, account.SCIMUserPatch{DisplayName: &display})
	require.NoError(t, err)
	_, err = m.SCIMReplaceUser(ctx, accountID, userID, user.Id,
		account.SCIMUserInput{UserName: "ANA@example.com", DisplayName: "Ana B.", Active: true})
	require.NoError(t, err)
}

// ADR-0024 D3: a pending reservation is not a SCIM resource yet.
func TestSCIMHidesPendingLocalUsers(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	creds.listErr = errors.New("dex down")
	_, err := m.SCIMCreateLocalUser(ctx, accountID, userID, localInput("ana@example.com"), goodPassword)
	require.Error(t, err)
	pending, err := m.findSCIMUserByUserName(ctx, accountID, "ana@example.com")
	require.NoError(t, err)
	require.NotNil(t, pending)

	_, err = m.SCIMGetUser(ctx, accountID, userID, pending.Id)
	require.ErrorIs(t, err, ErrSCIMUserNotFound)
	users, total, err := m.SCIMListUsers(ctx, accountID, userID, "", 1, 100)
	require.NoError(t, err)
	for _, u := range users {
		require.NotEqual(t, pending.Id, u.Id)
	}
	filtered, _, err := m.SCIMListUsers(ctx, accountID, userID, "ana@example.com", 1, 100)
	require.NoError(t, err)
	require.Empty(t, filtered)
	require.Equal(t, len(users), total)
}
