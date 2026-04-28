package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/openzro/openzro/management/server/auth/providers"
)

// newSetupFixture builds a Handler wired with a real bootstrap
// token + a real providers.Store backed by an in-memory sqlite,
// pointing at the fake IdP supplied by the caller. Returns the
// router, the active token, and the providers.Store so tests can
// observe persistence side-effects.
func newSetupFixture(t *testing.T, idp *fakeIdP, clientID string) (*Handler, *providers.Store, string) {
	t.Helper()
	mgr, _ := newTestProviders(t, idp, clientID)

	// The Manager constructor newTestProviders creates also gives
	// us its Store via List+Save round-trips elsewhere in tests,
	// but we need direct access to the Store the wizard writes
	// to. Reuse the providers package's NewStore on the same DB
	// the manager already pre-populated.
	freshStore, freshMgr := newCleanStoreManager(t, idp.URL, clientID)
	bootstrap, err := NewBootstrapTokenStore(t.TempDir())
	require.NoError(t, err)
	tok, err := bootstrap.EnsureMinted()
	require.NoError(t, err)

	sealer := newTestSealer(t)
	sessions := newTestSession(t)
	h, err := NewHandler(freshMgr, sealer, sessions,
		WithSecureCookies(false),
		WithBootstrap(bootstrap, freshStore),
	)
	require.NoError(t, err)

	_ = mgr // not used here; kept to align with other test helpers
	return h, freshStore, tok
}

// newCleanStoreManager builds a providers.Store + Manager with
// no rows configured. Useful for the setup wizard tests because
// we want the wizard to create the very first row.
func newCleanStoreManager(t *testing.T, _, _ string) (*providers.Store, *providers.Manager) {
	t.Helper()
	store := newProvidersStoreFromTempDir(t)
	mgr := providers.NewManager(store, "http://127.0.0.1/auth/callback")
	_, err := mgr.Refresh(context.Background())
	require.NoError(t, err)
	return store, mgr
}

func newProvidersStoreFromTempDir(t *testing.T) *providers.Store {
	t.Helper()
	dsn := "file:" + t.TempDir() + "/providers.db"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	s, err := providers.NewStore(db, base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)
	return s
}

func TestSetup_GETRequiresValidToken(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	h, _, tok := newSetupFixture(t, idp, "client-uuid")

	t.Run("missing token returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/setup", nil)
		rr := httptest.NewRecorder()
		routerFor(h).ServeHTTP(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})

	t.Run("wrong token returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/setup?token=oz_bootstrap_wrong", nil)
		rr := httptest.NewRecorder()
		routerFor(h).ServeHTTP(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})

	t.Run("correct token renders the wizard", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/setup?token="+tok, nil)
		rr := httptest.NewRecorder()
		routerFor(h).ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		body := rr.Body.String()
		assert.Contains(t, body, "First-time setup")
		assert.Contains(t, body, "Issuer URL")
		assert.Contains(t, body, `name="token"`)
	})
}

func TestSetup_POSTCreatesProviderAndInvalidatesToken(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	h, store, tok := newSetupFixture(t, idp, "client-uuid")

	form := url.Values{
		"token":         {tok},
		"name":          {"prod-zitadel"},
		"type":          {string(providers.TypeZitadel)},
		"issuer_url":    {idp.URL},
		"client_id":     {"client-uuid"},
		"client_secret": {"super-secret"},
		"scopes":        {"openid profile email"},
		"brand_label":   {"Acme SSO"},
	}
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)

	require.Equal(t, http.StatusFound, rr.Code, "body=%s", rr.Body.String())
	assert.Equal(t, "/login", rr.Header().Get("Location"))

	rows, err := store.List(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "prod-zitadel", rows[0].Name)
	assert.Equal(t, providers.TypeZitadel, rows[0].Type)
	assert.True(t, rows[0].Enabled)
	assert.Equal(t, "Acme SSO", rows[0].BrandLabel)

	// Token must be invalidated. Re-issuing GET /setup with the
	// same token yields 404.
	req2 := httptest.NewRequest(http.MethodGet, "/setup?token="+tok, nil)
	rr2 := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr2, req2)
	assert.Equal(t, http.StatusNotFound, rr2.Code,
		"bootstrap token must be one-shot")
}

func TestSetup_POSTRejectsInvalidToken(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	h, store, _ := newSetupFixture(t, idp, "client-uuid")

	form := url.Values{
		"token":         {"oz_bootstrap_wrong"},
		"name":          {"x"},
		"issuer_url":    {idp.URL},
		"client_id":     {"client-uuid"},
		"client_secret": {"s"},
	}
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	rows, err := store.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, rows, "wrong token must not have created a row")
}

func TestSetup_POSTRendersValidationErrors(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	h, store, tok := newSetupFixture(t, idp, "client-uuid")

	form := url.Values{
		"token":         {tok},
		"name":          {""}, // missing
		"issuer_url":    {idp.URL},
		"client_id":     {"client-uuid"},
		"client_secret": {"s"},
	}
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "Name is required")

	rows, err := store.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestSetup_NotRegisteredWithoutBootstrap(t *testing.T) {
	// Build a Handler WITHOUT WithBootstrap — /setup should not
	// be registered, so requests yield 404 from the router.
	idp := newFakeIdP(t, "client-uuid")
	mgr, _ := newTestProviders(t, idp, "client-uuid")
	sealer := newTestSealer(t)
	sessions := newTestSession(t)
	h, err := NewHandler(mgr, sealer, sessions, WithSecureCookies(false))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/setup?token=anything", nil)
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}
