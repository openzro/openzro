package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/idp"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// setupProvisioningAccount returns a manager whose account (owned by the
// package-level test userID) carries the given allowlist.
func setupProvisioningAccount(t *testing.T, allowed []string) (*DefaultAccountManager, string) {
	t.Helper()
	manager, err := createManager(t)
	require.NoError(t, err)
	accountID, err := manager.GetAccountIDByUserID(context.Background(), userID, "")
	require.NoError(t, err)

	settings, err := manager.Store.GetAccountSettings(context.Background(), store.LockingStrengthShare, accountID)
	require.NoError(t, err)
	settings.AllowedEmailDomains = allowed
	require.NoError(t, manager.Store.SaveAccountSettings(context.Background(), store.LockingStrengthUpdate, accountID, settings))
	return manager, accountID
}

func requireDomainBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	sErr, ok := status.FromError(err)
	require.True(t, ok, "expected a status error, got %v", err)
	assert.Equal(t, status.BadRequest, sErr.Type())
}

// ADR-0025 D4: SCIM can only provision userNames in an allowed domain.
func TestSCIMCreateUser_AllowedEmailDomains(t *testing.T) {
	tests := []struct {
		name     string
		allowed  []string
		userName string
		wantErr  bool
	}{
		{name: "empty list allows any userName", allowed: nil, userName: "not-an-email", wantErr: false},
		{name: "listed domain", allowed: []string{"example.com"}, userName: "ana@example.com", wantErr: false},
		{name: "unlisted domain", allowed: []string{"example.com"}, userName: "eve@evil.com", wantErr: true},
		{name: "non-email userName", allowed: []string{"example.com"}, userName: "ana", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manager, accountID := setupProvisioningAccount(t, tc.allowed)
			user, err := manager.SCIMCreateUser(context.Background(), accountID, userID,
				account.SCIMUserInput{UserName: tc.userName, Active: true})
			if tc.wantErr {
				requireDomainBadRequest(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.userName, user.SCIMUserName)
		})
	}
}

// ADR-0025 D4: changing userName is checked; re-sending an unchanged
// userName that predates the allowlist is not.
func TestSCIMUpdateUser_AllowedEmailDomains(t *testing.T) {
	manager, accountID := setupProvisioningAccount(t, nil)
	legacy, err := manager.SCIMCreateUser(context.Background(), accountID, userID,
		account.SCIMUserInput{UserName: "old@legacy.com", Active: true})
	require.NoError(t, err)

	settings, err := manager.Store.GetAccountSettings(context.Background(), store.LockingStrengthShare, accountID)
	require.NoError(t, err)
	settings.AllowedEmailDomains = []string{"example.com"}
	require.NoError(t, manager.Store.SaveAccountSettings(context.Background(), store.LockingStrengthUpdate, accountID, settings))

	// Unchanged userName: allowed on both PUT and PATCH.
	_, err = manager.SCIMReplaceUser(context.Background(), accountID, userID, legacy.Id,
		account.SCIMUserInput{UserName: "old@legacy.com", DisplayName: "Old", Active: true})
	require.NoError(t, err)
	same := "old@legacy.com"
	_, err = manager.SCIMPatchUser(context.Background(), accountID, userID, legacy.Id,
		account.SCIMUserPatch{UserName: &same})
	require.NoError(t, err)
	caseOnly := "OLD@LEGACY.COM"
	updated, err := manager.SCIMPatchUser(context.Background(), accountID, userID, legacy.Id,
		account.SCIMUserPatch{UserName: &caseOnly})
	require.NoError(t, err, "SCIM userName is case-insensitive; a case-only re-send is unchanged")
	assert.Equal(t, caseOnly, updated.SCIMUserName)

	// Changed to an unlisted domain: rejected on both.
	_, err = manager.SCIMReplaceUser(context.Background(), accountID, userID, legacy.Id,
		account.SCIMUserInput{UserName: "eve@evil.com", Active: true})
	requireDomainBadRequest(t, err)
	evil := "eve@evil.com"
	_, err = manager.SCIMPatchUser(context.Background(), accountID, userID, legacy.Id,
		account.SCIMUserPatch{UserName: &evil})
	requireDomainBadRequest(t, err)

	// Changed to a listed domain: allowed.
	good := "ana@example.com"
	updated, err = manager.SCIMPatchUser(context.Background(), accountID, userID, legacy.Id,
		account.SCIMUserPatch{UserName: &good})
	require.NoError(t, err)
	assert.Equal(t, good, updated.SCIMUserName)

	// PATCH must not bypass the existing create/replace validation.
	empty := ""
	_, err = manager.SCIMPatchUser(context.Background(), accountID, userID, legacy.Id,
		account.SCIMUserPatch{UserName: &empty})
	require.Error(t, err)
	sErr, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, status.InvalidArgument, sErr.Type())
}

// ADR-0025 D4: the IdP-manager invite rejects unlisted domains before any
// IdP call is made.
func TestInviteUser_AllowedEmailDomains(t *testing.T) {
	manager, accountID := setupProvisioningAccount(t, []string{"example.com"})
	manager.idpManager = &idp.MockIDP{}

	_, err := manager.CreateUser(context.Background(), accountID, userID, &types.UserInfo{
		Email: "eve@evil.com",
		Name:  "Eve",
		Role:  string(types.UserRoleUser),
	})
	requireDomainBadRequest(t, err)
}
