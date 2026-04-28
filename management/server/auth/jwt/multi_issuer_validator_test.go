package jwt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/openzro/openzro/management/server/auth/providers"
)

// signingServer boots an httptest.Server that publishes a single
// RSA public key in its JWKs endpoint and serves an OIDC
// discovery doc whose issuer claim equals the server's URL. The
// returned signer mints tokens with the matching private key so
// tests can produce JWTs that the OIDC verifier (loaded by the
// providers.Manager) actually accepts.
type signingServer struct {
	URL    string
	signer *signer
}

type signer struct {
	key    *rsa.PrivateKey
	issuer string
	kid    string
}

func newSigningServer(t *testing.T) *signingServer {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	n := base64.RawURLEncoding.EncodeToString(priv.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes())
	const kid = "test-key-1"

	var (
		mu  sync.Mutex
		iss string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		current := iss
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                current,
			"authorization_endpoint":                current + "/oauth/authorize",
			"token_endpoint":                        current + "/oauth/token",
			"jwks_uri":                              current + "/jwks",
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
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mu.Lock()
	iss = srv.URL
	mu.Unlock()
	return &signingServer{
		URL:    srv.URL,
		signer: &signer{key: priv, issuer: srv.URL, kid: kid},
	}
}

func (s *signer) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = s.kid
	raw, err := tok.SignedString(s.key)
	require.NoError(t, err)
	return raw
}

func newProvidersStore(t *testing.T) *providers.Store {
	t.Helper()
	dsn := "file:" + t.TempDir() + "/test.db"
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

func setupManager(t *testing.T, issuers ...string) *providers.Manager {
	t.Helper()
	store := newProvidersStore(t)
	ctx := context.Background()
	for i, iss := range issuers {
		_, err := store.Save(ctx, providers.SaveInput{
			Name:    "p" + string(rune('a'+i)),
			Type:    providers.TypeZitadel,
			Enabled: true,
			Config: providers.Config{
				IssuerURL:    iss,
				ClientID:     "client-uuid",
				ClientSecret: "super-secret",
			},
		})
		require.NoError(t, err)
	}
	mgr := providers.NewManager(store, "https://openzro.example.com/auth/callback")
	perRow, err := mgr.Refresh(ctx)
	require.NoError(t, err)
	require.Empty(t, perRow, "all configured issuers should resolve")
	return mgr
}

// fakeFallback records calls and returns a canned response.
type fakeFallback struct {
	called int
	tok    *jwt.Token
	err    error
}

func (f *fakeFallback) ValidateAndParse(ctx context.Context, raw string) (*jwt.Token, error) {
	f.called++
	return f.tok, f.err
}

func TestMultiIssuer_RoutesByIssAndVerifies(t *testing.T) {
	a := newSigningServer(t)
	b := newSigningServer(t)
	mgr := setupManager(t, a.URL, b.URL)
	v := NewMultiIssuerValidator(mgr, nil)
	ctx := context.Background()

	now := time.Now()
	raw := a.signer.sign(t, jwt.MapClaims{
		"iss": a.URL,
		"aud": "client-uuid",
		"sub": "user-1",
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	})

	tok, err := v.ValidateAndParse(ctx, raw)
	require.NoError(t, err)
	assert.True(t, tok.Valid)
	claims := tok.Claims.(jwt.MapClaims)
	assert.Equal(t, "user-1", claims["sub"])
	assert.Equal(t, a.URL, claims["iss"])
}

func TestMultiIssuer_RejectsTokenSignedByDifferentKey(t *testing.T) {
	// Stand up two signing servers but only register issuer A
	// with the manager. A token signed by B that *claims* iss=A
	// must fail verification because A's JWKs don't have B's key.
	a := newSigningServer(t)
	b := newSigningServer(t)
	mgr := setupManager(t, a.URL)
	v := NewMultiIssuerValidator(mgr, nil)
	ctx := context.Background()

	now := time.Now()
	raw := b.signer.sign(t, jwt.MapClaims{
		"iss": a.URL, // claim the wrong issuer
		"aud": "client-uuid",
		"sub": "attacker",
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	})

	_, err := v.ValidateAndParse(ctx, raw)
	require.Error(t, err)
	assert.ErrorIs(t, err, errTokenInvalid)
}

func TestMultiIssuer_RejectsExpiredToken(t *testing.T) {
	a := newSigningServer(t)
	mgr := setupManager(t, a.URL)
	v := NewMultiIssuerValidator(mgr, nil)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	raw := a.signer.sign(t, jwt.MapClaims{
		"iss": a.URL,
		"aud": "client-uuid",
		"sub": "user",
		"iat": past.Add(-time.Hour).Unix(),
		"exp": past.Unix(),
	})

	_, err := v.ValidateAndParse(ctx, raw)
	require.Error(t, err)
	assert.ErrorIs(t, err, errTokenInvalid)
}

func TestMultiIssuer_RejectsWrongAudience(t *testing.T) {
	a := newSigningServer(t)
	mgr := setupManager(t, a.URL)
	v := NewMultiIssuerValidator(mgr, nil)
	ctx := context.Background()

	now := time.Now()
	raw := a.signer.sign(t, jwt.MapClaims{
		"iss": a.URL,
		"aud": "wrong-client",
		"sub": "user",
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	})

	_, err := v.ValidateAndParse(ctx, raw)
	require.Error(t, err)
	assert.ErrorIs(t, err, errTokenInvalid)
}

func TestMultiIssuer_UnknownIssuerNoLegacy(t *testing.T) {
	a := newSigningServer(t)
	mgr := setupManager(t, a.URL)
	v := NewMultiIssuerValidator(mgr, nil)
	ctx := context.Background()

	now := time.Now()
	raw := a.signer.sign(t, jwt.MapClaims{
		"iss": "https://unknown-issuer.example.com",
		"aud": "client-uuid",
		"sub": "user",
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	})

	_, err := v.ValidateAndParse(ctx, raw)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownIssuer)
}

func TestMultiIssuer_UnknownIssuerFallsBackToLegacy(t *testing.T) {
	a := newSigningServer(t)
	mgr := setupManager(t, a.URL)
	expected := &jwt.Token{Valid: true, Claims: jwt.MapClaims{"sub": "legacy-user"}}
	legacy := &fakeFallback{tok: expected}
	v := NewMultiIssuerValidator(mgr, legacy)
	ctx := context.Background()

	now := time.Now()
	raw := a.signer.sign(t, jwt.MapClaims{
		"iss": "https://legacy-issuer.example.com",
		"aud": "any",
		"sub": "user",
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	})

	tok, err := v.ValidateAndParse(ctx, raw)
	require.NoError(t, err)
	assert.Equal(t, expected, tok)
	assert.Equal(t, 1, legacy.called)
}

func TestMultiIssuer_KnownIssuerSkipsLegacy(t *testing.T) {
	a := newSigningServer(t)
	mgr := setupManager(t, a.URL)
	legacy := &fakeFallback{err: errors.New("legacy must not be called")}
	v := NewMultiIssuerValidator(mgr, legacy)
	ctx := context.Background()

	now := time.Now()
	raw := a.signer.sign(t, jwt.MapClaims{
		"iss": a.URL,
		"aud": "client-uuid",
		"sub": "user",
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	})

	_, err := v.ValidateAndParse(ctx, raw)
	require.NoError(t, err)
	assert.Equal(t, 0, legacy.called,
		"known-issuer path must not consult the fallback")
}

func TestMultiIssuer_EmptyToken(t *testing.T) {
	v := NewMultiIssuerValidator(setupManager(t), nil)
	_, err := v.ValidateAndParse(context.Background(), "")
	assert.ErrorIs(t, err, errTokenEmpty)
}

func TestMultiIssuer_MalformedTokenNoLegacy(t *testing.T) {
	v := NewMultiIssuerValidator(setupManager(t), nil)
	_, err := v.ValidateAndParse(context.Background(), "not.a.jwt")
	require.Error(t, err)
	assert.ErrorIs(t, err, errTokenParsing)
}

func TestMultiIssuer_MalformedTokenFallsBackToLegacy(t *testing.T) {
	legacy := &fakeFallback{tok: &jwt.Token{Valid: true}}
	v := NewMultiIssuerValidator(setupManager(t), legacy)
	_, err := v.ValidateAndParse(context.Background(), "not.a.jwt")
	require.NoError(t, err)
	assert.Equal(t, 1, legacy.called)
}

func TestMultiIssuer_GitHubProviderRejected(t *testing.T) {
	// A configured GitHub provider has Verifier=nil — the
	// multi-issuer path can't verify ID tokens for it. Such
	// tokens (if any are claimed) must be rejected, not silently
	// fall through to legacy: the iss matched a known provider.
	store := newProvidersStore(t)
	ctx := context.Background()
	_, err := store.Save(ctx, providers.SaveInput{
		Name:    "github",
		Type:    providers.TypeGitHub,
		Enabled: true,
		Config: providers.Config{
			IssuerURL:    "https://github.com",
			ClientID:     "client-uuid",
			ClientSecret: "super-secret",
		},
	})
	require.NoError(t, err)
	mgr := providers.NewManager(store, "https://openzro.example.com/auth/callback")
	_, err = mgr.Refresh(ctx)
	require.NoError(t, err)

	legacy := &fakeFallback{err: errors.New("legacy must not be called")}
	v := NewMultiIssuerValidator(mgr, legacy)

	// Construct a token claiming iss=github. We don't need it to
	// be properly signed — the iss-lookup happens before the
	// crypto check, and the validator returns errTokenInvalid
	// before reaching the verifier (which is nil anyway).
	a := newSigningServer(t)
	now := time.Now()
	raw := a.signer.sign(t, jwt.MapClaims{
		"iss": "https://github.com",
		"aud": "client-uuid",
		"sub": "user",
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	})

	_, err = v.ValidateAndParse(context.Background(), raw)
	require.Error(t, err)
	assert.ErrorIs(t, err, errTokenInvalid)
	assert.Equal(t, 0, legacy.called,
		"matched provider with nil verifier must NOT fall through to legacy")
}

func TestUnsafeExtractIssuer(t *testing.T) {
	// Round-trip: produce a token with iss=X, extract iss without
	// verifying, expect X. Signature is irrelevant here.
	a := newSigningServer(t)
	now := time.Now()
	raw := a.signer.sign(t, jwt.MapClaims{
		"iss": a.URL,
		"sub": "user",
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	})

	iss, err := unsafeExtractIssuer(raw)
	require.NoError(t, err)
	assert.Equal(t, a.URL, iss)
}

func TestUnsafeExtractIssuer_MissingClaim(t *testing.T) {
	a := newSigningServer(t)
	now := time.Now()
	raw := a.signer.sign(t, jwt.MapClaims{
		"sub": "user",
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	})

	_, err := unsafeExtractIssuer(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing iss")
}

func TestUnsafeExtractIssuer_Malformed(t *testing.T) {
	_, err := unsafeExtractIssuer("not.a.jwt")
	require.Error(t, err)
}
