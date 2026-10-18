package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/store"
)

// ADR-0027 D6: every reset schedules its second pass, one Dex
// authorization-code lifetime (plus margin) later.
func TestLocalPasswordReset_SchedulesRecheck(t *testing.T) {
	m, accountID, _ := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")

	before := time.Now().Truncate(time.Second)
	_, err := m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	require.NoError(t, err)
	after := time.Now()

	at := storedUser(t, m, u.Id).CredentialsRecheckAt
	require.NotNil(t, at)
	assert.WithinRange(t, at.UTC(), before.Add(credentialsRecheckDelay).UTC(), after.Add(credentialsRecheckDelay).UTC())
}

// ADR-0027 D6, #220 review: a login that passed the old password before the
// reset can redeem its Dex authorization code afterwards and get tokens
// newer than the first cutoff, including a fresh refresh token. The second
// pass cuts them: it revokes refresh tokens again and moves the cutoff to
// the moment it runs, then expires the user's peers. The user stays active.
func TestCredentialsRecheck_CutsSessionsFromLoginsInFlight(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	peerID, _ := connectPeer(t, m, u)

	_, err := m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	require.NoError(t, err)
	first := storedUser(t, m, u.Id)
	require.NotNil(t, first.CredentialsRecheckAt)
	recheckAt := *first.CredentialsRecheckAt

	// The in-flight login's token postdates the first cutoff, and the device
	// logged back in with it.
	inFlight := first.CredentialsValidAfter.Add(5 * time.Second)
	require.False(t, first.JWTRevoked(inFlight), "the first pass cannot tell it apart from a new-password login")
	peer, err := m.Store.GetPeerByID(ctx, store.LockingStrengthShare, accountID, peerID)
	require.NoError(t, err)
	relogged := peer.Status.Copy()
	relogged.LoginExpired = false
	require.NoError(t, m.Store.SavePeerStatus(ctx, store.LockingStrengthUpdate, accountID, peerID, *relogged))
	revokedBefore := len(creds.revoked)

	m.runDueCredentialsRechecks(ctx, recheckAt.Add(-time.Second))
	assert.NotNil(t, storedUser(t, m, u.Id).CredentialsRecheckAt, "not due yet")
	assert.Len(t, creds.revoked, revokedBefore)

	pass := recheckAt.Add(40 * time.Second)
	m.runDueCredentialsRechecks(ctx, pass)
	second := storedUser(t, m, u.Id)
	assert.True(t, second.JWTRevoked(inFlight), "the in-flight login's token is cut")
	assert.True(t, second.JWTRevoked(pass), "so is anything issued up to the pass")
	assert.False(t, second.JWTRevoked(pass.Add(2*time.Second)), "later sign-ins are fine")
	assert.Nil(t, second.CredentialsRecheckAt, "the pass is done")
	assert.Greater(t, len(creds.revoked), revokedBefore, "refresh tokens are revoked again")
	assert.False(t, second.Blocked)
	peer, err = m.Store.GetPeerByID(ctx, store.LockingStrengthShare, accountID, peerID)
	require.NoError(t, err)
	assert.True(t, peer.Status.LoginExpired, "devices must sign in again")
}

// A user deactivated before the pass is due has nothing left to cut: the
// terminal transition already did. The pass only clears its schedule.
func TestCredentialsRecheck_TerminatedUserOnlyClears(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	_, err := m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	require.NoError(t, err)
	_, err = m.DeactivateLocalUser(ctx, accountID, userID, u.Id)
	require.NoError(t, err)
	recheckAt := *storedUser(t, m, u.Id).CredentialsRecheckAt
	revokedBefore := len(creds.revoked)

	m.runDueCredentialsRechecks(ctx, recheckAt)
	assert.Nil(t, storedUser(t, m, u.Id).CredentialsRecheckAt)
	assert.Len(t, creds.revoked, revokedBefore)
}
