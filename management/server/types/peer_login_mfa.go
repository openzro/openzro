package types

import "time"

// Lifetimes of a peer login approval (ADR-0028 D3).
const (
	// PeerLoginMFAWindow is how long an attempt can be approved.
	PeerLoginMFAWindow = 5 * time.Minute
	// PeerLoginMFADeadline bounds the record and its token from creation.
	// ExpiresAt never passes it.
	PeerLoginMFADeadline = 7 * time.Minute
	// PeerLoginMFAApprovedGrace is how long, at least, an approval stays
	// usable after it is given, so the waiting client can log in.
	PeerLoginMFAApprovedGrace = 2 * time.Minute
	// PeerLoginMFAMaxPerUser caps the unexpired records of one user, in any
	// state.
	PeerLoginMFAMaxPerUser = 5
)

// PeerLoginMFAApproval is one device login waiting for, or carrying, the
// user's second factor (ADR-0028 D3). It is keyed by (account, user,
// WireGuard key). Only three transitions are written, each by a
// conditional update: pending → approved, pending → denied and
// approved → consumed. Expired is derived from ExpiresAt and never
// written; a sweep deletes expired records.
type PeerLoginMFAApproval struct {
	ID        string `gorm:"primaryKey;size:32"`
	AccountID string `gorm:"size:255;not null;index:idx_peer_login_mfa_key,priority:1"`
	UserID    string `gorm:"size:255;not null;index:idx_peer_login_mfa_key,priority:2;index:idx_peer_login_mfa_user,priority:1"`
	WGPubKey  string `gorm:"size:64;not null;index:idx_peer_login_mfa_key,priority:3"`

	// Display snapshot for the approval page. Hostname and OS are what the
	// device reported; IP and location are what the server observed.
	Hostname   string `gorm:"size:255"`
	OS         string `gorm:"size:255"`
	IP         string `gorm:"size:64"`
	GeoCountry string `gorm:"size:64"`
	GeoCity    string `gorm:"size:255"`
	NewPeer    bool

	CreatedAt time.Time `gorm:"not null"`
	// ExpiresAt starts at CreatedAt + PeerLoginMFAWindow and is extended by
	// an approval, never past Deadline.
	ExpiresAt time.Time `gorm:"not null;index:idx_peer_login_mfa_key,priority:4;index:idx_peer_login_mfa_user,priority:2;index:idx_peer_login_mfa_expiry"`
	// Deadline is CreatedAt + PeerLoginMFADeadline; the token expires at it.
	Deadline time.Time `gorm:"not null"`

	ApprovedAt *time.Time
	ConsumedAt *time.Time
	DeniedAt   *time.Time
}

// TableName pins the table name.
func (PeerLoginMFAApproval) TableName() string { return "peer_login_mfa_approvals" }

// Pending reports whether the record can still be approved or denied at now.
func (a *PeerLoginMFAApproval) Pending(now time.Time) bool {
	return a.ApprovedAt == nil && a.DeniedAt == nil && a.ConsumedAt == nil && now.Before(a.ExpiresAt)
}

// Usable reports whether an approved record can still be consumed at now.
func (a *PeerLoginMFAApproval) Usable(now time.Time) bool {
	return a.ApprovedAt != nil && a.DeniedAt == nil && a.ConsumedAt == nil && now.Before(a.ExpiresAt)
}
