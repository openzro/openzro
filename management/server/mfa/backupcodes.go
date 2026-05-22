package mfa

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/openzro/openzro/management/server/types"
)

// bcryptCost is the work factor for backup-code hashing. 10 matches
// the rest of the codebase (setup-key password hashes); below 10 is
// brittle on modern hardware, above 12 starts blocking the auth
// hot-path on every challenge. Standard tradeoff.
const bcryptCost = 10

// GenerateBackupCodes mints `MFABackupCodeCount` fresh single-use
// recovery codes. Returns the plaintext list (returned ONCE to the
// caller for display to the user) and the bcrypt hashes (persisted
// to user_mfa.BackupCodesHash). The caller is responsible for
// showing the plaintext list exactly once — the management server
// never sees these codes again after this function returns.
//
// Visual format is dash-grouped hex pairs (`abcd-ef12-34`) so the
// user can transcribe from a printout / password manager more
// reliably than a wall of hex. The dashes are STRIPPED before
// hashing/verification, so a user who types either form works.
func GenerateBackupCodes() (plaintext []string, hashes []string, err error) {
	plaintext = make([]string, 0, types.MFABackupCodeCount)
	hashes = make([]string, 0, types.MFABackupCodeCount)
	for i := 0; i < types.MFABackupCodeCount; i++ {
		raw := make([]byte, types.MFABackupCodeLen)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, fmt.Errorf("mfa: rand.Read backup code: %w", err)
		}
		code := hex.EncodeToString(raw)
		display := formatBackupCode(code)
		hash, err := bcrypt.GenerateFromPassword([]byte(code), bcryptCost)
		if err != nil {
			return nil, nil, fmt.Errorf("mfa: bcrypt backup code: %w", err)
		}
		plaintext = append(plaintext, display)
		hashes = append(hashes, string(hash))
	}
	return plaintext, hashes, nil
}

// ConsumeBackupCode checks `candidate` against the array of bcrypt
// hashes and, on match, returns (remaining hashes after removing
// the consumed one, true). Caller writes the remaining slice back
// to the user_mfa row in the SAME transaction as the session
// upgrade, so a successful challenge atomically consumes the code
// (no replay possible).
//
// Returns (nil, false) on no match. Returns (nil, false) on empty
// input — refusing rather than accepting an empty candidate is
// defense-in-depth against any future path that ends up calling
// this with the wrong field.
func ConsumeBackupCode(candidate string, hashes []string) (remaining []string, ok bool) {
	candidate = strings.ReplaceAll(candidate, "-", "")
	candidate = strings.TrimSpace(strings.ToLower(candidate))
	if candidate == "" {
		return nil, false
	}
	for i, h := range hashes {
		if bcrypt.CompareHashAndPassword([]byte(h), []byte(candidate)) == nil {
			out := make([]string, 0, len(hashes)-1)
			out = append(out, hashes[:i]...)
			out = append(out, hashes[i+1:]...)
			return out, true
		}
	}
	return nil, false
}

// formatBackupCode pretty-prints a hex code in 4-char groups (so a
// 10-char hex code becomes "aabb-ccdd-ee"). The dashes are visual
// only — Consume strips them before bcrypt comparison.
func formatBackupCode(hexCode string) string {
	var b strings.Builder
	for i, r := range hexCode {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return b.String()
}
