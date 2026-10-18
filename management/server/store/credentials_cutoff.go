package store

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

// colCredentialsValidAfter is the users column holding the credentials
// cutoff set by a local password reset (ADR-0027).
const colCredentialsValidAfter = "credentials_valid_after"

// SetUserCredentialsValidAfter records that the user's JWTs issued at or
// before t are no longer accepted (ADR-0027 D2). It is a targeted UPDATE of
// that one column, never a full-row save, and it is monotonic: an older or
// equal t leaves the stored cutoff unchanged. t is stored in UTC truncated
// to the second, the precision of a JWT iat. It returns NotFound when the
// account has no such user.
//
// It runs under the account mutex so that SaveAccount, which re-inserts
// every user of the account, carries over a value that cannot change under
// it (see keepCredentialsCutoffs).
func (s *SqlStore) SetUserCredentialsValidAfter(ctx context.Context, accountID, userID string, t time.Time) error {
	cutoff := t.UTC().Truncate(time.Second)
	return s.underAccountLock(ctx, accountID, func(tx *gorm.DB) error {
		result := tx.Model(&types.User{}).
			Where(accountAndIDQueryCondition+" AND ("+colCredentialsValidAfter+" IS NULL OR "+
				colCredentialsValidAfter+" < ?)", accountID, userID, cutoff).
			Update(colCredentialsValidAfter, cutoff)
		if result.Error != nil {
			log.WithContext(ctx).Errorf("failed to set the credentials cutoff of user %s: %s", userID, result.Error)
			return status.Errorf(status.Internal, "failed to set the credentials cutoff")
		}
		if result.RowsAffected > 0 {
			return nil
		}
		// No row changed: either the cutoff is already at or past t, or
		// there is no such user in this account.
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

// GetUserCredentialsValidAfter returns the user's credentials cutoff, or nil
// when it has none. It reads that one column only: token validation calls it
// on every JWT request (ADR-0027). It returns NotFound when there is no such
// user.
func (s *SqlStore) GetUserCredentialsValidAfter(ctx context.Context, userID string) (*time.Time, error) {
	var rows []struct{ CredentialsValidAfter *time.Time }
	result := s.db.Model(&types.User{}).Select(colCredentialsValidAfter).
		Where(idQueryCondition, userID).Limit(1).Find(&rows)
	if result.Error != nil {
		log.WithContext(ctx).Errorf("failed to read the credentials cutoff of user %s: %s", userID, result.Error)
		return nil, status.Errorf(status.Internal, "failed to read the credentials cutoff")
	}
	if len(rows) == 0 {
		return nil, status.NewUserNotFoundError(userID)
	}
	return rows[0].CredentialsValidAfter, nil
}

// keepCredentialsCutoffs overwrites the cutoff of every user in a
// SaveAccount snapshot with the persisted one, so that re-inserting the
// account's users from a snapshot read before a password reset cannot erase
// or roll back the reset's cutoff (ADR-0027 D2). It must run inside
// SaveAccount's transaction after the account mutex is taken, which
// SetUserCredentialsValidAfter also takes, so the values read here are
// stable until the transaction commits.
func keepCredentialsCutoffs(tx *gorm.DB, account *types.Account) error {
	type row struct {
		ID                    string
		CredentialsValidAfter *time.Time
	}
	var rows []row
	result := tx.Model(&types.User{}).
		Select("id", colCredentialsValidAfter).
		Where("account_id = ? AND "+colCredentialsValidAfter+" IS NOT NULL", account.Id).
		Find(&rows)
	if result.Error != nil {
		return status.Errorf(status.Internal, "failed to read credentials cutoffs of account %s: %v",
			account.Id, result.Error)
	}
	persisted := make(map[string]*time.Time, len(rows))
	for _, r := range rows {
		persisted[r.ID] = r.CredentialsValidAfter
	}
	for i := range account.UsersG {
		account.UsersG[i].CredentialsValidAfter = persisted[account.UsersG[i].Id]
	}
	return nil
}
