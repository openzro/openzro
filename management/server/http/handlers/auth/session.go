package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SessionCookieName carries the openZro-issued session JWT. The
// dashboard's API calls go out with this cookie; PR 6's middleware
// will recognize it as the primary auth signal.
const SessionCookieName = "oz_session"

// SessionTTL is the lifetime of the openZro session JWT. Short on
// purpose: the refresh cookie (RefreshCookieName) extends the
// session without re-prompting the user upstream.
const SessionTTL = 1 * time.Hour

// SessionIssuer name embedded as the JWT `iss` claim. Distinct
// from any upstream IdP so MultiIssuerValidator's iss-based
// routing never confuses a session token with an upstream token.
const SessionIssuer = "openzro-management"

// SessionAudience is the dashboard's expected audience. Pinned so
// that a session JWT minted for one dashboard instance can't be
// silently replayed against another.
const SessionAudience = "openzro-dashboard"

// ErrSessionInvalid is returned by SessionVerifier when a token
// fails any of the validity checks (signature, expiry, issuer,
// audience). The middleware maps it to 401.
var ErrSessionInvalid = errors.New("auth: session token invalid")

// SessionClaims is the openZro session JWT body. Combines our
// own user identifier with the upstream provenance so audit logs
// can answer "which IdP authorised this session?" without a
// cross-table join.
type SessionClaims struct {
	Email       string `json:"email,omitempty"`
	Name        string `json:"name,omitempty"`
	ProviderID  uint64 `json:"provider_id"`
	UpstreamIss string `json:"upstream_iss"`
	UpstreamSub string `json:"upstream_sub"`
	jwt.RegisteredClaims
}

// SessionService issues + verifies the openZro session JWT. The
// signing key is the 32-byte raw form of the management's
// DataStoreEncryptionKey — same threat model as the at-rest
// envelope. HMAC-SHA256 on a 32-byte key is RFC-grade.
type SessionService struct {
	signingKey []byte
}

// NewSessionService takes the same base64-encoded 32-byte key
// the at-rest helpers use. Decoded internally; the caller never
// sees the raw bytes.
func NewSessionService(key string) (*SessionService, error) {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return nil, fmt.Errorf("auth: decode session key: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("auth: session key must be 32 bytes after base64 decode (got %d)", len(raw))
	}
	return &SessionService{signingKey: raw}, nil
}

// Issue mints an HS256-signed JWT carrying the supplied claims.
// The iss / aud / iat / exp claims are stamped from this method;
// callers fill in the openZro-specific fields (Email, Name,
// ProviderID, UpstreamIss, UpstreamSub, Subject).
func (s *SessionService) Issue(c SessionClaims, ttl time.Duration) (string, error) {
	now := time.Now()
	c.RegisteredClaims.Issuer = SessionIssuer
	c.RegisteredClaims.Audience = jwt.ClaimStrings{SessionAudience}
	c.RegisteredClaims.IssuedAt = jwt.NewNumericDate(now)
	c.RegisteredClaims.NotBefore = jwt.NewNumericDate(now)
	c.RegisteredClaims.ExpiresAt = jwt.NewNumericDate(now.Add(ttl))
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	signed, err := tok.SignedString(s.signingKey)
	if err != nil {
		return "", fmt.Errorf("auth: sign session: %w", err)
	}
	return signed, nil
}

// Verify parses and validates the session JWT. The audience +
// issuer + signing-method checks defend against confusion
// attacks (RS256-asymmetric tokens being smuggled into the
// HMAC verifier).
func (s *SessionService) Verify(raw string) (*SessionClaims, error) {
	if raw == "" {
		return nil, ErrSessionInvalid
	}
	var claims SessionClaims
	tok, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return s.signingKey, nil
	},
		jwt.WithIssuer(SessionIssuer),
		jwt.WithAudience(SessionAudience),
		jwt.WithIssuedAt(),
	)
	if err != nil || !tok.Valid {
		return nil, fmt.Errorf("%w: %v", ErrSessionInvalid, err)
	}
	return &claims, nil
}
