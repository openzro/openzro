package server

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/auth/jwt"
	"github.com/openzro/openzro/management/server/dex_proxy"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

const goodPassword = "correct-horse-battery"

// fakeLocalCredentials stands in for Dex's password DB.
type fakeLocalCredentials struct {
	mu        sync.Mutex
	entries   map[string]*fakePassword // key: lowercased email
	listErr   error
	createErr error
	// raceOnCreate makes the next CreatePassword report AlreadyExists even
	// though the preflight saw no entry: a writer slipped in between.
	raceOnCreate bool
	updates      int
	deleteErr    error
	revokeErr    error
	revoked      []string // subjects whose refresh tokens were revoked
}

type fakePassword struct {
	userID string
	hash   []byte
}

func newFakeLocalCredentials() *fakeLocalCredentials {
	return &fakeLocalCredentials{entries: map[string]*fakePassword{}}
}

func (f *fakeLocalCredentials) CreatePassword(_ context.Context, email, _, userID string, hash []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	if f.raceOnCreate {
		f.raceOnCreate = false
		f.entries[strings.ToLower(email)] = &fakePassword{userID: "intruder", hash: []byte("x")}
		return dex_proxy.ErrAlreadyExists
	}
	if _, ok := f.entries[strings.ToLower(email)]; ok {
		return dex_proxy.ErrAlreadyExists
	}
	f.entries[strings.ToLower(email)] = &fakePassword{userID: userID, hash: hash}
	return nil
}

func (f *fakeLocalCredentials) UpdatePassword(_ context.Context, email string, hash []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[strings.ToLower(email)]
	if !ok {
		return dex_proxy.ErrNotFound
	}
	e.hash = hash
	f.updates++
	return nil
}

func (f *fakeLocalCredentials) ListPasswords(_ context.Context) ([]dex_proxy.PasswordEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []dex_proxy.PasswordEntry
	for email, e := range f.entries {
		out = append(out, dex_proxy.PasswordEntry{Email: email, UserID: e.userID})
	}
	return out, nil
}

func (f *fakeLocalCredentials) DeletePassword(_ context.Context, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.entries[strings.ToLower(email)]; !ok {
		return dex_proxy.ErrNotFound
	}
	delete(f.entries, strings.ToLower(email))
	return nil
}

func (f *fakeLocalCredentials) RevokeAllRefreshTokens(_ context.Context, sub string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.revokeErr != nil {
		return f.revokeErr
	}
	f.revoked = append(f.revoked, sub)
	return nil
}

func (f *fakeLocalCredentials) revokedFor(sub string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.revoked {
		if s == sub {
			n++
		}
	}
	return n
}

func (f *fakeLocalCredentials) entry(email string) *fakePassword {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.entries[strings.ToLower(email)]
}

func setupLocalUsers(t *testing.T) (*DefaultAccountManager, string, *fakeLocalCredentials) {
	t.Helper()
	manager, err := createManager(t)
	require.NoError(t, err)
	accountID, err := manager.GetAccountIDByUserID(context.Background(), userID, "")
	require.NoError(t, err)
	creds := newFakeLocalCredentials()
	manager.EnableLocalUsers(creds)
	return manager, accountID, creds
}

func localInput(userName string) account.SCIMUserInput {
	return account.SCIMUserInput{UserName: userName, DisplayName: "Ana", Active: true, AutoGroups: []string{}}
}

func requireStatusType(t *testing.T, err error, want status.Type) {
	t.Helper()
	require.Error(t, err)
	sErr, ok := status.FromError(err)
	require.True(t, ok, "expected a status error, got %v", err)
	require.Equal(t, want, sErr.Type(), "unexpected error: %v", err)
}

func requireNoLocalUser(t *testing.T, m *DefaultAccountManager, accountID, userName string) {
	t.Helper()
	u, err := m.findSCIMUserByUserName(context.Background(), accountID, userName)
	require.NoError(t, err)
	require.Nil(t, u, "no user record may remain for %s", userName)
}

func TestSCIMCreateLocalUser_CreatesActiveUserUnderDexSubject(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()

	user, err := m.SCIMCreateLocalUser(ctx, accountID, userID, localInput("Ana@Example.com"), goodPassword)
	require.NoError(t, err)

	entry := creds.entry("ana@example.com")
	require.NotNil(t, entry, "the Dex credential was not created")
	require.NoError(t, bcrypt.CompareHashAndPassword(entry.hash, []byte(goodPassword)))
	assert.Equal(t, jwt.EncodeDexSubject(entry.userID, "local"), user.Id,
		"the openZro user ID must be the sub Dex will issue")

	stored, err := m.Store.GetUserByUserID(ctx, store.LockingStrengthShare, user.Id)
	require.NoError(t, err)
	assert.Equal(t, types.LocalCredentialActive, stored.LocalCredentialState)
	assert.False(t, stored.Blocked)
	assert.Equal(t, "Ana@Example.com", stored.SCIMUserName)
}

func TestSCIMCreateLocalUser_Validation(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		input    account.SCIMUserInput
		password string
	}{
		{name: "11 code points", input: localInput("a@example.com"), password: "elevenchars"},
		{name: "73 bytes", input: localInput("b@example.com"), password: strings.Repeat("a", 73)},
		{name: "25 three-byte runes = 75 bytes", input: localInput("c@example.com"), password: strings.Repeat("€", 25)},
		{name: "userName not an email", input: localInput("ana"), password: goodPassword},
		{name: "inactive on create", input: account.SCIMUserInput{UserName: "d@example.com", Active: false}, password: goodPassword},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.SCIMCreateLocalUser(ctx, accountID, userID, tc.input, tc.password)
			requireStatusType(t, err, status.BadRequest)
			requireNoLocalUser(t, m, accountID, tc.input.UserName)
			require.Nil(t, creds.entry(tc.input.UserName), "no Dex credential may be created")
		})
	}

	// Boundaries that must pass: exactly 72 bytes, and 24 three-byte runes.
	_, err := m.SCIMCreateLocalUser(ctx, accountID, userID, localInput("e@example.com"), strings.Repeat("a", 72))
	require.NoError(t, err)
	_, err = m.SCIMCreateLocalUser(ctx, accountID, userID, localInput("f@example.com"), strings.Repeat("€", 24))
	require.NoError(t, err)
}

func TestSCIMCreateLocalUser_DisabledByDefault(t *testing.T) {
	manager, err := createManager(t)
	require.NoError(t, err)
	accountID, err := manager.GetAccountIDByUserID(context.Background(), userID, "")
	require.NoError(t, err)

	_, err = manager.SCIMCreateLocalUser(context.Background(), accountID, userID, localInput("ana@example.com"), goodPassword)
	requireStatusType(t, err, status.BadRequest)
	requireNoLocalUser(t, manager, accountID, "ana@example.com")
}

func TestSCIMCreateLocalUser_RespectsEmailAllowlist(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	settings, err := m.Store.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
	require.NoError(t, err)
	settings.AllowedEmailDomains = []string{"example.com"}
	require.NoError(t, m.Store.SaveAccountSettings(ctx, store.LockingStrengthUpdate, accountID, settings))

	_, err = m.SCIMCreateLocalUser(ctx, accountID, userID, localInput("eve@evil.com"), goodPassword)
	requireStatusType(t, err, status.BadRequest)
	require.Nil(t, creds.entry("eve@evil.com"))
}

// ADR-0024 D4/D5: Dex credential ownership is verified, never assumed.
func TestSCIMCreateLocalUser_Conflicts(t *testing.T) {
	ctx := context.Background()

	t.Run("existing active local user", func(t *testing.T) {
		m, accountID, creds := setupLocalUsers(t)
		_, err := m.SCIMCreateLocalUser(ctx, accountID, userID, localInput("ana@example.com"), goodPassword)
		require.NoError(t, err)
		before := creds.entry("ana@example.com").hash

		_, err = m.SCIMCreateLocalUser(ctx, accountID, userID, localInput("ANA@example.com"), "another-password-1")
		require.ErrorIs(t, err, ErrSCIMUserAlreadyExists)
		require.Equal(t, before, creds.entry("ana@example.com").hash, "the existing credential must not change")
	})

	t.Run("credential owned by someone else", func(t *testing.T) {
		m, accountID, creds := setupLocalUsers(t)
		creds.entries["ana@example.com"] = &fakePassword{userID: "other-account-user", hash: []byte("theirs")}

		_, err := m.SCIMCreateLocalUser(ctx, accountID, userID, localInput("ana@example.com"), goodPassword)
		requireStatusType(t, err, status.AlreadyExists)
		require.Equal(t, []byte("theirs"), creds.entry("ana@example.com").hash)
		require.Equal(t, 0, creds.updates)
		requireNoLocalUser(t, m, accountID, "ana@example.com")
	})

	t.Run("writer slips in after the preflight", func(t *testing.T) {
		m, accountID, creds := setupLocalUsers(t)
		creds.raceOnCreate = true

		_, err := m.SCIMCreateLocalUser(ctx, accountID, userID, localInput("ana@example.com"), goodPassword)
		requireStatusType(t, err, status.AlreadyExists)
		require.Equal(t, "intruder", creds.entry("ana@example.com").userID, "never adopt after AlreadyExists")
		require.Equal(t, 0, creds.updates)
		requireNoLocalUser(t, m, accountID, "ana@example.com")
	})
}

// ADR-0024 D4: a create interrupted after the reservation resumes on retry
// with the stored identity, and fails closed meanwhile.
func TestSCIMCreateLocalUser_ResumesInterruptedCreate(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()

	// First attempt: the reservation is written, then Dex is unreachable.
	creds.listErr = errors.New("dex down")
	_, err := m.SCIMCreateLocalUser(ctx, accountID, userID, localInput("ana@example.com"), goodPassword)
	requireStatusType(t, err, status.Internal)

	pending, err := m.findSCIMUserByUserName(ctx, accountID, "ana@example.com")
	require.NoError(t, err)
	require.NotNil(t, pending, "the reservation stays")
	require.Equal(t, types.LocalCredentialPending, pending.LocalCredentialState)
	require.True(t, pending.Blocked, "an interrupted create fails closed")

	// Simulate the credential having been created before the crash.
	uid, _, ok := jwt.DecodeDexSubject(pending.Id)
	require.True(t, ok)
	creds.listErr = nil
	creds.entries["ana@example.com"] = &fakePassword{userID: uid, hash: []byte("half-written")}

	user, err := m.SCIMCreateLocalUser(ctx, accountID, userID, localInput("ana@example.com"), goodPassword)
	require.NoError(t, err)
	require.Equal(t, pending.Id, user.Id, "a retry resumes the same identity")
	require.Equal(t, 1, creds.updates)
	require.NoError(t, bcrypt.CompareHashAndPassword(creds.entry("ana@example.com").hash, []byte(goodPassword)))

	stored, err := m.Store.GetUserByUserID(ctx, store.LockingStrengthShare, user.Id)
	require.NoError(t, err)
	require.Equal(t, types.LocalCredentialActive, stored.LocalCredentialState)
	require.False(t, stored.Blocked)
}

// ADR-0024 D8: the plaintext password never reaches logs.
func TestSCIMCreateLocalUser_NeverLogsPassword(t *testing.T) {
	var buf bytes.Buffer
	prevOut, prevLevel := log.StandardLogger().Out, log.GetLevel()
	log.SetOutput(&buf)
	log.SetLevel(log.TraceLevel)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetLevel(prevLevel)
	})

	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	_, err := m.SCIMCreateLocalUser(ctx, accountID, userID, localInput("ana@example.com"), goodPassword)
	require.NoError(t, err)
	creds.listErr = errors.New("dex down")
	_, _ = m.SCIMCreateLocalUser(ctx, accountID, userID, localInput("bea@example.com"), goodPassword)

	require.NotContains(t, buf.String(), goodPassword)
}
