package server

import (
	"context"
	"testing"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #217 review: an invalid PUT/PATCH must be rejected before the terminal
// saga starts. v1 cannot re-activate, so a request that returns 400 must
// leave the user exactly as it was.
func TestSCIMDeactivate_InvalidRequestLeavesUserUntouched(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	peerID, _ := connectPeer(t, m, u)
	pat, err := types.CreateNewPAT("ana-token", 30, u.Id, userID)
	require.NoError(t, err)
	require.NoError(t, m.Store.SavePAT(ctx, store.LockingStrengthUpdate, &pat.PersonalAccessToken))

	inactive := false
	renamed := "ana2@example.com"
	_, err = m.SCIMPatchUser(ctx, accountID, userID, u.Id, account.SCIMUserPatch{Active: &inactive, UserName: &renamed})
	requireStatusType(t, err, status.BadRequest)
	_, err = m.SCIMReplaceUser(ctx, accountID, userID, u.Id, account.SCIMUserInput{UserName: renamed, Active: false})
	requireStatusType(t, err, status.BadRequest)
	_, err = m.SCIMReplaceUser(ctx, accountID, userID, u.Id, account.SCIMUserInput{UserName: "  ", Active: false})
	require.Error(t, err)

	stored, err := m.Store.GetUserByUserID(ctx, store.LockingStrengthShare, u.Id)
	require.NoError(t, err)
	assert.Equal(t, types.LocalCredentialActive, stored.LocalCredentialState)
	assert.False(t, stored.Blocked)
	assert.NotNil(t, creds.entry("ana@example.com"), "the credential must survive a rejected request")
	pats, err := m.Store.GetUserPATs(ctx, store.LockingStrengthShare, u.Id)
	require.NoError(t, err)
	assert.Len(t, pats, 1)
	peer, err := m.Store.GetPeerByID(ctx, store.LockingStrengthShare, accountID, peerID)
	require.NoError(t, err)
	assert.False(t, peer.Status.LoginExpired)
	assert.Zero(t, creds.revokedFor(u.Id))
}
