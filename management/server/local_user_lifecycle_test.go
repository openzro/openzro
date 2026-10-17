package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/openzro/openzro/management/server/account"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

const newPassword = "another-strong-password"

func createLocal(t *testing.T, m *DefaultAccountManager, accountID, email string) *types.User {
	t.Helper()
	u, err := m.SCIMCreateLocalUser(context.Background(), accountID, userID, localInput(email), goodPassword)
	require.NoError(t, err)
	return u
}

// ADR-0024 D6: a password reset changes only the credential, after an
// ownership check, and revokes the user's refresh tokens.
func TestSCIMResetLocalPassword(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")

	_, err := m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	require.NoError(t, err)
	require.NoError(t, bcrypt.CompareHashAndPassword(creds.entry("ana@example.com").hash, []byte(newPassword)))
	assert.Equal(t, 1, creds.revokedFor(u.Id), "a reset must revoke refresh tokens")

	// Policy applies.
	_, err = m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, "short", nil)
	requireStatusType(t, err, status.BadRequest)

	// A PUT that resets the password may change nothing else.
	same := account.SCIMUserInput{UserName: "ANA@example.com", DisplayName: "Ana", Active: true}
	_, err = m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, &same)
	require.NoError(t, err)
	changed := account.SCIMUserInput{UserName: "ana@example.com", DisplayName: "Someone else", Active: true}
	_, err = m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, &changed)
	requireStatusType(t, err, status.BadRequest)

	// Only local users have a password here.
	plain, err := m.SCIMCreateUser(ctx, accountID, userID, account.SCIMUserInput{UserName: "bea@example.com", Active: true})
	require.NoError(t, err)
	_, err = m.SCIMResetLocalPassword(ctx, accountID, userID, plain.Id, newPassword, nil)
	requireStatusType(t, err, status.BadRequest)
}

// ADR-0024 D5: a credential that is missing or owned by someone else is
// never written; it signals out-of-band tampering.
func TestSCIMResetLocalPassword_OwnershipMismatch(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	creds.entries["ana@example.com"].userID = "someone-else"

	_, err := m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	requireStatusType(t, err, status.Internal)
	require.Equal(t, 0, creds.updates)
}

// connectPeer gives the user a peer with an open update channel.
func connectPeer(t *testing.T, m *DefaultAccountManager, user *types.User) (string, chan *UpdateMessage) {
	t.Helper()
	key, err := wgtypes.GeneratePrivateKey()
	require.NoError(t, err)
	peer, _, _, err := m.AddPeer(context.Background(), "", user.Id, &nbpeer.Peer{
		Key: key.PublicKey().String(), Meta: nbpeer.PeerSystemMeta{Hostname: "laptop"},
	})
	require.NoError(t, err)
	return peer.ID, m.peersUpdateManager.CreateChannel(context.Background(), peer.ID)
}

// ADR-0024 D7: deactivation is terminal and runs the full cleanup: peers
// expired and disconnected, PATs deleted, refresh tokens revoked, the Dex
// credential removed. The user stays visible to SCIM as inactive.
func TestSCIMDeactivateLocalUser_RunsFullCleanup(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	peerID, ch := connectPeer(t, m, u)
	// Regular users cannot mint PATs themselves; an admin-promoted local
	// user can, so seed one directly.
	pat, err := types.CreateNewPAT("ana-token", 30, u.Id, userID)
	require.NoError(t, err)
	require.NoError(t, m.Store.SavePAT(ctx, store.LockingStrengthUpdate, &pat.PersonalAccessToken))

	inactive := false
	got, err := m.SCIMPatchUser(ctx, accountID, userID, u.Id, account.SCIMUserPatch{Active: &inactive})
	require.NoError(t, err)
	assert.True(t, got.Blocked)

	stored, err := m.Store.GetUserByUserID(ctx, store.LockingStrengthShare, u.Id)
	require.NoError(t, err)
	assert.Equal(t, types.LocalCredentialDeactivated, stored.LocalCredentialState)
	assert.False(t, stored.LocalCleanupPending, "cleanup converged")
	requirePeerCutOff(t, m, accountID, peerID, ch)
	pats, err := m.Store.GetUserPATs(ctx, store.LockingStrengthShare, u.Id)
	require.NoError(t, err)
	assert.Empty(t, pats)
	assert.Equal(t, 1, creds.revokedFor(u.Id))
	assert.Nil(t, creds.entry("ana@example.com"), "the Dex credential is removed")

	visible, err := m.SCIMGetUser(ctx, accountID, userID, u.Id)
	require.NoError(t, err, "a deactivated local user stays visible to SCIM")
	assert.True(t, visible.Blocked)

	// Re-activation is not supported.
	active := true
	_, err = m.SCIMPatchUser(ctx, accountID, userID, u.Id, account.SCIMUserPatch{Active: &active})
	requireStatusType(t, err, status.BadRequest)
	// Re-sending the deactivation is idempotent.
	_, err = m.SCIMPatchUser(ctx, accountID, userID, u.Id, account.SCIMUserPatch{Active: &inactive})
	require.NoError(t, err)
	// A reset on a deactivated user is refused.
	_, err = m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	requireStatusType(t, err, status.BadRequest)
}

// ADR-0024 D7: DELETE tombstones the user (404 afterwards, hidden from
// lists), and the email cannot be provisioned again in v1.
func TestSCIMDeleteLocalUser_TombstonesAndHides(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")

	require.NoError(t, m.SCIMDeactivateUser(ctx, accountID, userID, u.Id))

	stored, err := m.Store.GetUserByUserID(ctx, store.LockingStrengthShare, u.Id)
	require.NoError(t, err)
	assert.Equal(t, types.LocalCredentialTombstoned, stored.LocalCredentialState)
	assert.True(t, stored.Blocked)
	assert.False(t, stored.LocalCleanupPending)
	assert.Nil(t, creds.entry("ana@example.com"))

	_, err = m.SCIMGetUser(ctx, accountID, userID, u.Id)
	require.ErrorIs(t, err, ErrSCIMUserNotFound)
	require.Error(t, m.SCIMDeactivateUser(ctx, accountID, userID, u.Id), "DELETE on a cleaned tombstone is 404")

	_, err = m.SCIMCreateLocalUser(ctx, accountID, userID, localInput("ana@example.com"), goodPassword)
	require.ErrorIs(t, err, ErrSCIMUserAlreadyExists, "no re-creation of a used email in v1")
}

// ADR-0024 D7: a failed cleanup step fails the request and leaves the user
// blocked with cleanup pending; retrying resumes and converges.
func TestSCIMDeleteLocalUser_FailedCleanupResumes(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")

	creds.revokeErr = errors.New("dex down")
	requireStatusType(t, m.SCIMDeactivateUser(ctx, accountID, userID, u.Id), status.Internal)
	stored, err := m.Store.GetUserByUserID(ctx, store.LockingStrengthShare, u.Id)
	require.NoError(t, err)
	assert.True(t, stored.Blocked, "access is cut even though cleanup is incomplete")
	assert.True(t, stored.LocalCleanupPending)
	assert.NotNil(t, creds.entry("ana@example.com"))

	creds.revokeErr = nil
	require.NoError(t, m.SCIMDeactivateUser(ctx, accountID, userID, u.Id), "a retried DELETE resumes the cleanup")
	stored, err = m.Store.GetUserByUserID(ctx, store.LockingStrengthShare, u.Id)
	require.NoError(t, err)
	assert.False(t, stored.LocalCleanupPending)
	assert.Nil(t, creds.entry("ana@example.com"))
}

// ADR-0024 D7: a credential owned by someone else is never deleted.
func TestSCIMDeleteLocalUser_OwnershipMismatch(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	creds.entries["ana@example.com"].userID = "someone-else"

	requireStatusType(t, m.SCIMDeactivateUser(ctx, accountID, userID, u.Id), status.Internal)
	require.NotNil(t, creds.entry("ana@example.com"), "a foreign credential is left untouched")
}

// The REST delete of a local user (dashboard, service user) follows the
// same tombstone path instead of removing the record.
func TestRESTDeleteLocalUser_Tombstones(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	svc, err := m.CreateUser(ctx, accountID, userID, &types.UserInfo{
		Name: "iga", Role: string(types.UserRoleAdmin), IsServiceUser: true, Issued: types.UserIssuedAPI,
	})
	require.NoError(t, err)
	u := createLocal(t, m, accountID, "ana@example.com")

	require.NoError(t, m.DeleteUser(ctx, accountID, svc.ID, u.Id))
	stored, err := m.Store.GetUserByUserID(ctx, store.LockingStrengthShare, u.Id)
	require.NoError(t, err)
	assert.Equal(t, types.LocalCredentialTombstoned, stored.LocalCredentialState)
	assert.Nil(t, creds.entry("ana@example.com"))
}
