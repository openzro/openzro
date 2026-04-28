package auth

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSession(t *testing.T) *SessionService {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	s, err := NewSessionService(base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)
	return s
}

func TestSession_IssueAndVerify(t *testing.T) {
	s := newTestSession(t)
	tok, err := s.Issue(SessionClaims{
		Email:       "user@example.com",
		Name:        "Test User",
		ProviderID:  7,
		UpstreamIss: "https://idp.example.com",
		UpstreamSub: "upstream-uuid",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "openzro-user-1",
		},
	}, SessionTTL)
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	claims, err := s.Verify(tok)
	require.NoError(t, err)
	assert.Equal(t, "openzro-user-1", claims.Subject)
	assert.Equal(t, "user@example.com", claims.Email)
	assert.Equal(t, uint64(7), claims.ProviderID)
	assert.Equal(t, "https://idp.example.com", claims.UpstreamIss)
	assert.Equal(t, SessionIssuer, claims.Issuer)
	assert.Contains(t, claims.Audience, SessionAudience)
}

func TestSession_RejectsTampering(t *testing.T) {
	s := newTestSession(t)
	tok, err := s.Issue(SessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user"},
	}, SessionTTL)
	require.NoError(t, err)

	parts := strings.Split(tok, ".")
	require.Len(t, parts, 3)
	// Flip a byte in the payload (middle segment).
	payload := []byte(parts[1])
	payload[len(payload)-1] ^= 0x01
	tampered := parts[0] + "." + string(payload) + "." + parts[2]

	_, err = s.Verify(tampered)
	assert.ErrorIs(t, err, ErrSessionInvalid)
}

func TestSession_RejectsExpired(t *testing.T) {
	s := newTestSession(t)
	tok, err := s.Issue(SessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user"},
	}, -time.Second) // already expired
	require.NoError(t, err)

	_, err = s.Verify(tok)
	assert.ErrorIs(t, err, ErrSessionInvalid)
}

func TestSession_RejectsCrossKey(t *testing.T) {
	a := newTestSession(t)
	b := newTestSession(t)
	tok, err := a.Issue(SessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user"},
	}, SessionTTL)
	require.NoError(t, err)

	_, err = b.Verify(tok)
	assert.ErrorIs(t, err, ErrSessionInvalid)
}

func TestSession_RejectsEmptyToken(t *testing.T) {
	s := newTestSession(t)
	_, err := s.Verify("")
	assert.ErrorIs(t, err, ErrSessionInvalid)
}

func TestSession_RejectsAlgConfusion(t *testing.T) {
	// Forge a "none"-alg token claiming the right issuer +
	// audience and assert the verifier rejects it. Defends
	// against the classic JWT alg=none confusion attack.
	s := newTestSession(t)
	claims := SessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:  "attacker",
			Issuer:   SessionIssuer,
			Audience: jwt.ClaimStrings{SessionAudience},
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	raw, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = s.Verify(raw)
	assert.ErrorIs(t, err, ErrSessionInvalid)
}

func TestSession_RejectsBadKeyLength(t *testing.T) {
	_, err := NewSessionService(base64.StdEncoding.EncodeToString([]byte("only-16-bytes-no")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}
