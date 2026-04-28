package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

// ErrVerifierMissing is returned by VerifyIDToken when the
// LiveProvider doesn't expose an OIDC ID-token verifier (typically
// GitHub OAuth, which validates the user via the userinfo
// endpoint instead). The PKCE callback handler (PR 4) catches this
// and switches to the userinfo path.
var ErrVerifierMissing = errors.New("providers: id-token verification not supported by this provider")

// LiveProvider is one decrypted, runtime-ready provider snapshot.
// Held in the Manager's cache; treat as read-only after Refresh.
type LiveProvider struct {
	Row    AuthenticationProvider
	Config Config

	// OIDC is the discovered OIDC provider. nil for OAuth2-only
	// types (GitHub) or when discovery was skipped because the
	// operator supplied explicit endpoints.
	OIDC *oidc.Provider

	// Verifier validates ID tokens issued by OIDC. nil when OIDC is
	// nil — callers should fall back to the userinfo endpoint.
	Verifier *oidc.IDTokenVerifier

	// OAuth2 is always populated. The PKCE flow (PR 4) calls
	// AuthCodeURL and Exchange against this.
	OAuth2 *oauth2.Config
}

// VerifyIDToken runs the upstream ID-token verifier. Returns
// ErrVerifierMissing for OAuth2-only providers; callers handle
// that by switching to the userinfo endpoint.
func (l *LiveProvider) VerifyIDToken(ctx context.Context, raw string) (*oidc.IDToken, error) {
	if l.Verifier == nil {
		return nil, ErrVerifierMissing
	}
	return l.Verifier.Verify(ctx, raw)
}

// ProviderError is a per-row failure during Refresh. Non-fatal:
// surviving providers keep working. The admin API (PR 6) surfaces
// these to the dashboard so operators see why a provider is
// missing from the /login page.
type ProviderError struct {
	ID   uint64
	Name string
	Err  error
}

func (e ProviderError) Error() string {
	return fmt.Sprintf("provider %d (%s): %v", e.ID, e.Name, e.Err)
}

func (e ProviderError) Unwrap() error { return e.Err }

// Manager builds and caches LiveProvider objects from Store rows.
// Refresh() is called at boot and after every admin API mutation
// so the /login flow picks up changes without a restart.
type Manager struct {
	store       *Store
	redirectURL string
	httpClient  *http.Client

	mu       sync.RWMutex
	byID     map[uint64]*LiveProvider
	byIssuer map[string]*LiveProvider
}

// ManagerOption configures a Manager at construction time.
type ManagerOption func(*Manager)

// WithHTTPClient overrides the http.Client used for OIDC discovery
// and JWK fetches. Tests inject httptest servers via this; in
// production the default 10s-timeout client is fine.
func WithHTTPClient(c *http.Client) ManagerOption {
	return func(m *Manager) { m.httpClient = c }
}

// NewManager wires a Manager. RedirectURL is the openZro
// /auth/callback URL; it is embedded into every oauth2.Config so
// all providers redirect back to the same path.
func NewManager(store *Store, redirectURL string, opts ...ManagerOption) *Manager {
	m := &Manager{
		store:       store,
		redirectURL: redirectURL,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		byID:        map[uint64]*LiveProvider{},
		byIssuer:    map[string]*LiveProvider{},
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Refresh re-reads enabled rows from the Store and rebuilds the
// cache atomically (build under no lock, swap under write lock).
// Per-row build failures are non-fatal — they're returned as
// ProviderError and the surviving providers stay live.
//
// The fatal error return is reserved for Store.ListEnabled
// failures (DB unreachable, etc.).
func (m *Manager) Refresh(ctx context.Context) ([]ProviderError, error) {
	rows, err := m.store.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("providers: refresh list: %w", err)
	}

	nextByID := make(map[uint64]*LiveProvider, len(rows))
	nextByIssuer := make(map[string]*LiveProvider, len(rows))
	var perRow []ProviderError

	// Inject our http.Client into go-oidc's discovery + JWK fetch.
	discoveryCtx := oidc.ClientContext(ctx, m.httpClient)

	for i := range rows {
		row := rows[i]
		live, err := m.build(discoveryCtx, &row)
		if err != nil {
			log.WithContext(ctx).
				WithField("provider_id", row.ID).
				WithField("provider_name", row.Name).
				Errorf("providers: build: %v", err)
			perRow = append(perRow, ProviderError{ID: row.ID, Name: row.Name, Err: err})
			continue
		}
		nextByID[row.ID] = live
		if live.Config.IssuerURL != "" {
			nextByIssuer[live.Config.IssuerURL] = live
		}
	}

	m.mu.Lock()
	m.byID = nextByID
	m.byIssuer = nextByIssuer
	m.mu.Unlock()
	return perRow, nil
}

func (m *Manager) build(ctx context.Context, row *AuthenticationProvider) (*LiveProvider, error) {
	cfg, err := m.store.Decrypt(row)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	live := &LiveProvider{Row: *row, Config: *cfg}

	// Decide whether to attempt OIDC discovery. GitHub OAuth has no
	// discovery doc; explicit endpoints in Config also bypass it
	// (operator already filled the form for a non-discovery IdP).
	explicitEndpoints := cfg.AuthorizationEndpoint != "" && cfg.TokenEndpoint != ""
	skipDiscovery := row.Type == TypeGitHub || cfg.IssuerURL == "" || explicitEndpoints

	if !skipDiscovery {
		prov, err := oidc.NewProvider(ctx, cfg.IssuerURL)
		if err != nil {
			return nil, fmt.Errorf("oidc discovery: %w", err)
		}
		live.OIDC = prov
		live.Verifier = prov.Verifier(&oidc.Config{ClientID: cfg.ClientID})
		live.OAuth2 = &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  m.redirectURL,
			Endpoint:     prov.Endpoint(),
			Scopes:       defaultScopes(row.Type, cfg.Scopes),
		}
		return live, nil
	}

	endpoint := oauth2.Endpoint{
		AuthURL:  cfg.AuthorizationEndpoint,
		TokenURL: cfg.TokenEndpoint,
	}
	// Built-in defaults for GitHub OAuth — operator can still
	// override via Config.AuthorizationEndpoint / TokenEndpoint.
	if row.Type == TypeGitHub && endpoint.AuthURL == "" {
		endpoint = oauth2.Endpoint{
			AuthURL:  "https://github.com/login/oauth/authorize",
			TokenURL: "https://github.com/login/oauth/access_token",
		}
	}
	if endpoint.AuthURL == "" || endpoint.TokenURL == "" {
		return nil, errors.New("config missing authorization_endpoint or token_endpoint")
	}
	live.OAuth2 = &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  m.redirectURL,
		Endpoint:     endpoint,
		Scopes:       defaultScopes(row.Type, cfg.Scopes),
	}
	return live, nil
}

func defaultScopes(t ProviderType, configured []string) []string {
	if len(configured) > 0 {
		return configured
	}
	// GitHub OAuth doesn't use the openid scope; the others all do.
	if t == TypeGitHub {
		return []string{"read:user", "user:email"}
	}
	return []string{oidc.ScopeOpenID, "profile", "email"}
}

// GetByID returns the cached LiveProvider for an ID. The /auth/
// start handler uses this to look up the provider the user picked.
func (m *Manager) GetByID(id uint64) (*LiveProvider, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.byID[id]
	return p, ok
}

// GetByIssuer looks up a provider by issuer URL. PR 3's
// MultiIssuerValidator uses this to route ID tokens by `iss`
// claim, avoiding the cost of trying every verifier.
func (m *Manager) GetByIssuer(iss string) (*LiveProvider, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.byIssuer[iss]
	return p, ok
}

// ListEnabled returns the cached providers in stable ID order.
// The /login page renders one button per entry; PR 3's validator
// initialisation iterates this to populate verifiers.
func (m *Manager) ListEnabled() []*LiveProvider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*LiveProvider, 0, len(m.byID))
	for _, p := range m.byID {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Row.ID < out[j].Row.ID })
	return out
}
