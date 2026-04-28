package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/openzro/openzro/management/server/auth/providers"
)

// emptyManager builds a Manager with no providers configured.
func emptyManager(t *testing.T) *providers.Manager {
	t.Helper()
	dsn := "file:" + t.TempDir() + "/test.db"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	store, err := providers.NewStore(db, base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)
	mgr := providers.NewManager(store, "http://127.0.0.1/auth/callback")
	_, err = mgr.Refresh(context.Background())
	require.NoError(t, err)
	return mgr
}

func TestLogin_RendersWithProviders(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	mgr, providerID := newTestProviders(t, idp, "client-uuid")
	h, _, _ := newTestHandler(t, mgr)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "openZro")
	assert.Contains(t, body, "Sign in")
	// Brand pieces
	assert.Contains(t, body, `<span class="oz-z">`)
	// One <a> per provider, pointing at /auth/start
	assert.Contains(t, body, "/auth/start?provider="+strconv.FormatUint(providerID, 10))
	assert.Contains(t, body, "Sign in with test-idp")

	// Security headers
	assert.Equal(t, "DENY", rr.Header().Get("X-Frame-Options"))
	assert.Equal(t, "nosniff", rr.Header().Get("X-Content-Type-Options"))
	assert.Contains(t, rr.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'")
	assert.Contains(t, rr.Header().Get("Cache-Control"), "no-store")
}

func TestLogin_EmptyState(t *testing.T) {
	mgr := emptyManager(t)
	h, _, _ := newTestHandler(t, mgr)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "No authentication providers configured")
	assert.Contains(t, body, "Settings")
	assert.NotContains(t, body, "Sign in with")
}

func TestLogin_PreservesSafeReturnTo(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	mgr, providerID := newTestProviders(t, idp, "client-uuid")
	h, _, _ := newTestHandler(t, mgr)

	req := httptest.NewRequest(http.MethodGet, "/login?return_to=/peers/12", nil)
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "provider="+strconv.FormatUint(providerID, 10))
	assert.Contains(t, body, "return_to=%2Fpeers%2F12")
}

func TestLogin_DropsUnsafeReturnTo(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	mgr, _ := newTestProviders(t, idp, "client-uuid")
	h, _, _ := newTestHandler(t, mgr)

	req := httptest.NewRequest(http.MethodGet, "/login?return_to=https://evil.example.com", nil)
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.NotContains(t, body, "evil.example.com")
	assert.NotContains(t, body, "return_to=")
}

func TestLogin_RendersError(t *testing.T) {
	mgr := emptyManager(t)
	h, _, _ := newTestHandler(t, mgr)

	req := httptest.NewRequest(http.MethodGet, "/login?error=upstream+rejected+sign-in", nil)
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "oz-login__error")
	assert.Contains(t, body, "upstream rejected sign-in")
}

func TestLogin_FiltersGitHubProviders(t *testing.T) {
	// Configure two providers: one OIDC (Verifier present) and
	// one github (Verifier nil — userinfo path not implemented).
	// The login page must surface only the OIDC one until the
	// userinfo path lands.
	idp := newFakeIdP(t, "client-uuid")
	mgr, oidcID := newTestProviders(t, idp, "client-uuid")

	// Add a github-typed provider.
	dsn := "file:" + t.TempDir() + "/test2.db"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	mixedStore, err := providers.NewStore(db, base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)
	_, err = mixedStore.Save(context.Background(), providers.SaveInput{
		Name:    "test-idp",
		Type:    providers.TypeZitadel,
		Enabled: true,
		Config: providers.Config{
			IssuerURL:    idp.URL,
			ClientID:     "client-uuid",
			ClientSecret: "test-secret",
		},
	})
	require.NoError(t, err)
	_, err = mixedStore.Save(context.Background(), providers.SaveInput{
		Name:    "github-contractors",
		Type:    providers.TypeGitHub,
		Enabled: true,
		Config: providers.Config{
			IssuerURL:    "https://github.com",
			ClientID:     "github-client",
			ClientSecret: "github-secret",
		},
	})
	require.NoError(t, err)
	mixedMgr := providers.NewManager(mixedStore, "http://127.0.0.1/auth/callback")
	_, err = mixedMgr.Refresh(context.Background())
	require.NoError(t, err)

	h, _, _ := newTestHandler(t, mixedMgr)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "test-idp")
	assert.NotContains(t, body, "github-contractors",
		"github (OAuth2-only) providers must be filtered until userinfo path lands")
	// Sanity: oidcID is unused in the github path
	_ = oidcID
	_ = mgr
}

func TestLogin_HTMLEscapesUnsafeBrandLabel(t *testing.T) {
	// Configure a provider with an XSS-y BrandLabel; the
	// rendered page must escape it.
	idp := newFakeIdP(t, "client-uuid")
	dsn := "file:" + t.TempDir() + "/xss.db"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	store, err := providers.NewStore(db, base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)
	_, err = store.Save(context.Background(), providers.SaveInput{
		Name:       "evil",
		Type:       providers.TypeZitadel,
		Enabled:    true,
		BrandLabel: `<script>alert('xss')</script>`,
		Config: providers.Config{
			IssuerURL:    idp.URL,
			ClientID:     "client-uuid",
			ClientSecret: "test-secret",
		},
	})
	require.NoError(t, err)
	mgr := providers.NewManager(store, "http://127.0.0.1/auth/callback")
	_, err = mgr.Refresh(context.Background())
	require.NoError(t, err)

	h, _, _ := newTestHandler(t, mgr)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.NotContains(t, body, "<script>alert",
		"BrandLabel must be HTML-escaped by html/template")
	assert.Contains(t, body, "&lt;script&gt;",
		"BrandLabel should appear escape-encoded")
}
