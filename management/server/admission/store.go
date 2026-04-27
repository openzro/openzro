package admission

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ErrNotFound is returned when no bypass row matches the lookup.
// Callers translate to HTTP 404 at the API boundary.
var ErrNotFound = errors.New("admission: bypass not found")

// MaxBypassDuration is the longest expiry the API accepts. 30 days
// is enough for "this device is being replaced next month" and short
// enough that a forgotten bypass does not become permanent.
const MaxBypassDuration = 30 * 24 * time.Hour

// Store provides CRUD on PeerAdmissionBypass rows scoped by account.
// Uses GORM's auto-migrate so the consumer (cmd/management.go) does
// not need to think about migrations.
type Store struct {
	db *gorm.DB
}

// NewStore wires a Store and runs AutoMigrate.
func NewStore(db *gorm.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("admission: db is required")
	}
	if err := db.AutoMigrate(&PeerAdmissionBypass{}); err != nil {
		return nil, fmt.Errorf("admission: automigrate: %w", err)
	}
	return &Store{db: db}, nil
}

// GrantInput is the public-facing payload for Grant. Reason is
// required (free text); ExpiresAt is required and must be in the
// future and within MaxBypassDuration of now.
type GrantInput struct {
	AccountID   string
	PeerID      string
	InitiatorID string
	Reason      string
	ExpiresAt   time.Time
}

func (in *GrantInput) validate(now time.Time) error {
	if in.AccountID == "" || in.PeerID == "" {
		return errors.New("admission: account_id and peer_id are required")
	}
	if in.InitiatorID == "" {
		return errors.New("admission: initiator_id is required (audit trail)")
	}
	if in.Reason == "" {
		return errors.New("admission: reason is required (audit trail)")
	}
	if in.ExpiresAt.IsZero() {
		return errors.New("admission: expires_at is required (no-expiry bypasses are not permitted)")
	}
	if !in.ExpiresAt.After(now) {
		return errors.New("admission: expires_at must be in the future")
	}
	if in.ExpiresAt.Sub(now) > MaxBypassDuration {
		return fmt.Errorf("admission: expires_at exceeds %s (operator must re-grant for longer windows)", MaxBypassDuration)
	}
	return nil
}

// Grant inserts a new bypass row. If an active bypass already exists
// for the same (account, peer) tuple, it is replaced — the new
// reason/expiry win and an audit event records the supersession at
// the API layer. Returns the inserted row.
func (s *Store) Grant(ctx context.Context, in GrantInput) (*PeerAdmissionBypass, error) {
	now := time.Now().UTC()
	if err := in.validate(now); err != nil {
		return nil, err
	}

	// Replace any existing active rows so IsActive lookups stay
	// O(1) on the (account, peer) index. Hard-delete: the audit
	// trail lives in activity events, not in the bypass table.
	if err := s.db.WithContext(ctx).
		Where("account_id = ? AND peer_id = ?", in.AccountID, in.PeerID).
		Delete(&PeerAdmissionBypass{}).Error; err != nil {
		return nil, fmt.Errorf("admission: delete existing: %w", err)
	}

	row := PeerAdmissionBypass{
		AccountID:   in.AccountID,
		PeerID:      in.PeerID,
		InitiatorID: in.InitiatorID,
		Reason:      in.Reason,
		GrantedAt:   now,
		ExpiresAt:   in.ExpiresAt.UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, fmt.Errorf("admission: insert: %w", err)
	}
	return &row, nil
}

// Revoke deletes the bypass row for (account, peer). Returns
// ErrNotFound when no row exists. The caller emits the audit event.
func (s *Store) Revoke(ctx context.Context, accountID, peerID string) (*PeerAdmissionBypass, error) {
	var row PeerAdmissionBypass
	err := s.db.WithContext(ctx).
		Where("account_id = ? AND peer_id = ?", accountID, peerID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.db.WithContext(ctx).Delete(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// IsActive reports whether the peer has an unexpired bypass right
// now. Hot-path call from evaluateAdmission — keep it cheap; the
// (account, peer) index makes this an indexed point lookup.
func (s *Store) IsActive(ctx context.Context, accountID, peerID string) (bool, *PeerAdmissionBypass, error) {
	var row PeerAdmissionBypass
	err := s.db.WithContext(ctx).
		Where("account_id = ? AND peer_id = ?", accountID, peerID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil, nil
		}
		return false, nil, err
	}
	if !row.IsActive(time.Now().UTC()) {
		return false, &row, nil
	}
	return true, &row, nil
}

// List returns every bypass row for the account, ordered by
// most-recently-granted first. Used by the dashboard and by the
// auditor's CSV export so an active bypass shows up alongside
// the denials it modified.
func (s *Store) List(ctx context.Context, accountID string) ([]PeerAdmissionBypass, error) {
	var rows []PeerAdmissionBypass
	if err := s.db.WithContext(ctx).
		Where("account_id = ?", accountID).
		Order("granted_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// SweepExpired deletes every row whose ExpiresAt is in the past and
// returns the deleted set. The expiry worker calls this on a timer
// and emits one `peer.admission.bypass.expired` audit event per row.
//
// Zero-ExpiresAt rows ("never expires") are skipped — they are only
// removable via Revoke.
func (s *Store) SweepExpired(ctx context.Context, now time.Time) ([]PeerAdmissionBypass, error) {
	var expired []PeerAdmissionBypass
	if err := s.db.WithContext(ctx).
		Where("expires_at < ?", now.UTC()).
		Where("expires_at <> ?", time.Time{}).
		Find(&expired).Error; err != nil {
		return nil, fmt.Errorf("admission: sweep find: %w", err)
	}
	if len(expired) == 0 {
		return nil, nil
	}
	ids := make([]uint64, 0, len(expired))
	for i := range expired {
		ids = append(ids, expired[i].ID)
	}
	if err := s.db.WithContext(ctx).
		Where("id IN ?", ids).
		Delete(&PeerAdmissionBypass{}).Error; err != nil {
		return nil, fmt.Errorf("admission: sweep delete: %w", err)
	}
	return expired, nil
}

// HasGroupOverlap reports whether any of `peerGroupIDs` is in
// `exemptGroupIDs`. Free function (no DB access) so the hot
// evaluateAdmission path does not pay an extra round trip — the
// caller already has both slices in scope.
func HasGroupOverlap(peerGroupIDs, exemptGroupIDs []string) bool {
	if len(peerGroupIDs) == 0 || len(exemptGroupIDs) == 0 {
		return false
	}
	exempt := make(map[string]struct{}, len(exemptGroupIDs))
	for _, g := range exemptGroupIDs {
		exempt[g] = struct{}{}
	}
	for _, g := range peerGroupIDs {
		if _, ok := exempt[g]; ok {
			return true
		}
	}
	return false
}
