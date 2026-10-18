package store

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

// colCredentialsRecheckAt is the users column holding when the second pass
// of a local password reset is due (ADR-0027 D6).
const colCredentialsRecheckAt = "credentials_recheck_at"

// CredentialsRecheck is a due second pass of a local password reset.
type CredentialsRecheck struct {
	AccountID string
	UserID    string
	At        time.Time
}

// ScheduleUserCredentialsRecheck makes the second pass of a reset due at
// at. It is a targeted, monotonic UPDATE under the account mutex, like
// SetUserCredentialsValidAfter: an earlier at leaves a later schedule in
// place. It returns NotFound when the account has no such user.
func (s *SqlStore) ScheduleUserCredentialsRecheck(ctx context.Context, accountID, userID string, at time.Time) error {
	at = at.UTC().Truncate(time.Second)
	return s.underAccountLock(ctx, accountID, func(tx *gorm.DB) error {
		result := tx.Model(&types.User{}).
			Where(accountAndIDQueryCondition+" AND ("+colCredentialsRecheckAt+" IS NULL OR "+
				colCredentialsRecheckAt+" < ?)", accountID, userID, at).
			Update(colCredentialsRecheckAt, at)
		if result.Error != nil {
			log.WithContext(ctx).Errorf("failed to schedule the credentials recheck of user %s: %s", userID, result.Error)
			return status.Errorf(status.Internal, "failed to schedule the credentials recheck")
		}
		if result.RowsAffected > 0 {
			return nil
		}
		var n int64
		if err := tx.Model(&types.User{}).Where(accountAndIDQueryCondition, accountID, userID).Count(&n).Error; err != nil {
			return status.Errorf(status.Internal, "failed to read user %s: %v", userID, err)
		}
		if n == 0 {
			return status.NewUserNotFoundError(userID)
		}
		return nil
	})
}

// ListUserCredentialsRechecksDue returns every second pass due at or before
// now, across accounts.
func (s *SqlStore) ListUserCredentialsRechecksDue(ctx context.Context, now time.Time) ([]CredentialsRecheck, error) {
	var rows []struct {
		AccountID            string
		ID                   string
		CredentialsRecheckAt time.Time
	}
	result := s.db.Model(&types.User{}).
		Select("account_id", "id", colCredentialsRecheckAt).
		Where(colCredentialsRecheckAt+" IS NOT NULL AND "+colCredentialsRecheckAt+" <= ?", now.UTC()).
		Find(&rows)
	if result.Error != nil {
		log.WithContext(ctx).Errorf("failed to list due credentials rechecks: %s", result.Error)
		return nil, status.Errorf(status.Internal, "failed to list due credentials rechecks")
	}
	due := make([]CredentialsRecheck, 0, len(rows))
	for _, r := range rows {
		due = append(due, CredentialsRecheck{AccountID: r.AccountID, UserID: r.ID, At: r.CredentialsRecheckAt})
	}
	return due, nil
}

// ClearUserCredentialsRecheck marks the second pass scheduled at at as done.
// It only clears that exact schedule: one a newer reset moved later stays.
func (s *SqlStore) ClearUserCredentialsRecheck(ctx context.Context, accountID, userID string, at time.Time) error {
	at = at.UTC().Truncate(time.Second)
	return s.underAccountLock(ctx, accountID, func(tx *gorm.DB) error {
		result := tx.Model(&types.User{}).
			Where(accountAndIDQueryCondition+" AND "+colCredentialsRecheckAt+" = ?", accountID, userID, at).
			Update(colCredentialsRecheckAt, nil)
		if result.Error != nil {
			log.WithContext(ctx).Errorf("failed to clear the credentials recheck of user %s: %s", userID, result.Error)
			return status.Errorf(status.Internal, "failed to clear the credentials recheck")
		}
		return nil
	})
}
