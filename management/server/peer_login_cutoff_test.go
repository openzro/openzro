package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// ADR-0027 D5: after a reset, an expired peer cannot log back in with a JWT
// issued before it, and a JWT issued after it (new password) logs it in.
func TestLoginPeer_RejectsJWTIssuedBeforeReset(t *testing.T) {
	m, accountID, _ := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	peerID, _ := connectPeer(t, m, u)
	peer, err := m.Store.GetPeerByID(ctx, store.LockingStrengthShare, accountID, peerID)
	require.NoError(t, err)

	oldJWT := time.Now()
	_, err = m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	require.NoError(t, err)

	login := types.PeerLogin{
		WireGuardPubKey: peer.Key,
		Meta:            peer.Meta,
		UserID:          u.Id,
		JWTIssuedAt:     oldJWT,
	}
	_, _, _, err = m.LoginPeer(ctx, login)
	requireStatusType(t, err, status.Unauthorized)
	peer, err = m.Store.GetPeerByID(ctx, store.LockingStrengthShare, accountID, peerID)
	require.NoError(t, err)
	assert.True(t, peer.Status.LoginExpired, "an old JWT must not re-login the expired peer")

	// A token without iat fails closed for a user with a cutoff.
	login.JWTIssuedAt = time.Time{}
	_, _, _, err = m.LoginPeer(ctx, login)
	requireStatusType(t, err, status.Unauthorized)

	login.JWTIssuedAt = time.Now().Add(2 * time.Second)
	_, _, _, err = m.LoginPeer(ctx, login)
	require.NoError(t, err)
	peer, err = m.Store.GetPeerByID(ctx, store.LockingStrengthShare, accountID, peerID)
	require.NoError(t, err)
	assert.False(t, peer.Status.LoginExpired, "a JWT issued after the reset logs the peer in")
}

// ADR-0027 D5: registering a new device needs a JWT issued after the reset
// too.
func TestLoginPeer_NewPeerRejectsJWTIssuedBeforeReset(t *testing.T) {
	m, accountID, _ := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")

	oldJWT := time.Now()
	_, err := m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	require.NoError(t, err)

	key, err := wgtypes.GeneratePrivateKey()
	require.NoError(t, err)
	login := types.PeerLogin{
		WireGuardPubKey: key.PublicKey().String(),
		Meta:            nbpeer.PeerSystemMeta{Hostname: "phone"},
		UserID:          u.Id,
		JWTIssuedAt:     oldJWT,
	}
	_, _, _, err = m.LoginPeer(ctx, login)
	requireStatusType(t, err, status.Unauthorized)
	_, err = m.Store.GetPeerByPeerPubKey(ctx, store.LockingStrengthShare, login.WireGuardPubKey)
	requireStatusType(t, err, status.NotFound)

	login.JWTIssuedAt = time.Now().Add(2 * time.Second)
	_, _, _, err = m.LoginPeer(ctx, login)
	require.NoError(t, err)
}

// Users without a cutoff keep logging peers in with any JWT, including one
// without iat: the check changes nothing for them.
func TestLoginPeer_NoCutoffUnchanged(t *testing.T) {
	m, accountID, _ := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")

	key, err := wgtypes.GeneratePrivateKey()
	require.NoError(t, err)
	_, _, _, err = m.LoginPeer(ctx, types.PeerLogin{
		WireGuardPubKey: key.PublicKey().String(),
		Meta:            nbpeer.PeerSystemMeta{Hostname: "phone"},
		UserID:          u.Id,
	})
	require.NoError(t, err)
}
