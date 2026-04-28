package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeOIDCServer boots an httptest.Server that serves an OIDC
// discovery document and an empty JWKs set. The discovery doc's
// `issuer` claim matches the server's URL — go-oidc enforces that
// match strictly.
func fakeOIDCServer(t *testing.T) string {
	t.Helper()
	var (
		mu     sync.Mutex
		issuer string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		iss := issuer
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                iss,
			"authorization_endpoint":                iss + "/oauth/authorize",
			"token_endpoint":                        iss + "/oauth/token",
			"jwks_uri":                              iss + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mu.Lock()
	issuer = srv.URL
	mu.Unlock()
	return srv.URL
}

func newTestManager(t *testing.T, redirectURL string) (*Manager, *Store) {
	t.Helper()
	s := newTestStore(t)
	m := NewManager(s, redirectURL, WithHTTPClient(&http.Client{}))
	return m, s
}

func TestManager_RefreshOIDCDiscovery(t *testing.T) {
	issuer := fakeOIDCServer(t)
	m, s := newTestManager(t, "https://openzro.example.com/auth/callback")
	ctx := context.Background()

	in := validInput()
	in.Config.IssuerURL = issuer
	in.Config.AuthorizationEndpoint = ""
	in.Config.TokenEndpoint = ""
	row, err := s.Save(ctx, in)
	require.NoError(t, err)

	perRow, err := m.Refresh(ctx)
	require.NoError(t, err)
	assert.Empty(t, perRow)

	live, ok := m.GetByID(row.ID)
	require.True(t, ok)
	assert.NotNil(t, live.OIDC)
	assert.NotNil(t, live.Verifier)
	require.NotNil(t, live.OAuth2)
	assert.Equal(t, issuer+"/oauth/authorize", live.OAuth2.Endpoint.AuthURL)
	assert.Equal(t, issuer+"/oauth/token", live.OAuth2.Endpoint.TokenURL)
	assert.Equal(t, "https://openzro.example.com/auth/callback", live.OAuth2.RedirectURL)
	assert.Equal(t, []string{"openid", "profile", "email"}, live.OAuth2.Scopes)

	byIss, ok := m.GetByIssuer(issuer)
	require.True(t, ok)
	assert.Equal(t, row.ID, byIss.Row.ID)
}

func TestManager_RefreshSkipsBrokenDiscovery(t *testing.T) {
	issuer := fakeOIDCServer(t)
	m, s := newTestManager(t, "https://openzro.example.com/auth/callback")
	ctx := context.Background()

	good := validInput()
	good.Name = "good"
	good.Config.IssuerURL = issuer
	good.Config.AuthorizationEndpoint = ""
	good.Config.TokenEndpoint = ""
	goodRow, err := s.Save(ctx, good)
	require.NoError(t, err)

	bad := validInput()
	bad.Name = "broken"
	bad.Config.IssuerURL = "http://127.0.0.1:1/never-listening"
	bad.Config.AuthorizationEndpoint = ""
	bad.Config.TokenEndpoint = ""
	badRow, err := s.Save(ctx, bad)
	require.NoError(t, err)

	perRow, err := m.Refresh(ctx)
	require.NoError(t, err)
	require.Len(t, perRow, 1, "broken provider should report an error")
	assert.Equal(t, badRow.ID, perRow[0].ID)
	assert.Equal(t, "broken", perRow[0].Name)
	assert.Contains(t, perRow[0].Error(), "broken")

	_, ok := m.GetByID(goodRow.ID)
	assert.True(t, ok, "good provider must stay in cache")
	_, ok = m.GetByID(badRow.ID)
	assert.False(t, ok, "broken provider must not appear in cache")
}

func TestManager_RefreshFiltersDisabled(t *testing.T) {
	issuer := fakeOIDCServer(t)
	m, s := newTestManager(t, "https://openzro.example.com/auth/callback")
	ctx := context.Background()

	in := validInput()
	in.Name = "disabled"
	in.Enabled = false
	in.Config.IssuerURL = issuer
	in.Config.AuthorizationEndpoint = ""
	in.Config.TokenEndpoint = ""
	_, err := s.Save(ctx, in)
	require.NoError(t, err)

	perRow, err := m.Refresh(ctx)
	require.NoError(t, err)
	assert.Empty(t, perRow)
	assert.Empty(t, m.ListEnabled(), "disabled rows must not load into cache")
}

func TestManager_GitHubDefaults(t *testing.T) {
	m, s := newTestManager(t, "https://openzro.example.com/auth/callback")
	ctx := context.Background()

	in := validInput()
	in.Name = "github-contractors"
	in.Type = TypeGitHub
	in.Config.IssuerURL = "https://github.com"
	in.Config.AuthorizationEndpoint = ""
	in.Config.TokenEndpoint = ""
	in.Config.Scopes = nil
	row, err := s.Save(ctx, in)
	require.NoError(t, err)

	perRow, err := m.Refresh(ctx)
	require.NoError(t, err)
	assert.Empty(t, perRow)

	live, ok := m.GetByID(row.ID)
	require.True(t, ok)
	assert.Nil(t, live.OIDC, "github type must skip OIDC discovery")
	assert.Nil(t, live.Verifier)
	require.NotNil(t, live.OAuth2)
	assert.Equal(t, "https://github.com/login/oauth/authorize", live.OAuth2.Endpoint.AuthURL)
	assert.Equal(t, "https://github.com/login/oauth/access_token", live.OAuth2.Endpoint.TokenURL)
	assert.Equal(t, []string{"read:user", "user:email"}, live.OAuth2.Scopes)
}

func TestManager_ExplicitEndpointsSkipDiscovery(t *testing.T) {
	m, s := newTestManager(t, "https://openzro.example.com/auth/callback")
	ctx := context.Background()

	in := validInput()
	in.Type = TypeGeneric
	in.Config.IssuerURL = "http://127.0.0.1:1/never-listening"
	in.Config.AuthorizationEndpoint = "https://example.com/oauth/authorize"
	in.Config.TokenEndpoint = "https://example.com/oauth/token"
	row, err := s.Save(ctx, in)
	require.NoError(t, err)

	perRow, err := m.Refresh(ctx)
	require.NoError(t, err)
	require.Empty(t, perRow,
		"explicit endpoints must skip discovery (no live discovery attempt against the bogus issuer)")

	live, ok := m.GetByID(row.ID)
	require.True(t, ok)
	assert.Nil(t, live.OIDC)
	assert.Equal(t, "https://example.com/oauth/authorize", live.OAuth2.Endpoint.AuthURL)
	assert.Equal(t, "https://example.com/oauth/token", live.OAuth2.Endpoint.TokenURL)
}

func TestManager_ListEnabledOrdered(t *testing.T) {
	issuerA := fakeOIDCServer(t)
	issuerB := fakeOIDCServer(t)
	m, s := newTestManager(t, "https://openzro.example.com/auth/callback")
	ctx := context.Background()

	a := validInput()
	a.Name = "first"
	a.Config.IssuerURL = issuerA
	a.Config.AuthorizationEndpoint = ""
	a.Config.TokenEndpoint = ""
	rowA, err := s.Save(ctx, a)
	require.NoError(t, err)

	b := validInput()
	b.Name = "second"
	b.Config.IssuerURL = issuerB
	b.Config.AuthorizationEndpoint = ""
	b.Config.TokenEndpoint = ""
	rowB, err := s.Save(ctx, b)
	require.NoError(t, err)

	_, err = m.Refresh(ctx)
	require.NoError(t, err)

	list := m.ListEnabled()
	require.Len(t, list, 2)
	assert.Equal(t, rowA.ID, list[0].Row.ID)
	assert.Equal(t, rowB.ID, list[1].Row.ID)
}

func TestManager_DefaultScopes(t *testing.T) {
	cases := []struct {
		name string
		typ  ProviderType
		in   []string
		want []string
	}{
		{"explicit-overrides", TypeGoogle, []string{"openid", "custom"}, []string{"openid", "custom"}},
		{"google-default", TypeGoogle, nil, []string{"openid", "profile", "email"}},
		{"generic-default", TypeGeneric, nil, []string{"openid", "profile", "email"}},
		{"github-default", TypeGitHub, nil, []string{"read:user", "user:email"}},
		{"github-explicit", TypeGitHub, []string{"repo"}, []string{"repo"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, defaultScopes(tc.typ, tc.in))
		})
	}
}

func TestManager_VerifyIDTokenMissing(t *testing.T) {
	live := &LiveProvider{}
	_, err := live.VerifyIDToken(context.Background(), "irrelevant-token")
	assert.ErrorIs(t, err, ErrVerifierMissing)
}

func TestManager_RefreshReplacesPreviousCache(t *testing.T) {
	issuer := fakeOIDCServer(t)
	m, s := newTestManager(t, "https://openzro.example.com/auth/callback")
	ctx := context.Background()

	in := validInput()
	in.Config.IssuerURL = issuer
	in.Config.AuthorizationEndpoint = ""
	in.Config.TokenEndpoint = ""
	row, err := s.Save(ctx, in)
	require.NoError(t, err)

	_, err = m.Refresh(ctx)
	require.NoError(t, err)
	_, ok := m.GetByID(row.ID)
	require.True(t, ok)

	in.ID = row.ID
	in.Enabled = false
	_, err = s.Save(ctx, in)
	require.NoError(t, err)

	_, err = m.Refresh(ctx)
	require.NoError(t, err)
	_, ok = m.GetByID(row.ID)
	assert.False(t, ok, "Refresh must drop now-disabled rows from cache")
}

func TestManager_GetByIssuerMissing(t *testing.T) {
	m, _ := newTestManager(t, "https://openzro.example.com/auth/callback")
	_, ok := m.GetByIssuer("https://does-not-exist.example.com")
	assert.False(t, ok)
}

func TestManager_ProviderErrorUnwrap(t *testing.T) {
	inner := assert.AnError
	pe := ProviderError{ID: 7, Name: "x", Err: inner}
	assert.ErrorIs(t, pe, inner, "ProviderError must unwrap to the underlying error")
	assert.Contains(t, pe.Error(), "provider 7 (x)")
}
