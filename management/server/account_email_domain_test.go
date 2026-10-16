package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/activity"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

const (
	emailDomainAccountID = "acct-email-domain"
	emailDomainOwnerID   = "owner-email-domain"
	emailDomainDomain    = "example.com"
)

type failingAccountSettingsStore struct {
	store.Store
}

func (s *failingAccountSettingsStore) ExecuteInTransaction(ctx context.Context, f func(store.Store) error) error {
	return f(s)
}

func (s *failingAccountSettingsStore) GetAccountSettings(context.Context, store.LockingStrength, string) (*types.Settings, error) {
	return nil, errors.New("store unavailable")
}

// setupEmailDomainAccount creates the primary private-domain account a new
// user joins on first login, with the given allowlist.
func setupEmailDomainAccount(t *testing.T, allowed []string) *DefaultAccountManager {
	t.Helper()
	manager, err := createManager(t)
	require.NoError(t, err)

	account := newAccountWithId(context.Background(), emailDomainAccountID, emailDomainOwnerID, emailDomainDomain, false)
	account.DomainCategory = types.PrivateCategory
	account.IsDomainPrimaryAccount = true
	account.Settings.AllowedEmailDomains = allowed
	require.NoError(t, manager.Store.SaveAccount(context.Background(), account))
	return manager
}

func joinAuth(userID, email string, verified bool) nbcontext.UserAuth {
	return nbcontext.UserAuth{
		UserId:         userID,
		Domain:         emailDomainDomain,
		DomainCategory: types.PrivateCategory,
		Email:          email,
		EmailVerified:  verified,
	}
}

// ADR-0025 D4/D5: a first login joining an account with a non-empty
// allowlist needs a verified email in a listed domain; every deny leaves no
// user behind and records an audit event.
func TestGetAccountIDFromUserAuth_AllowedEmailDomains(t *testing.T) {
	tests := []struct {
		name       string
		allowed    []string
		email      string
		verified   bool
		wantJoin   bool
		wantReason string
	}{
		{name: "empty list keeps legacy behavior", allowed: nil, email: "", verified: false, wantJoin: true},
		{name: "listed domain joins", allowed: []string{"example.com"}, email: "ana@example.com", verified: true, wantJoin: true},
		{name: "case-insensitive match joins", allowed: []string{"example.com"}, email: "Ana@EXAMPLE.com", verified: true, wantJoin: true},
		{name: "unlisted domain denied", allowed: []string{"example.com"}, email: "eve@evil.com", verified: true, wantReason: "domain"},
		{name: "subdomain denied", allowed: []string{"example.com"}, email: "eve@eng.example.com", verified: true, wantReason: "domain"},
		{name: "unverified email denied", allowed: []string{"example.com"}, email: "ana@example.com", verified: false, wantReason: "unverified"},
		{name: "missing email denied", allowed: []string{"example.com"}, email: "", verified: true, wantReason: "missing"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manager := setupEmailDomainAccount(t, tc.allowed)
			userID := "new-user"

			accountID, _, err := manager.GetAccountIDFromUserAuth(context.Background(), joinAuth(userID, tc.email, tc.verified))

			if tc.wantJoin {
				require.NoError(t, err)
				assert.Equal(t, emailDomainAccountID, accountID)
				_, err = manager.Store.GetUserByUserID(context.Background(), store.LockingStrengthShare, userID)
				require.NoError(t, err, "joined user must be persisted")
				return
			}

			require.Error(t, err)
			sErr, ok := status.FromError(err)
			require.True(t, ok, "expected a status error, got %v", err)
			assert.Equal(t, status.PermissionDenied, sErr.Type())
			assert.Equal(t, errEmailDomainNotAllowed, sErr.Message, "all deny reasons share one message")

			_, err = manager.Store.GetUserByUserID(context.Background(), store.LockingStrengthShare, userID)
			require.Error(t, err, "denied user must not be persisted")

			assert.Eventually(t, func() bool {
				events, err := manager.eventStore.Get(context.Background(), emailDomainAccountID, 0, 100, false)
				if err != nil {
					return false
				}
				for _, e := range events {
					if e.Activity == activity.UserJoinDeniedByDomain && e.Meta["reason"] == tc.wantReason {
						assert.Equal(t, activity.SystemInitiator, e.InitiatorID)
						assert.Equal(t, emailDomainAccountID, e.TargetID)
						return true
					}
				}
				return false
			}, 2*time.Second, 20*time.Millisecond, "deny audit event with reason %q", tc.wantReason)
		})
	}
}

// ADR-0025 D4: the allowlist is not retroactive — an existing member whose
// domain is not listed keeps logging in.
func TestGetAccountIDFromUserAuth_AllowedEmailDomains_ExistingUser(t *testing.T) {
	manager := setupEmailDomainAccount(t, []string{"other.com"})

	accountID, userID, err := manager.GetAccountIDFromUserAuth(context.Background(),
		joinAuth(emailDomainOwnerID, "owner@example.com", false))
	require.NoError(t, err)
	assert.Equal(t, emailDomainAccountID, accountID)
	assert.Equal(t, emailDomainOwnerID, userID)
}

// ADR-0025 D2: the allowlist is normalized on write, invalid entries are
// rejected, and a change is audited.
func TestUpdateAccountSettings_AllowedEmailDomains(t *testing.T) {
	manager, err := createManager(t)
	require.NoError(t, err)
	accountID, err := manager.GetAccountIDByUserID(context.Background(), userID, "")
	require.NoError(t, err)

	base := func(domains []string) *types.Settings {
		return &types.Settings{
			PeerLoginExpiration: time.Hour,
			AllowedEmailDomains: domains,
			Extra:               &types.ExtraSettings{},
		}
	}

	got, err := manager.UpdateAccountSettings(context.Background(), accountID, userID,
		base([]string{" @Example.COM", "example.com", "eng.example.com"}))
	require.NoError(t, err)
	assert.Equal(t, []string{"example.com", "eng.example.com"}, got.AllowedEmailDomains)

	stored, err := manager.Store.GetAccountSettings(context.Background(), store.LockingStrengthShare, accountID)
	require.NoError(t, err)
	assert.Equal(t, []string{"example.com", "eng.example.com"}, stored.AllowedEmailDomains)

	assert.Eventually(t, func() bool {
		events, err := manager.eventStore.Get(context.Background(), accountID, 0, 100, false)
		if err != nil {
			return false
		}
		for _, e := range events {
			if e.Activity == activity.AccountAllowedEmailDomainsUpdated {
				return true
			}
		}
		return false
	}, 2*time.Second, 20*time.Millisecond, "allowlist change must be audited")

	_, err = manager.UpdateAccountSettings(context.Background(), accountID, userID, base([]string{"*.example.com"}))
	sErr, ok := status.FromError(err)
	require.True(t, ok && err != nil, "expected a status error, got %v", err)
	assert.Equal(t, status.InvalidArgument, sErr.Type())

	stored, err = manager.Store.GetAccountSettings(context.Background(), store.LockingStrengthShare, accountID)
	require.NoError(t, err)
	assert.Equal(t, []string{"example.com", "eng.example.com"}, stored.AllowedEmailDomains,
		"a rejected update must not change the stored list")

	// Nil means the API field was omitted. The manager must merge the
	// current value while holding the row lock rather than trusting a stale
	// value read by the HTTP handler.
	got, err = manager.UpdateAccountSettings(context.Background(), accountID, userID, base(nil))
	require.NoError(t, err)
	assert.Equal(t, []string{"example.com", "eng.example.com"}, got.AllowedEmailDomains)

	// If that transactional read fails, the update aborts rather than
	// treating omission as an explicit clear.
	workingStore := manager.Store
	manager.Store = &failingAccountSettingsStore{Store: workingStore}
	_, err = manager.UpdateAccountSettings(context.Background(), accountID, userID, base(nil))
	require.ErrorContains(t, err, "store unavailable")
	stored, err = workingStore.GetAccountSettings(context.Background(), store.LockingStrengthShare, accountID)
	require.NoError(t, err)
	assert.Equal(t, []string{"example.com", "eng.example.com"}, stored.AllowedEmailDomains)
}
