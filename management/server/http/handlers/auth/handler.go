package auth

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/openzro/openzro/management/server/auth/providers"
)

// Handler holds the runtime dependencies for the centralized
// login HTTP surface. Constructed once at server boot, registered
// onto the rootRouter (no /api prefix, no auth middleware — these
// endpoints are how a user GETS authenticated).
type Handler struct {
	providers *providers.Manager
	sealer    *StateCookieSealer
	sessions  *SessionService
	renderer  *loginRenderer

	// httpClient is used for OIDC discovery + token-exchange HTTP
	// calls. Tests inject a custom client (httptest); production
	// uses the default 10s-timeout client.
	httpClient *http.Client

	// secureCookies controls the Secure flag on every cookie this
	// handler emits. Off for `http://localhost` development; on
	// for any deployment served over HTTPS.
	secureCookies bool

	// defaultReturnTo is where the callback redirects when no
	// safe `return_to` was supplied. Conventionally /peers — the
	// dashboard's home view after sign-in.
	defaultReturnTo string
}

// HandlerOption configures the Handler at construction.
type HandlerOption func(*Handler)

// WithHTTPClient overrides the http.Client used for upstream
// communication. Tests use this for httptest server injection.
func WithHTTPClient(c *http.Client) HandlerOption {
	return func(h *Handler) { h.httpClient = c }
}

// WithSecureCookies toggles the Secure flag on outgoing cookies.
// Default is true; tests and local-http development pass false.
func WithSecureCookies(v bool) HandlerOption {
	return func(h *Handler) { h.secureCookies = v }
}

// WithDefaultReturnTo overrides the post-login landing path.
func WithDefaultReturnTo(p string) HandlerOption {
	return func(h *Handler) { h.defaultReturnTo = p }
}

// NewHandler wires the auth handler. The sealer + sessions are
// constructed by the caller from the management's data-store
// encryption key (single key reused — same threat model as the
// at-rest envelope).
func NewHandler(mgr *providers.Manager, sealer *StateCookieSealer, sessions *SessionService, opts ...HandlerOption) (*Handler, error) {
	renderer, err := newLoginRenderer()
	if err != nil {
		return nil, err
	}
	h := &Handler{
		providers:       mgr,
		sealer:          sealer,
		sessions:        sessions,
		renderer:        renderer,
		httpClient:      &http.Client{Timeout: 10 * time.Second},
		secureCookies:   true,
		defaultReturnTo: "/peers",
	}
	for _, o := range opts {
		o(h)
	}
	return h, nil
}

// AddEndpoints registers the auth routes onto router. Mount it on
// the rootRouter (no /api prefix, no auth middleware) so the
// browser's redirect from the upstream IdP can reach
// /auth/callback unauthenticated.
func AddEndpoints(h *Handler, router *mux.Router) {
	router.HandleFunc("/login", h.login).Methods(http.MethodGet)
	router.HandleFunc("/auth/start", h.start).Methods(http.MethodGet)
	router.HandleFunc("/auth/callback", h.callback).Methods(http.MethodGet)
	router.HandleFunc("/auth/logout", h.logout).Methods(http.MethodPost)
}

// randomURLString returns n bytes of crypto-random base64-url
// (no padding). Used for PKCE verifier, nonce, URL state.
func randomURLString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// isSafeReturnTo defends against open-redirect: only same-origin
// paths starting with "/" and not "//" (protocol-relative) are
// allowed. Anything else falls back to defaultReturnTo.
func isSafeReturnTo(s string) bool {
	if s == "" || !strings.HasPrefix(s, "/") {
		return false
	}
	if strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/\\") {
		return false
	}
	// Reject CR/LF injection into Location header.
	if strings.ContainsAny(s, "\r\n") {
		return false
	}
	return true
}
