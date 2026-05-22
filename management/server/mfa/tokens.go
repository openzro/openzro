package mfa

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenPurpose identifies what an MFA-scoped token can be redeemed
// for. Tokens minted with one purpose are rejected at endpoints
// expecting another — cross-use is a hostile request, not an honest
// mistake, so we surface as 401 with no leakage about which purpose
// was minted.
type TokenPurpose string

const (
	// PurposeChallenge: short-lived bearer for /api/mfa/challenge.
	// Issued when an enrolled user has not completed an MFA challenge
	// for the current JWT session.
	PurposeChallenge TokenPurpose = "mfa_challenge"

	// PurposeEnrollment: short-lived bearer for /api/mfa/enroll/*.
	// Issued when enforcement is on and the user has no MFA enrolled.
	PurposeEnrollment TokenPurpose = "mfa_enrollment"

	// PurposePendingEnrollment: short-lived bearer returned by
	// /enroll/start and consumed by /enroll/finish. Carries the
	// (pending, not yet persisted) TOTP secret so a daemon restart
	// or a request landing on a different HA replica between start
	// and finish doesn't lose the in-flight enrollment. The token is
	// HTTPS-only and the secret is the same one the user already
	// scanned in the QR code; no new leak surface.
	PurposePendingEnrollment TokenPurpose = "mfa_pending_enrollment"

	// PurposeSession: long(er)-lived bearer that proves the holding
	// JWT session has cleared the MFA gate. Replaces the per-user
	// `user_mfa.last_verified_at` heuristic (issue #31 review
	// finding #3) — verification is now bound to a specific JWT
	// session via JTIBinding, so verifying on device A does not
	// silently elevate device B that happens to be logged in as the
	// same user.
	PurposeSession TokenPurpose = "mfa_session"
)

// pendingTokenTTL bounds how long the user has to scan the QR and
// type a code before /enroll/finish refuses. 15min mirrors the old
// in-memory cache window.
const pendingTokenTTL = 15 * time.Minute

// shortTokenTTL bounds the challenge / enrollment redirect tokens.
// 5min is long enough to scan a QR + type a code without rushing,
// short enough that a captured token from a logged-out browser
// session is uninteresting.
const shortTokenTTL = 5 * time.Minute

// sessionTokenTTL must mirror types.MFASessionTTL — duplicated here
// to avoid a `types` import from a leaf package. Any change to that
// constant should be mirrored in test asserts of expectedly long
// sessions.
const sessionTokenTTL = 8 * time.Hour

// mfaTokenSigningKeyInfo is the HKDF-style domain-separation label
// used to derive the HS256 signing key from the operator's
// DataStoreEncryptionKey. Without separation a token leak could be
// rotated by changing only one key — but with separation, a leak of
// the encryption key does NOT trivially yield the token-signing key
// (and vice versa). The label is versioned so future rotation can
// re-derive cleanly.
//
//nolint:gosec // G101 — not a credential, it's a fixed domain-
// separation label used as HKDF info parameter. The label is
// public information; the secret is the AES key it's combined with.
const mfaTokenSigningKeyInfo = "openzro-mfa-token-v1"

// Signer mints and validates short-lived MFA-scoped JWTs. Bound to
// a single derived key at construction so verifying is a fast HMAC
// — no per-request key derivation.
type Signer struct {
	key []byte
}

// NewSigner derives the HS256 signing key from the operator's
// DataStoreEncryptionKey (base64-encoded as it sits in
// management.json) via SHA-256(key || info-label). The same input
// yields the same output deterministically across daemon restarts,
// so already-issued tokens remain verifiable after a normal restart.
// Rotating DataStoreEncryptionKey invalidates outstanding MFA
// tokens — same trust boundary as the data-at-rest encryption that
// would already need to be rotated under that scenario.
func NewSigner(dataStoreEncryptionKeyB64 string) (*Signer, error) {
	if dataStoreEncryptionKeyB64 == "" {
		return nil, errors.New("mfa: empty DataStoreEncryptionKey — set it in management.json before enabling MFA")
	}
	raw, err := base64.StdEncoding.DecodeString(dataStoreEncryptionKeyB64)
	if err != nil {
		return nil, fmt.Errorf("mfa: DataStoreEncryptionKey is not base64: %w", err)
	}
	h := sha256.New()
	h.Write(raw)
	h.Write([]byte(mfaTokenSigningKeyInfo))
	return &Signer{key: h.Sum(nil)}, nil
}

// tokenClaims is the wire shape. `Purpose` lives in a dedicated
// claim so the verify path can refuse a wrong-purpose token before
// reaching the endpoint's business logic. `JTIBinding` ties the
// token to a specific JWT session so verifying on one device does
// not silently elevate another device logged in as the same user.
// `Secret` carries the base32 TOTP shared secret for
// PurposePendingEnrollment only (empty for other purposes).
type tokenClaims struct {
	Purpose    TokenPurpose `json:"purpose"`
	JTIBinding string       `json:"jti_binding,omitempty"`
	Secret     string       `json:"secret,omitempty"`
	jwt.RegisteredClaims
}

// IssueRedirect mints a short-lived challenge_token or
// enrollment_token (both ride the same shape). The token carries
// the JWT session id that triggered the gate decision so the
// dashboard's subsequent mfa_session_token will be bound back to
// the same session. Use PurposeChallenge or PurposeEnrollment.
func (s *Signer) IssueRedirect(userID, jtiBinding string, purpose TokenPurpose) (string, error) {
	if userID == "" {
		return "", errors.New("mfa: IssueRedirect requires userID")
	}
	if purpose != PurposeChallenge && purpose != PurposeEnrollment {
		return "", fmt.Errorf("mfa: IssueRedirect refuses purpose %q", purpose)
	}
	return s.issue(userID, jtiBinding, "", purpose, shortTokenTTL)
}

// IssuePending mints a pending_enrollment_token carrying the
// base32-encoded TOTP secret. The token replaces the in-memory
// PendingStore so /enroll/finish can land on any HA replica.
// Lifetime is 15min, matching the legacy cache TTL.
func (s *Signer) IssuePending(userID, secretBase32 string) (string, error) {
	if userID == "" || secretBase32 == "" {
		return "", errors.New("mfa: IssuePending requires userID and secret")
	}
	return s.issue(userID, "", secretBase32, PurposePendingEnrollment, pendingTokenTTL)
}

// IssueSession mints an mfa_session_token bound to a specific JWT
// session. Returned by /challenge and /enroll/finish on success;
// stored by the dashboard and sent in X-MFA-Token on subsequent
// requests so the middleware can verify the session WITHOUT
// trusting a per-user `last_verified_at` shared across devices.
func (s *Signer) IssueSession(userID, jtiBinding string) (string, error) {
	if userID == "" || jtiBinding == "" {
		return "", errors.New("mfa: IssueSession requires userID and jtiBinding")
	}
	return s.issue(userID, jtiBinding, "", PurposeSession, sessionTokenTTL)
}

func (s *Signer) issue(userID, jtiBinding, secret string, purpose TokenPurpose, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := tokenClaims{
		Purpose:    purpose,
		JTIBinding: jtiBinding,
		Secret:     secret,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(s.key)
}

// VerifyResult is what Verify returns on success: the subject, the
// JWT session binding (empty for pending-enrollment), and the
// carried TOTP secret (set only for pending-enrollment). On any
// failure path returns a zero result + non-nil error; the caller
// maps that to 401 with no detail leak.
type VerifyResult struct {
	UserID     string
	JTIBinding string
	Secret     string
}

// Verify parses, validates signature + expiry, and asserts the
// claim's purpose matches `want`. Returns the subject and the
// JTI binding on success.
func (s *Signer) Verify(raw string, want TokenPurpose) (VerifyResult, error) {
	if raw == "" {
		return VerifyResult{}, errors.New("mfa: empty token")
	}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	tok, err := parser.ParseWithClaims(raw, &tokenClaims{}, func(t *jwt.Token) (any, error) {
		return s.key, nil
	})
	if err != nil {
		return VerifyResult{}, fmt.Errorf("mfa: parse: %w", err)
	}
	claims, ok := tok.Claims.(*tokenClaims)
	if !ok || !tok.Valid {
		return VerifyResult{}, errors.New("mfa: token invalid")
	}
	if claims.Purpose != want {
		// Don't leak the actual purpose — just refuse.
		return VerifyResult{}, errors.New("mfa: token purpose mismatch")
	}
	if claims.Subject == "" {
		return VerifyResult{}, errors.New("mfa: token missing subject")
	}
	return VerifyResult{
		UserID:     claims.Subject,
		JTIBinding: claims.JTIBinding,
		Secret:     claims.Secret,
	}, nil
}
