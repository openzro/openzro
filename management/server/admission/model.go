// Package admission persists and queries the per-peer admission
// bypass records that complement the account-wide Device Admission
// gate. The gate (in management/server/types/account.go) refuses
// non-compliant devices at Login / Sync; the bypass overrides that
// for a specific peer with mandatory audit metadata (initiator,
// reason, expiry).
//
// Why bypass exists. ADR-0003 originally rejected per-peer overrides
// out of concern that they would erode the audit story. ADR-0004
// supersedes that decision: a bypass is a legitimate break-glass
// mechanism PROVIDED every grant, revoke, and expiry emits a durable
// activity event. The audit trail then carries MORE information,
// not less — the auditor sees who authorized the exception, why,
// and when it expired.
//
// Bypass scope. A bypass is per-account-per-peer. It applies only to
// the admission gate; per-policy posture checks still run as usual.
// A bypass with `expires_at` in the past is considered inactive even
// if it has not been physically deleted yet — the SweepExpired
// worker tidies the rows asynchronously and emits the
// `peer.admission.bypass.expired` audit event when it does.
package admission

import "time"

// PeerAdmissionBypass is the GORM-managed row.
//
// AccountID is part of the primary key relationship so a bypass
// cannot leak across tenants on a peer-ID collision (peer IDs are
// xid, globally unique in practice, but we still scope every query
// by account to defend against ID-guessing).
type PeerAdmissionBypass struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement"`
	AccountID   string    `gorm:"size:64;not null;index:idx_admission_bypass_account_peer"`
	PeerID      string    `gorm:"size:64;not null;index:idx_admission_bypass_account_peer"`
	InitiatorID string    `gorm:"size:64;not null"`
	Reason      string    `gorm:"type:text;not null"`
	GrantedAt   time.Time `gorm:"not null"`
	// ExpiresAt is the wall-clock cutoff. A bypass with ExpiresAt
	// in the past is inactive regardless of its physical row state.
	// Use time.Time{} (zero) for "no expiry" — discouraged in
	// production but allowed for break-glass without an obvious
	// remediation timeline.
	ExpiresAt time.Time `gorm:"not null"`
}

func (PeerAdmissionBypass) TableName() string { return "peer_admission_bypasses" }

// IsActive reports whether the bypass is currently in effect at t.
// A zero ExpiresAt is treated as "never expires" — the only path
// that needs a manual revoke. Operators with a strict compliance
// posture should never grant a no-expiry bypass; the API rejects
// the field unless explicitly toggled.
func (b PeerAdmissionBypass) IsActive(t time.Time) bool {
	if b.ExpiresAt.IsZero() {
		return true
	}
	return t.Before(b.ExpiresAt)
}
