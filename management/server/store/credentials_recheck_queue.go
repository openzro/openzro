package store

import (
	"context"
	"time"

	"github.com/rs/xid"
	log "github.com/sirupsen/logrus"

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
// now, oldest first, so a caller works through a backlog in bounded batches.
func (s *SqlStore) ListDueCredentialsRechecks(ctx context.Context, now time.Time, limit int) ([]CredentialsRecheck, error) {
	var rows []credentialsRecheck
	result := s.db.Where("due_at <= ?", now.UTC()).Order("due_at, id").Limit(limit).Find(&rows)
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
