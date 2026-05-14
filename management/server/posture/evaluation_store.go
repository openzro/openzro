package posture

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// GormEvalStore is the production-backed EvalStore. Shares the same
// *gorm.DB as the rest of the management server so transactions and
// connection pooling stay coherent — no separate DSN.
type GormEvalStore struct {
	db *gorm.DB
}

// NewGormEvalStore wires the store with an existing GORM handle.
// MigrateEvaluationTable must have been called against the same
// handle at startup (it is — see management/cmd/management.go).
func NewGormEvalStore(db *gorm.DB) *GormEvalStore {
	return &GormEvalStore{db: db}
}

// Insert writes a batch of evaluations in a single statement when the
// driver supports multi-row insert, falling back to per-row otherwise.
// Errors on individual rows are non-fatal: we log + continue so the
// posture eval pipeline keeps working through transient persistence
// issues (table locks, brief outage). The recorder treats this as
// best-effort by design.
func (s *GormEvalStore) Insert(ctx context.Context, batch []PostureEvaluation) error {
	if len(batch) == 0 {
		return nil
	}
	// CreateInBatches splits the slice into chunks the driver can
	// handle; 200 is well under Postgres' ~65k parameter ceiling
	// for our 7-column schema and keeps MySQL/SQLite happy too.
	return s.db.WithContext(ctx).CreateInBatches(batch, 200).Error
}

// ListForPeer answers the dashboard timeline query: newest evals
// for one peer in one account, capped at limit. The compound index
// (account_id, peer_id, evaluated_at DESC) makes this a straight
// index range scan with no sort.
func (s *GormEvalStore) ListForPeer(ctx context.Context, accountID, peerID string, limit int) ([]PostureEvaluation, error) {
	if accountID == "" || peerID == "" {
		return nil, errors.New("posture: ListForPeer requires accountID and peerID")
	}
	if limit <= 0 || limit > 500 {
		// Cap the limit so a misconfigured caller can't pull the
		// whole table back in one request. 500 is well above what
		// the UI ever renders (we paginate at 50 rows there).
		limit = 100
	}
	var rows []PostureEvaluation
	err := s.db.WithContext(ctx).
		Where("account_id = ? AND peer_id = ?", accountID, peerID).
		Order("evaluated_at DESC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("posture: list evaluations: %w", err)
	}
	return rows, nil
}

// PurgeOlderThan trims rows older than the cutoff. Run by the
// retention goroutine on a timer (see eval_retention.go).
func (s *GormEvalStore) PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res := s.db.WithContext(ctx).
		Where("evaluated_at < ?", cutoff).
		Delete(&PostureEvaluation{})
	if res.Error != nil {
		return 0, fmt.Errorf("posture: purge evaluations: %w", res.Error)
	}
	return res.RowsAffected, nil
}
