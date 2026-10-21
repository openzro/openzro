package mfa

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MatchTOTPStep reports the time step a code belongs to, within the ±1 step
// clock-skew window, so callers can refuse a step that was already used.
func TestMatchTOTPStep(t *testing.T) {
	key, err := NewTOTPKey("acct", "user@example.com")
	require.NoError(t, err)
	secret := key.Secret()

	// 12:00:00 UTC is the start of a 30-second step.
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	step := uint64(now.Unix()) / 30
	code := func(at time.Time) string {
		c, err := totp.GenerateCode(secret, at)
		require.NoError(t, err)
		return c
	}

	tests := []struct {
		name     string
		codeAt   time.Time
		checkAt  time.Time
		wantStep uint64
		wantOK   bool
	}{
		{"current step", now, now, step, true},
		{"previous step within skew", now.Add(-30 * time.Second), now, step - 1, true},
		{"next step within skew", now.Add(30 * time.Second), now, step + 1, true},
		{"last second of a step", now.Add(29 * time.Second), now.Add(29 * time.Second), step, true},
		{"first second of the next step", now.Add(29 * time.Second), now.Add(30 * time.Second), step, true},
		{"two steps old", now.Add(-60 * time.Second), now, 0, false},
		{"two steps ahead", now.Add(60 * time.Second), now, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := MatchTOTPStep(code(tc.codeAt), secret, tc.checkAt)
			require.NoError(t, err)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantStep, got)
			}
		})
	}

	t.Run("wrong code", func(t *testing.T) {
		wrong := "000000"
		if code(now) == wrong {
			wrong = "111111"
		}
		_, ok, err := MatchTOTPStep(wrong, secret, now)
		require.NoError(t, err)
		assert.False(t, ok)
	})
	t.Run("malformed code", func(t *testing.T) {
		for _, c := range []string{"", "12345", "1234567", "abcdef"} {
			_, ok, err := MatchTOTPStep(c, secret, now)
			require.NoError(t, err, c)
			assert.False(t, ok, c)
		}
	})
	t.Run("malformed secret", func(t *testing.T) {
		_, _, err := MatchTOTPStep(code(now), "not base32!", now)
		require.Error(t, err)
	})
}

// A pending-enrollment token says whether it is a re-enrollment and which
// factor it replaces. A re-enrollment of a factor without an ID is still a
// re-enrollment.
func TestPendingTokenCarriesEnrollmentMode(t *testing.T) {
	s, err := NewSigner(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	require.NoError(t, err)
	tests := []struct {
		reenroll bool
		factor   string
	}{{false, ""}, {true, ""}, {true, "factor-1"}}
	for _, tc := range tests {
		raw, err := s.IssuePending("user-1", "JBSWY3DPEHPK3PXP", tc.reenroll, tc.factor)
		require.NoError(t, err)
		res, err := s.Verify(raw, PurposePendingEnrollment)
		require.NoError(t, err)
		assert.Equal(t, tc.reenroll, res.ReEnrollment)
		assert.Equal(t, tc.factor, res.ReplacesFactor)
		assert.Equal(t, "JBSWY3DPEHPK3PXP", res.Secret)
	}
}

// A peer login approval token names its record and expires at the
// record's deadline; it is useless for any other purpose.
func TestPeerLoginToken(t *testing.T) {
	s, err := NewSigner(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	require.NoError(t, err)
	deadline := time.Now().Add(7 * time.Minute).Truncate(time.Second)

	raw, err := s.IssuePeerLogin("user-1", "approval-1", deadline)
	require.NoError(t, err)
	res, err := s.Verify(raw, PurposePeerLogin)
	require.NoError(t, err)
	assert.Equal(t, "user-1", res.UserID)
	assert.Equal(t, "approval-1", res.ApprovalID)
	assert.True(t, deadline.Equal(res.ExpiresAt), "expires at the record's deadline")

	_, err = s.Verify(raw, PurposeSession)
	require.Error(t, err, "not usable as another purpose")

	expired, err := s.IssuePeerLogin("user-1", "approval-1", time.Now().Add(-time.Second))
	require.NoError(t, err)
	_, err = s.Verify(expired, PurposePeerLogin)
	require.Error(t, err, "an expired token is refused")

	_, err = s.IssuePeerLogin("user-1", "", deadline)
	require.Error(t, err, "the record is required")
}
