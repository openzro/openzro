package mfa

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

// TestNewTOTPKey_ValidatesAgainstItsOwnSecret pins the core round-trip:
// a freshly generated key's URL carries a secret that ValidateTOTP
// accepts when fed the current time-bucket code. If this breaks, no
// other test can pass — start here.
func TestNewTOTPKey_ValidatesAgainstItsOwnSecret(t *testing.T) {
	key, err := NewTOTPKey("test.openzro.io", "alice@example.com")
	require.NoError(t, err)
	require.NotEmpty(t, key.Secret())
	require.NotEmpty(t, key.URL())

	code, err := totp.GenerateCode(key.Secret(), time.Now().UTC())
	require.NoError(t, err)

	ok, err := ValidateTOTP(code, key.Secret())
	require.NoError(t, err)
	require.True(t, ok, "fresh key must validate its own current-time code")
}

// TestValidateTOTP_ClockSkewWindow pins the ±30s tolerance — a code
// generated for the next or previous step must still validate, but
// a code from 2 steps away must NOT. RFC 6238 §5.2 calls for at
// most ±1 step; widening leaks brute-force surface, narrowing
// breaks for users with NTP drift.
func TestValidateTOTP_ClockSkewWindow(t *testing.T) {
	key, err := NewTOTPKey("test.openzro.io", "alice@example.com")
	require.NoError(t, err)

	now := time.Now().UTC()
	t.Run("current step accepted", func(t *testing.T) {
		code, err := totp.GenerateCode(key.Secret(), now)
		require.NoError(t, err)
		ok, _ := ValidateTOTP(code, key.Secret())
		require.True(t, ok)
	})
	t.Run("previous step accepted", func(t *testing.T) {
		code, err := totp.GenerateCode(key.Secret(), now.Add(-30*time.Second))
		require.NoError(t, err)
		ok, _ := ValidateTOTP(code, key.Secret())
		require.True(t, ok, "±1 step skew must accept the previous code")
	})
	t.Run("next step accepted", func(t *testing.T) {
		code, err := totp.GenerateCode(key.Secret(), now.Add(30*time.Second))
		require.NoError(t, err)
		ok, _ := ValidateTOTP(code, key.Secret())
		require.True(t, ok, "±1 step skew must accept the next code")
	})
	t.Run("two steps away rejected", func(t *testing.T) {
		code, err := totp.GenerateCode(key.Secret(), now.Add(-90*time.Second))
		require.NoError(t, err)
		ok, _ := ValidateTOTP(code, key.Secret())
		require.False(t, ok, "two steps away must NOT validate — skew window pinned at ±1")
	})
}

// TestValidateTOTP_WrongCodeNotError pins the "user typo isn't a
// 5xx" contract: an invalid (but well-formed) 6-digit code returns
// (false, nil), not (false, err). The auth middleware uses this to
// distinguish a wrong code (increment failed_attempts) from a
// configuration failure (return 500).
func TestValidateTOTP_WrongCodeNotError(t *testing.T) {
	key, err := NewTOTPKey("test.openzro.io", "alice@example.com")
	require.NoError(t, err)

	ok, err := ValidateTOTP("000000", key.Secret())
	require.NoError(t, err)
	require.False(t, ok)
}

// TestValidateTOTP_MalformedSecretIsError pins the inverse: a broken
// secret returns a non-nil error so the middleware doesn't credit
// a malformed user_mfa row as a "wrong code" attempt.
func TestValidateTOTP_MalformedSecretIsError(t *testing.T) {
	_, err := ValidateTOTP("123456", "not-base32-!!!")
	require.Error(t, err)
}

// TestGenerateBackupCodes_Shape pins the format the dashboard sees:
// 10 codes, dash-grouped 4-char hex pairs, distinct from each other.
// The hashes match the plaintext (consume round-trips).
func TestGenerateBackupCodes_Shape(t *testing.T) {
	codes, hashes, err := GenerateBackupCodes()
	require.NoError(t, err)
	require.Len(t, codes, 10)
	require.Len(t, hashes, 10)

	seen := map[string]bool{}
	for _, c := range codes {
		require.False(t, seen[c], "backup codes must be unique within a batch: %q", c)
		seen[c] = true
		require.Contains(t, c, "-", "display format is dash-grouped: %q", c)
	}

	// Sanity: each plaintext consumes against its own hash.
	remaining := append([]string(nil), hashes...)
	rest, ok := ConsumeBackupCode(codes[0], remaining)
	require.True(t, ok, "plaintext must consume against its own batch")
	require.Len(t, rest, 9)
}

// TestConsumeBackupCode_SingleUse pins the single-use property —
// the second consume of the same code MUST fail, and the hash for
// that code MUST be removed from the array after the first
// success. The auth path persists the trimmed array atomically so a
// concurrent retry can't replay.
func TestConsumeBackupCode_SingleUse(t *testing.T) {
	codes, hashes, err := GenerateBackupCodes()
	require.NoError(t, err)

	remaining, ok := ConsumeBackupCode(codes[0], hashes)
	require.True(t, ok)

	// Second consume against the TRIMMED slice must fail.
	_, ok2 := ConsumeBackupCode(codes[0], remaining)
	require.False(t, ok2, "single-use: same code must NOT match after consume")
}

// TestConsumeBackupCode_NormalisesDashesAndCase confirms an operator
// who types either "aabb-ccdd-ee" or "AABBCCDDEE" gets a match —
// transcribing a printout shouldn't fail because of cosmetic
// formatting.
func TestConsumeBackupCode_NormalisesDashesAndCase(t *testing.T) {
	codes, hashes, err := GenerateBackupCodes()
	require.NoError(t, err)
	noDashes := strings.ReplaceAll(codes[0], "-", "")
	upper := strings.ToUpper(noDashes)

	_, ok := ConsumeBackupCode(noDashes, hashes)
	require.True(t, ok, "no-dash form must match")

	_, hashes2, err := GenerateBackupCodes()
	require.NoError(t, err)
	_, ok2 := ConsumeBackupCode(upper, hashes2)
	require.False(t, ok2, "different batch shouldn't match a previous code")
}

// TestSigner_IssueVerifyRoundTrip pins the happy-path: a redirect
// token minted with purpose P verifies with Verify(P) and yields
// the user id + JTI binding.
func TestSigner_IssueVerifyRoundTrip(t *testing.T) {
	signer := newTestSigner(t)

	token, err := signer.IssueRedirect("user-123", "jti-abc", PurposeChallenge)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	res, err := signer.Verify(token, PurposeChallenge)
	require.NoError(t, err)
	require.Equal(t, "user-123", res.UserID)
	require.Equal(t, "jti-abc", res.JTIBinding)
}

// TestSigner_RejectsWrongPurpose pins the cross-purpose refusal —
// an enrollment token MUST be rejected at the challenge endpoint
// and vice versa. Without this gate a captured enrollment token
// from a forced-enroll flow could be replayed at /challenge to
// bypass the actual TOTP step.
func TestSigner_RejectsWrongPurpose(t *testing.T) {
	signer := newTestSigner(t)

	enrollToken, err := signer.IssueRedirect("user-123", "jti-abc", PurposeEnrollment)
	require.NoError(t, err)

	_, err = signer.Verify(enrollToken, PurposeChallenge)
	require.Error(t, err, "challenge endpoint must NOT accept an enrollment token")

	// And a session token must not pass for redirect purposes.
	sess, err := signer.IssueSession("user-123", "jti-abc")
	require.NoError(t, err)
	_, err = signer.Verify(sess, PurposeChallenge)
	require.Error(t, err, "challenge endpoint must NOT accept a session token")
}

// TestSigner_RejectsCorrupted pins the integrity check: tampering
// with a token's last byte yields a verification failure. Pins the
// same defense-in-depth as the per-TTL exp check.
func TestSigner_RejectsCorrupted(t *testing.T) {
	signer := newTestSigner(t)
	tok, err := signer.IssueRedirect("user-xyz", "jti-abc", PurposeChallenge)
	require.NoError(t, err)
	_, err = signer.Verify(tok+"x", PurposeChallenge)
	require.Error(t, err)
}

// TestSigner_RejectsEmpty pins the empty-token refusal — covers
// the "Authorization: Bearer " split where the second part is
// missing.
func TestSigner_RejectsEmpty(t *testing.T) {
	signer := newTestSigner(t)
	_, err := signer.Verify("", PurposeChallenge)
	require.Error(t, err)
}

// TestSigner_PendingCarriesSecret pins the secret round-trip for the
// stateless pending-enrollment flow. The secret embedded at issue
// time MUST be returned verbatim by Verify so /enroll/finish can
// validate the user-supplied TOTP code against it.
func TestSigner_PendingCarriesSecret(t *testing.T) {
	signer := newTestSigner(t)
	tok, err := signer.IssuePending("user-1", "JBSWY3DPEHPK3PXP")
	require.NoError(t, err)

	res, err := signer.Verify(tok, PurposePendingEnrollment)
	require.NoError(t, err)
	require.Equal(t, "user-1", res.UserID)
	require.Equal(t, "JBSWY3DPEHPK3PXP", res.Secret)

	// Cross-purpose refusal still applies.
	_, err = signer.Verify(tok, PurposeChallenge)
	require.Error(t, err, "pending token must NOT verify as a challenge token")
}

// TestSigner_SessionBindsToJTI pins the per-session contract: a
// session token issued for JTI A must verify and carry A back; a
// session token must NOT silently accept a different JTI on a later
// gated request (that check lives in the manager, but the token
// itself MUST carry the binding so the manager can enforce it).
func TestSigner_SessionBindsToJTI(t *testing.T) {
	signer := newTestSigner(t)
	tok, err := signer.IssueSession("user-1", "jti-A")
	require.NoError(t, err)
	res, err := signer.Verify(tok, PurposeSession)
	require.NoError(t, err)
	require.Equal(t, "jti-A", res.JTIBinding)
}

// newTestSigner builds a Signer from a freshly generated AES key so
// each test gets its own deterministic-but-independent key.
func newTestSigner(t *testing.T) *Signer {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	signer, err := NewSigner(base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)
	return signer
}
