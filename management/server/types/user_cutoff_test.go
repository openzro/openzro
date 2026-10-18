package types

import (
	"testing"
	"time"
)

// ADR-0027 D3: a JWT issued at or before the cutoff second is revoked; a
// token without iat is revoked only for users that have a cutoff.
func TestUser_JWTRevoked(t *testing.T) {
	cutoff := time.Date(2026, 10, 18, 12, 0, 5, 0, time.UTC)
	tests := []struct {
		name   string
		cutoff *time.Time
		iat    time.Time
		want   bool
	}{
		{"no cutoff, old token", nil, cutoff.Add(-time.Hour), false},
		{"no cutoff, no iat", nil, time.Time{}, false},
		{"before cutoff", &cutoff, cutoff.Add(-time.Second), true},
		{"same second as cutoff", &cutoff, cutoff, true},
		{"same second, later fraction", &cutoff, cutoff.Add(900 * time.Millisecond), true},
		{"next second", &cutoff, cutoff.Add(time.Second), false},
		{"cutoff set, no iat", &cutoff, time.Time{}, true},
		{"other time zone, after", &cutoff, cutoff.Add(time.Minute).In(time.FixedZone("x", -3*3600)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &User{CredentialsValidAfter: tt.cutoff}
			if got := u.JWTRevoked(tt.iat); got != tt.want {
				t.Fatalf("JWTRevoked(%s) = %v, want %v", tt.iat, got, tt.want)
			}
		})
	}
}

// Copy must carry the cutoff (the REST update path edits through Copy), and
// must not alias it.
func TestUser_CopyKeepsCredentialsCutoff(t *testing.T) {
	cutoff := time.Date(2026, 10, 18, 12, 0, 5, 0, time.UTC)
	u := &User{Id: "u", CredentialsValidAfter: &cutoff}
	c := u.Copy()
	if c.CredentialsValidAfter == nil || !c.CredentialsValidAfter.Equal(cutoff) {
		t.Fatalf("Copy lost the credentials cutoff: %+v", c.CredentialsValidAfter)
	}
	if c.CredentialsValidAfter == u.CredentialsValidAfter {
		t.Fatal("Copy aliases the credentials cutoff")
	}
}
