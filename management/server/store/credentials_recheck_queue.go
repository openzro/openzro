package store

import (
	"context"
	"time"

	"github.com/rs/xid"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/openzro/openzro/management/server/status"
)

// credentialsRecheck is one pending pass of a local password reset
// (ADR-0027 D6). Every reset enqueues its own sweep, and every sweep its own
// fence, so overlapping resets never share or move each other's deadline.
type credentialsRecheck struct {
	ID        string    `gorm:"primaryKey;size:32"`
	AccountID string    `gorm:"size:255;index"`
	UserID    string    `gorm:"size:255;index"`
	DueAt     time.Time `gorm:"index"`
	Fence     bool
}

// TableName pins the table name.
func (credentialsRecheck) TableName() string { return "local_credential_rechecks" }

// CredentialsRecheck is a pending pass of a local password reset.
type CredentialsRecheck struct {
	ID        string
	AccountID string
	UserID    string
	DueAt     time.Time
	// Fence marks the pass that follows a sweep to catch the tokens Dex was
	// issuing while the sweep ran.
	Fence bool
}

// EnqueueCredentialsRecheck adds one pass for the user, due at dueAt.
func (s *SqlStore) EnqueueCredentialsRecheck(ctx context.Context, accountID, userID string, dueAt time.Time,
	fence bool) error {
	row := credentialsRecheck{
		ID: xid.New().String(), AccountID: accountID, UserID: userID,
		DueAt: dueAt.UTC().Truncate(time.Second), Fence: fence,
	}
	if err := s.db.Create(&row).Error; err != nil {
		log.WithContext(ctx).Errorf("failed to enqueue a credentials recheck for user %s: %s", userID, err)
		return status.Errorf(status.Internal, "failed to enqueue a credentials recheck")
	}
	return nil
}

// ListDueCredentialsRechecks returns at most limit passes due at or before
// now, ordered by due time then id, starting after the pass after (nil for
// the first page). Paging by cursor lets a caller move past passes that
// keep failing instead of reading the same oldest page forever.
func (s *SqlStore) ListDueCredentialsRechecks(ctx context.Context, now time.Time, after *CredentialsRecheck,
	limit int) ([]CredentialsRecheck, error) {
	query := s.db.Where("due_at <= ?", now.UTC())
	if after != nil {
		query = query.Where("(due_at > ? OR (due_at = ? AND id > ?))", after.DueAt, after.DueAt, after.ID)
	}
	var rows []credentialsRecheck
	result := query.Order("due_at, id").Limit(limit).Find(&rows)
	if result.Error != nil {
		log.WithContext(ctx).Errorf("failed to list due credentials rechecks: %s", result.Error)
		return nil, status.Errorf(status.Internal, "failed to list due credentials rechecks")
	}
	due := make([]CredentialsRecheck, 0, len(rows))
	for _, r := range rows {
		due = append(due, CredentialsRecheck{ID: r.ID, AccountID: r.AccountID, UserID: r.UserID, DueAt: r.DueAt,
			Fence: r.Fence})
	}
	return due, nil
}

// CredentialsRecheckExists reports whether a pass is still pending. A worker
// checks it after taking the user's identity lock: a pass another replica
// already ran is gone and must not run again.
func (s *SqlStore) CredentialsRecheckExists(ctx context.Context, id string) (bool, error) {
	var n int64
	if err := s.db.Model(&credentialsRecheck{}).Where(idQueryCondition, id).Count(&n).Error; err != nil {
		log.WithContext(ctx).Errorf("failed to read credentials recheck %s: %s", id, err)
		return false, status.Errorf(status.Internal, "failed to read a credentials recheck")
	}
	return n > 0, nil
}

// DeleteCredentialsRecheck marks a pass as done. Deleting a pass that is
// already gone is not an error.
func (s *SqlStore) DeleteCredentialsRecheck(ctx context.Context, id string) error {
	if err := s.db.Where(idQueryCondition, id).Delete(&credentialsRecheck{}).Error; err != nil {
		log.WithContext(ctx).Errorf("failed to delete credentials recheck %s: %s", id, err)
		return status.Errorf(status.Internal, "failed to delete a credentials recheck")
	}
	return nil
}

// FinishCredentialsRecheck marks a pass as done and, for a sweep, enqueues
// its fence due at fenceAt, in one transaction. Only the caller that deletes
// the row enqueues the fence, so finishing the same sweep again (a retry
// after a crash, or another replica) adds no second fence.
func (s *SqlStore) FinishCredentialsRecheck(ctx context.Context, recheck CredentialsRecheck, fenceAt time.Time) error {
	return s.transaction(ctx, s.db, func(tx *gorm.DB) error {
		result := tx.Where(idQueryCondition, recheck.ID).Delete(&credentialsRecheck{})
		if result.Error != nil {
			log.WithContext(ctx).Errorf("failed to finish credentials recheck %s: %s", recheck.ID, result.Error)
			return status.Errorf(status.Internal, "failed to finish a credentials recheck")
		}
		if result.RowsAffected == 0 || recheck.Fence {
			return nil
		}
		fence := credentialsRecheck{
			ID: xid.New().String(), AccountID: recheck.AccountID, UserID: recheck.UserID,
			DueAt: fenceAt.UTC().Truncate(time.Second), Fence: true,
		}
		if err := tx.Create(&fence).Error; err != nil {
			log.WithContext(ctx).Errorf("failed to enqueue the fence of credentials recheck %s: %s", recheck.ID, err)
			return status.Errorf(status.Internal, "failed to enqueue a credentials fence")
		}
		return nil
	})
}
