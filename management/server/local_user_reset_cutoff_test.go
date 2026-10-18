package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

func storedUser(t *testing.T, m *DefaultAccountManager, id string) *types.User {
	t.Helper()
	u, err := m.Store.GetUserByUserID(context.Background(), store.LockingStrengthShare, id)
	require.NoError(t, err)
	return u
}

// ADR-0027 D1: once a reset returns, every JWT issued up to it is revoked
// and the user's SSO peers are login-expired and disconnected. The user is
// not blocked: it signs in again with the new password.
func TestSCIMResetLocalPassword_CutsExistingAccess(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	peerID, ch := connectPeer(t, m, u)
	require.Nil(t, storedUser(t, m, u.Id).CredentialsValidAfter, "a new local user has no cutoff")

	issuedBefore := time.Now()
	_, err := m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	require.NoError(t, err)

	stored := storedUser(t, m, u.Id)
	require.NotNil(t, stored.CredentialsValidAfter, "a reset sets the credentials cutoff")
	assert.True(t, stored.JWTRevoked(issuedBefore), "a JWT issued before the reset is revoked")
	assert.False(t, stored.JWTRevoked(time.Now().Add(2*time.Second)), "a JWT issued after the reset is accepted")
	assert.False(t, stored.Blocked, "a reset does not block the user")
	assert.Equal(t, types.LocalCredentialActive, stored.LocalCredentialState)
	assert.Equal(t, 1, creds.revokedFor(u.Id))
	requirePeerCutOff(t, m, accountID, peerID, ch)
}

// ADR-0027 D4: the refresh tokens are revoked before the cutoff is set, so
// no refresh can mint a token that postdates the cutoff. A failure leaves
// the cutoff unset and a retry converges.
func TestSCIMResetLocalPassword_RetryConvergesAfterFailedRevoke(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	peerID, ch := connectPeer(t, m, u)

	creds.revokeErr = errors.New("dex down")
	_, err := m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	requireStatusType(t, err, status.Internal)
	assert.Nil(t, storedUser(t, m, u.Id).CredentialsValidAfter, "no cutoff before the refresh tokens are revoked")

	creds.revokeErr = nil
	_, err = m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	require.NoError(t, err)
	assert.NotNil(t, storedUser(t, m, u.Id).CredentialsValidAfter)
	requirePeerCutOff(t, m, accountID, peerID, ch)

	// A further reset is idempotent: the cutoff moves forward, peers stay
	// expired, and the already-expired peers are disconnected again.
	_, err = m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	require.NoError(t, err)
	peer, err := m.Store.GetPeerByID(ctx, store.LockingStrengthShare, accountID, peerID)
	require.NoError(t, err)
	assert.True(t, peer.Status.LoginExpired)
}
