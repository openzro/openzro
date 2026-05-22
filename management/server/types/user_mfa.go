package types

import (
	"time"
)

// MFA method identifiers used by UserMFA.Method. TOTP is the only
// shipped method today; WebAuthn / FIDO2 is provisioned as an enum
// value so a future PR can add it without a schema migration —
// the `method` column is already free-form text.
const (
	MFAMethodTOTP = "totp"
)

// UserMFA is the per-user second-factor state for openZro's
// in-management MFA layer (issue #31). It lives in a SEPARATE table
// from User so the encrypted secret stays off the hot User.Copy()
// path and the table can grow new method-specific columns
// (WebAuthn credential blobs, recovery-question hashes, etc.) without
// bloating the canonical user row.
//
// PRIMARY KEY is UserID (one MFA record per user). Each user can
// have AT MOST one active second factor today; if WebAuthn lands
// later, the schema choice is between a multi-method table (one row
// per method) and per-method columns on this single row. We pick the
// single-row design first because it keeps the gate decision in the
// middleware to one SELECT.
type UserMFA struct {
	// UserID owns the FK to users(id). PK so a SELECT by user_id is
	// the hot path the auth middleware runs on every gated request.
	UserID string `gorm:"primaryKey"`

	// Method identifies the second-factor type currently enrolled.
	// MFAMethodTOTP today; placeholder for "webauthn", "u2f", etc.
	// when those land. Defaulted server-side at enrollment.
	Method string `gorm:"not null;default:totp"`

	// TOTPSecretEnc is the AES-256-GCM-encrypted RFC 4226 shared
	// secret. The plaintext secret is 160 bits (20 bytes) per RFC
	// 6238 recommendation; the ciphertext + GCM nonce + auth tag +
	// base64 framing inflates this to roughly 64 bytes on disk.
	// Encryption key comes from Config.DataStoreEncryptionKey via
	// the existing FieldEncrypt helper (activity/store/crypt.go).
	// The plaintext is NEVER logged and is decrypted only at the
	// verification call site.
	TOTPSecretEnc string `gorm:"not null"`

	// BackupCodesHash holds the bcrypt hashes of unused single-use
	// recovery codes. Plaintext codes are shown to the user exactly
	// once at enrollment (and on explicit regenerate); they are
	// stored hashed so a DB read cannot recover them. A successful
	// challenge against a backup code REMOVES that hash from the
	// slice (single-use semantics enforced atomically via a single
	// SaveUserMFA write). When the slice empties, the user must
	// regenerate to keep recovery available.
	BackupCodesHash []string `gorm:"serializer:json"`

	// EnrolledAt records when the user completed enrollment. Useful
	// for audit and for showing a "Two-factor authentication active
	// since X" line on the profile page.
	EnrolledAt time.Time `gorm:"not null"`

	// LastVerifiedAt is the timestamp of the most recent successful
	// challenge. The auth middleware uses this (plus a TTL) to gate
	// requests: a session is "MFA-verified" while LastVerifiedAt is
	// within the session window; outside the window, the user is
	// re-challenged. Reset to zero on Disenroll.
	LastVerifiedAt *time.Time

	// FailedAttempts is the running count of consecutive failed
	// challenges. Reset to 0 on successful challenge. When it hits
	// the lockout threshold (5), LockedUntil is set and FailedAttempts
	// holds at the threshold until the lockout expires.
	FailedAttempts int `gorm:"not null;default:0"`

	// LockedUntil is non-nil when the account is in brute-force
	// lockout. Challenge requests with `now < LockedUntil` return
	// 423 (Locked) without consuming a slot. Lockout duration is
	// 15min from the 5th failure.
	LockedUntil *time.Time
}

// MFASessionTTL bounds how long a successful TOTP challenge keeps a
// session "MFA-verified" before re-challenge. Set to the same order
// of magnitude as a typical Dex JWT lifetime so the operator's MFA
// policy effectively rides on the same session-rotation cadence as
// the IdP's. Re-challenge prompts the user mid-session if they leave
// the tab open over a weekend.
const MFASessionTTL = 8 * time.Hour

// MFALockoutThreshold is the consecutive-failure count after which
// the user is locked out for MFALockoutDuration. RFC-recommended
// guardrail against TOTP brute force: with a 6-digit window of
// 10^6 possibilities and a ±30s tolerance, an unbounded attacker
// has roughly 1/15000 per attempt — 5 attempts × 15min lockout
// keeps the per-account brute-force rate well below 1 success per
// year of sustained attack.
const MFALockoutThreshold = 5

// MFALockoutDuration is how long the user stays locked after
// hitting the threshold. 15min strikes a balance between thwarting
// brute force and being recoverable for a real user who typoed
// their authenticator app — they get a fresh window without needing
// an admin reset.
const MFALockoutDuration = 15 * time.Minute

// MFABackupCodeCount is how many single-use recovery codes are
// minted at enrollment + on explicit regenerate. 10 is the de-facto
// industry standard (GitHub, Google, AWS, Vault) — enough to cover
// device loss + a few mistyped recoveries without becoming a
// management chore.
const MFABackupCodeCount = 10

// MFABackupCodeLen is the byte length of each backup code BEFORE
// hex encoding (so the user sees 2× this many hex chars). 5 bytes =
// 10 hex chars; trimmed to 4-char groups visually by the dashboard
// ("aabb-ccdd-ee"). Adequate entropy for a single-use code (40 bits)
// without making the user retype 30+ characters from a printed list.
const MFABackupCodeLen = 5
