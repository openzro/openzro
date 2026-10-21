package mfa

import (
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
