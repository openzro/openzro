// Package auth holds the HTTP surface for centralized,
// openZro-branded login: PKCE flow against the configured
// AuthenticationProviders, sealed state cookie carrying the
// PKCE verifier across the redirect, openZro-issued session JWTs.
//
// See ADR-0005.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	flowExports "github.com/openzro/openzro/management/server/flow_exports"
)

// StateCookieName is the cookie carrying the sealed state across
// the upstream redirect. Path is scoped to /auth so the cookie
// only travels with the OAuth callback request.
const StateCookieName = "oz_auth_state"

// StateCookieTTL is the maximum window between /auth/start and
// /auth/callback. Anything longer signals user abandonment; the
// callback rejects expired states with 400.
const StateCookieTTL = 5 * time.Minute

// stateCookie is the payload sealed into StateCookieName. The
// cookie is HttpOnly + SameSite=Lax + (when serving HTTPS) Secure.
// Sealing prevents tampering; sealing AND HttpOnly together
// prevent the SPA from reading the verifier (PKCE invariant).
type stateCookie struct {
	ProviderID uint64 `json:"provider_id"`

	// CodeVerifier is the PKCE secret. Held server-side via this
	// cookie and never exposed to JavaScript or the upstream IdP
	// until the /auth/callback exchange.
	CodeVerifier string `json:"code_verifier"`

	// URLState is the random value sent to the upstream as the
	// `state` query parameter. The callback verifies that the
	// query string's `state` equals the cookie's URLState — the
	// CSRF binding.
	URLState string `json:"url_state"`

	// Nonce is the OIDC nonce — the upstream echoes it into the
	// id_token, the callback verifies the echo. Defends against
	// id_token replay across sessions.
	Nonce string `json:"nonce"`

	// ReturnTo is the openZro-relative path the user lands on
	// after successful sign-in. Validated against
	// isSafeReturnTo; never an absolute URL.
	ReturnTo string `json:"return_to"`

	IssuedAt int64 `json:"issued_at"`
}

// StateCookieSealer wraps the same AES-256-GCM envelope used by
// flow_exports + mdm + auth/providers. Reusing the envelope keeps
// the threat-model surface to one helper instead of fragmenting
// it across three different ad-hoc encryption schemes.
type StateCookieSealer struct {
	encrypt *flowExports.FieldEncrypt
}

// NewStateCookieSealer takes the same base64-encoded 32-byte key
// that flow_exports + mdm already consume.
func NewStateCookieSealer(key string) (*StateCookieSealer, error) {
	enc, err := flowExports.NewFieldEncrypt(key)
	if err != nil {
		return nil, err
	}
	return &StateCookieSealer{encrypt: enc}, nil
}

// Seal serializes + encrypts the payload into a single
// cookie-safe string (base64 produced by FieldEncrypt). Caller
// stores the result in StateCookieName.
func (s *StateCookieSealer) Seal(c stateCookie) (string, error) {
	if c.IssuedAt == 0 {
		c.IssuedAt = time.Now().Unix()
	}
	plain, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("auth: marshal state: %w", err)
	}
	sealed, err := s.encrypt.Encrypt(plain)
	if err != nil {
		return "", fmt.Errorf("auth: seal state: %w", err)
	}
	return string(sealed), nil
}

// Unseal decrypts and unmarshals the cookie value, then enforces
// the TTL. ErrStateExpired is the typed error operators may want
// to handle separately (404 vs 400, log differently, etc.).
func (s *StateCookieSealer) Unseal(raw string) (stateCookie, error) {
	plain, err := s.encrypt.Decrypt([]byte(raw))
	if err != nil {
		return stateCookie{}, fmt.Errorf("auth: unseal state: %w", err)
	}
	var c stateCookie
	if err := json.Unmarshal(plain, &c); err != nil {
		return stateCookie{}, fmt.Errorf("auth: unmarshal state: %w", err)
	}
	if c.IssuedAt == 0 {
		return stateCookie{}, errors.New("auth: state cookie missing issued_at")
	}
	age := time.Since(time.Unix(c.IssuedAt, 0))
	if age > StateCookieTTL {
		return stateCookie{}, ErrStateExpired
	}
	return c, nil
}

// ErrStateExpired is returned by Unseal when the cookie's age
// exceeds StateCookieTTL. The callback maps it to 400.
var ErrStateExpired = errors.New("auth: state cookie expired")
