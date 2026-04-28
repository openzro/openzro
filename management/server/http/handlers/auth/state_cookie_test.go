package auth

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSealer(t *testing.T) *StateCookieSealer {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	s, err := NewStateCookieSealer(base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)
	return s
}

func TestStateCookie_SealUnsealRoundTrip(t *testing.T) {
	s := newTestSealer(t)
	in := stateCookie{
		ProviderID:   42,
		CodeVerifier: "verifier-xyz",
		URLState:     "state-abc",
		Nonce:        "nonce-123",
		ReturnTo:     "/peers",
	}
	sealed, err := s.Seal(in)
	require.NoError(t, err)
	assert.NotContains(t, sealed, "verifier-xyz",
		"sealed value must not leak the verifier in plaintext")

	out, err := s.Unseal(sealed)
	require.NoError(t, err)
	assert.Equal(t, in.ProviderID, out.ProviderID)
	assert.Equal(t, in.CodeVerifier, out.CodeVerifier)
	assert.Equal(t, in.URLState, out.URLState)
	assert.Equal(t, in.Nonce, out.Nonce)
	assert.Equal(t, in.ReturnTo, out.ReturnTo)
	assert.NotZero(t, out.IssuedAt)
}

func TestStateCookie_RejectsTampering(t *testing.T) {
	s := newTestSealer(t)
	sealed, err := s.Seal(stateCookie{ProviderID: 1, CodeVerifier: "v", URLState: "u", Nonce: "n", ReturnTo: "/"})
	require.NoError(t, err)

	// Flip a single character in the middle.
	tampered := []byte(sealed)
	tampered[len(tampered)/2] ^= 0x01
	_, err = s.Unseal(string(tampered))
	require.Error(t, err)
}

func TestStateCookie_RejectsExpired(t *testing.T) {
	s := newTestSealer(t)
	old := stateCookie{
		ProviderID:   1,
		CodeVerifier: "v",
		URLState:     "u",
		Nonce:        "n",
		ReturnTo:     "/",
		IssuedAt:     time.Now().Add(-StateCookieTTL - time.Second).Unix(),
	}
	sealed, err := s.Seal(old)
	require.NoError(t, err)

	_, err = s.Unseal(sealed)
	assert.ErrorIs(t, err, ErrStateExpired)
}

func TestStateCookie_RejectsCrossSealerToken(t *testing.T) {
	a := newTestSealer(t)
	b := newTestSealer(t)
	sealed, err := a.Seal(stateCookie{ProviderID: 1, CodeVerifier: "v", URLState: "u", Nonce: "n", ReturnTo: "/"})
	require.NoError(t, err)

	_, err = b.Unseal(sealed)
	require.Error(t, err, "sealer B must not unseal A's cookie (different key)")
}
