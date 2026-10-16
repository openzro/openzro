package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/openzro/openzro/management/server/account"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// setupSCIMUserWithPeer provisions a SCIM user that owns one connected peer
// and returns the user, the peer ID and the peer's update channel.
func setupSCIMUserWithPeer(t *testing.T) (*DefaultAccountManager, string, *types.User, string, chan *UpdateMessage) {
	t.Helper()
	manager, err := createManager(t)
	require.NoError(t, err)
	accountID, err := manager.GetAccountIDByUserID(context.Background(), userID, "")
	require.NoError(t, err)

	user, err := manager.SCIMCreateUser(context.Background(), accountID, userID,
		account.SCIMUserInput{UserName: "ana@example.com", Active: true})
	require.NoError(t, err)

	key, err := wgtypes.GeneratePrivateKey()
	require.NoError(t, err)
	peer, _, _, err := manager.AddPeer(context.Background(), "", user.Id, &nbpeer.Peer{
		Key:  key.PublicKey().String(),
		Meta: nbpeer.PeerSystemMeta{Hostname: "ana-laptop"},
	})
	require.NoError(t, err)

	ch := manager.peersUpdateManager.CreateChannel(context.Background(), peer.ID)
	return manager, accountID, user, peer.ID, ch
}

// requirePeerCutOff asserts the peer is login-expired and its update channel
// was closed, i.e. the management service disconnected it.
func requirePeerCutOff(t *testing.T, manager *DefaultAccountManager, accountID, peerID string, ch chan *UpdateMessage) {
	t.Helper()
	peer, err := manager.Store.GetPeerByID(context.Background(), store.LockingStrengthShare, accountID, peerID)
	require.NoError(t, err)
	assert.True(t, peer.Status.LoginExpired, "blocked user's peer must be login-expired")

	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-ch:
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("blocked user's peer channel was not closed")
		}
	}
}

// Regression for #211: every SCIM path that blocks a user must cut off the
// user's already-connected peers, as the REST block path does. Before the
// fix the SCIM paths only persisted Blocked=true, which gates new activity
// but leaves connected peers on the network until they reconnect.
func TestSCIMBlock_ExpiresConnectedPeers(t *testing.T) {
	inactive := false
	tests := []struct {
		name  string
		block func(m *DefaultAccountManager, accountID string, u *types.User) error
	}{
		{
			name: "DELETE",
			block: func(m *DefaultAccountManager, accountID string, u *types.User) error {
				return m.SCIMDeactivateUser(context.Background(), accountID, userID, u.Id)
			},
		},
		{
			name: "PUT active false",
			block: func(m *DefaultAccountManager, accountID string, u *types.User) error {
				_, err := m.SCIMReplaceUser(context.Background(), accountID, userID, u.Id,
					account.SCIMUserInput{UserName: u.SCIMUserName, Active: false})
				return err
			},
		},
		{
			name: "PATCH active false",
			block: func(m *DefaultAccountManager, accountID string, u *types.User) error {
				_, err := m.SCIMPatchUser(context.Background(), accountID, userID, u.Id,
					account.SCIMUserPatch{Active: &inactive})
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manager, accountID, user, peerID, ch := setupSCIMUserWithPeer(t)
			require.NoError(t, tc.block(manager, accountID, user))
			requirePeerCutOff(t, manager, accountID, peerID, ch)
		})
	}
}

// #211: a user blocked before the fix (or by any path that skipped peer
// expiry) still has live peers. Re-sending DELETE must converge and cut them
// off instead of returning early on "already blocked".
func TestSCIMBlock_AlreadyBlockedUserStillExpiresPeers(t *testing.T) {
	manager, accountID, user, peerID, ch := setupSCIMUserWithPeer(t)

	stored, err := manager.Store.GetUserByUserID(context.Background(), store.LockingStrengthShare, user.Id)
	require.NoError(t, err)
	stored.Blocked = true
	require.NoError(t, manager.Store.SaveUser(context.Background(), store.LockingStrengthUpdate, stored))

	require.NoError(t, manager.SCIMDeactivateUser(context.Background(), accountID, userID, user.Id))
	requirePeerCutOff(t, manager, accountID, peerID, ch)
}

// Transitions that leave the user active must not touch their peers.
func TestSCIMUpdate_ActiveUserKeepsPeers(t *testing.T) {
	manager, accountID, user, peerID, _ := setupSCIMUserWithPeer(t)

	display := "Ana"
	_, err := manager.SCIMPatchUser(context.Background(), accountID, userID, user.Id,
		account.SCIMUserPatch{DisplayName: &display})
	require.NoError(t, err)

	peer, err := manager.Store.GetPeerByID(context.Background(), store.LockingStrengthShare, accountID, peerID)
	require.NoError(t, err)
	assert.False(t, peer.Status.LoginExpired)
}
