package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/openzro/openzro/management/server/auth/providers"
)

// fakeIdP is a complete OIDC-ish upstream: discovery + JWKs +
// token-exchange. Tests pre-register codes via IssueCode; the
// /oauth/token handler pops the code and returns a freshly
// signed id_token bound to the registered claims.
type fakeIdP struct {
	URL string
	srv *httptest.Server
	key *rsa.PrivateKey
	kid string

	clientID string

	mu    sync.Mutex
	codes map[string]issuedCode
}

type issuedCode struct {
	sub   string
	email string
	name  string
	nonce string
}

func newFakeIdP(t *testing.T, clientID string) *fakeIdP {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	const kid = "test-key-1"
	n := base64.RawURLEncoding.EncodeToString(priv.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes())

	idp := &fakeIdP{
		key:      priv,
		kid:      kid,
		clientID: clientID,
		codes:    map[string]issuedCode{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                idp.URL,
			"authorization_endpoint":                idp.URL + "/oauth/authorize",
			"token_endpoint":                        idp.URL + "/oauth/token",
			"jwks_uri":                              idp.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []any{
				map[string]any{
					"kty": "RSA", "alg": "RS256", "use": "sig",
					"kid": kid, "n": n, "e": e,
				},
			},
		})
	})
	mux.HandleFunc("/oauth/token", idp.handleToken)
	idp.srv = httptest.NewServer(mux)
	idp.URL = idp.srv.URL
	t.Cleanup(idp.srv.Close)
	return idp
}

func (f *fakeIdP) issueCode(code string, c issuedCode) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes[code] = c
}

func (f *fakeIdP) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	code := r.Form.Get("code")
	f.mu.Lock()
	issued, ok := f.codes[code]
	delete(f.codes, code)
	f.mu.Unlock()
	if !ok {
		http.Error(w, "invalid_grant", 400)
		return
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss":   f.URL,
		"sub":   issued.sub,
		"aud":   f.clientID,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
		"email": issued.email,
		"name":  issued.name,
		"nonce": issued.nonce,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = f.kid
	idToken, err := tok.SignedString(f.key)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "fake-access-token",
		"id_token":     idToken,
		"token_type":   "Bearer",
		"expires_in":   3600,
	})
}

// --- shared helpers ----------------------------------------------------

func newTestProviders(t *testing.T, idp *fakeIdP, clientID string) (*providers.Manager, uint64) {
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

	row, err := store.Save(context.Background(), providers.SaveInput{
		Name:    "test-idp",
		Type:    providers.TypeZitadel,
		Enabled: true,
		Config: providers.Config{
			IssuerURL:    idp.URL,
			ClientID:     clientID,
			ClientSecret: "test-secret",
		},
	})
	require.NoError(t, err)

	mgr := providers.NewManager(store, "http://127.0.0.1/auth/callback")
	_, err = mgr.Refresh(context.Background())
	require.NoError(t, err)
	return mgr, row.ID
}

func newTestHandler(t *testing.T, mgr *providers.Manager) (*Handler, *StateCookieSealer, *SessionService) {
	t.Helper()
	sealer := newTestSealer(t)
	sessions := newTestSession(t)
	h := NewHandler(mgr, sealer, sessions, WithSecureCookies(false), WithDefaultReturnTo("/peers"))
	return h, sealer, sessions
}

func routerFor(h *Handler) *mux.Router {
	r := mux.NewRouter()
	AddEndpoints(h, r)
	return r
}

// --- start handler -----------------------------------------------------

func TestStart_RedirectsToUpstream(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	mgr, providerID := newTestProviders(t, idp, "client-uuid")
	h, _, _ := newTestHandler(t, mgr)

	req := httptest.NewRequest(http.MethodGet,
		"/auth/start?provider="+strconv.FormatUint(providerID, 10)+"&return_to=/peers",
		nil)
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)

	require.Equal(t, http.StatusFound, rr.Code, "body=%s", rr.Body.String())
	loc := rr.Header().Get("Location")
	require.NotEmpty(t, loc)
	u, err := url.Parse(loc)
	require.NoError(t, err)
	assert.Equal(t, idp.URL, u.Scheme+"://"+u.Host)
	q := u.Query()
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.NotEmpty(t, q.Get("code_challenge"))
	assert.NotEmpty(t, q.Get("state"))
	assert.NotEmpty(t, q.Get("nonce"))
	assert.Equal(t, "client-uuid", q.Get("client_id"))

	// State cookie must be set, HttpOnly, scoped to /auth.
	var c *http.Cookie
	for _, cc := range rr.Result().Cookies() {
		if cc.Name == StateCookieName {
			c = cc
		}
	}
	require.NotNil(t, c, "state cookie missing")
	assert.True(t, c.HttpOnly)
	assert.Equal(t, "/auth", c.Path)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
}

func TestStart_RejectsUnknownProvider(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	mgr, _ := newTestProviders(t, idp, "client-uuid")
	h, _, _ := newTestHandler(t, mgr)

	req := httptest.NewRequest(http.MethodGet, "/auth/start?provider=9999", nil)
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestStart_RejectsMissingProvider(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	mgr, _ := newTestProviders(t, idp, "client-uuid")
	h, _, _ := newTestHandler(t, mgr)

	req := httptest.NewRequest(http.MethodGet, "/auth/start", nil)
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestStart_FallsBackToDefaultReturnToOnUnsafeInput(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	mgr, providerID := newTestProviders(t, idp, "client-uuid")
	h, sealer, _ := newTestHandler(t, mgr)

	req := httptest.NewRequest(http.MethodGet,
		"/auth/start?provider="+strconv.FormatUint(providerID, 10)+"&return_to=https://evil.example.com",
		nil)
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)
	require.Equal(t, http.StatusFound, rr.Code)

	// Inspect the cookie to confirm the unsafe return_to was scrubbed.
	var raw string
	for _, cc := range rr.Result().Cookies() {
		if cc.Name == StateCookieName {
			raw = cc.Value
		}
	}
	require.NotEmpty(t, raw)
	state, err := sealer.Unseal(raw)
	require.NoError(t, err)
	assert.Equal(t, "/peers", state.ReturnTo)
}

// --- callback handler --------------------------------------------------

func TestCallback_HappyPath(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	mgr, providerID := newTestProviders(t, idp, "client-uuid")
	h, sealer, sessions := newTestHandler(t, mgr)

	idp.issueCode("test-code", issuedCode{
		sub:   "upstream-user-1",
		email: "u@example.com",
		name:  "Test User",
		nonce: "test-nonce",
	})

	sealed, err := sealer.Seal(stateCookie{
		ProviderID:   providerID,
		CodeVerifier: "test-verifier",
		URLState:     "test-url-state",
		Nonce:        "test-nonce",
		ReturnTo:     "/dashboard",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet,
		"/auth/callback?code=test-code&state=test-url-state", nil)
	req.AddCookie(&http.Cookie{Name: StateCookieName, Value: sealed})
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)

	require.Equal(t, http.StatusFound, rr.Code, "body=%s", rr.Body.String())
	assert.Equal(t, "/dashboard", rr.Header().Get("Location"))

	var (
		sessionCookie *http.Cookie
		clearedState  *http.Cookie
	)
	for _, cc := range rr.Result().Cookies() {
		switch cc.Name {
		case SessionCookieName:
			sessionCookie = cc
		case StateCookieName:
			clearedState = cc
		}
	}
	require.NotNil(t, sessionCookie, "session cookie not set")
	require.NotNil(t, clearedState, "state cookie not cleared")
	assert.Less(t, clearedState.MaxAge, 0, "state cookie must be expired")

	claims, err := sessions.Verify(sessionCookie.Value)
	require.NoError(t, err)
	assert.Equal(t, "u@example.com", claims.Email)
	assert.Equal(t, "Test User", claims.Name)
	assert.Equal(t, providerID, claims.ProviderID)
	assert.Equal(t, idp.URL, claims.UpstreamIss)
	assert.Equal(t, "upstream-user-1", claims.UpstreamSub)
}

func TestCallback_RejectsCSRFMismatch(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	mgr, providerID := newTestProviders(t, idp, "client-uuid")
	h, sealer, _ := newTestHandler(t, mgr)

	sealed, err := sealer.Seal(stateCookie{
		ProviderID:   providerID,
		CodeVerifier: "v",
		URLState:     "expected-state",
		Nonce:        "n",
		ReturnTo:     "/peers",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet,
		"/auth/callback?code=x&state=different-state", nil)
	req.AddCookie(&http.Cookie{Name: StateCookieName, Value: sealed})
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCallback_RejectsExpiredState(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	mgr, providerID := newTestProviders(t, idp, "client-uuid")
	h, sealer, _ := newTestHandler(t, mgr)

	sealed, err := sealer.Seal(stateCookie{
		ProviderID:   providerID,
		CodeVerifier: "v",
		URLState:     "u",
		Nonce:        "n",
		ReturnTo:     "/peers",
		IssuedAt:     time.Now().Add(-StateCookieTTL - time.Second).Unix(),
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet,
		"/auth/callback?code=x&state=u", nil)
	req.AddCookie(&http.Cookie{Name: StateCookieName, Value: sealed})
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "expired")
}

func TestCallback_RejectsMissingStateCookie(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	mgr, _ := newTestProviders(t, idp, "client-uuid")
	h, _, _ := newTestHandler(t, mgr)

	req := httptest.NewRequest(http.MethodGet,
		"/auth/callback?code=x&state=u", nil)
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCallback_SurfacesUpstreamError(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	mgr, _ := newTestProviders(t, idp, "client-uuid")
	h, _, _ := newTestHandler(t, mgr)

	req := httptest.NewRequest(http.MethodGet,
		"/auth/callback?error=access_denied", nil)
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "access_denied")
}

func TestCallback_RejectsNonceMismatch(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	mgr, providerID := newTestProviders(t, idp, "client-uuid")
	h, sealer, _ := newTestHandler(t, mgr)

	idp.issueCode("test-code", issuedCode{
		sub:   "user",
		nonce: "upstream-says-this-nonce", // not what's in the cookie
	})

	sealed, err := sealer.Seal(stateCookie{
		ProviderID:   providerID,
		CodeVerifier: "v",
		URLState:     "u",
		Nonce:        "cookie-says-this-nonce",
		ReturnTo:     "/peers",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet,
		"/auth/callback?code=test-code&state=u", nil)
	req.AddCookie(&http.Cookie{Name: StateCookieName, Value: sealed})
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "nonce")
}

// --- logout handler ----------------------------------------------------

func TestLogout_ClearsSessionCookie(t *testing.T) {
	idp := newFakeIdP(t, "client-uuid")
	mgr, _ := newTestProviders(t, idp, "client-uuid")
	h, _, _ := newTestHandler(t, mgr)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	rr := httptest.NewRecorder()
	routerFor(h).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	var c *http.Cookie
	for _, cc := range rr.Result().Cookies() {
		if cc.Name == SessionCookieName {
			c = cc
		}
	}
	require.NotNil(t, c)
	assert.Less(t, c.MaxAge, 0)
	assert.Empty(t, c.Value)
}

// --- helpers -----------------------------------------------------------

func TestIsSafeReturnTo(t *testing.T) {
	cases := []struct {
		in   string
		safe bool
	}{
		{"/peers", true},
		{"/peers?tab=overview", true},
		{"", false},
		{"//evil.example.com/peers", false},
		{"/\\evil", false},
		{"https://evil.example.com", false},
		{"javascript:alert(1)", false},
		{"/peers\r\nX-Injected: 1", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			assert.Equal(t, c.safe, isSafeReturnTo(c.in))
		})
	}
}
