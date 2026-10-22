package store

import (
	"context"
	"errors"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

// ErrPeerLoginMFAApprovalNotFound is returned when an approval record does
// not exist, including after the sweep removed it.
var ErrPeerLoginMFAApprovalNotFound = errors.New("store: peer login approval not found")

// pendingApproval matches records that can still be approved or denied.
const pendingApproval = "approved_at IS NULL AND denied_at IS NULL AND consumed_at IS NULL AND expires_at > ?"

func peerLoginMFAInternal(ctx context.Context, op string, err error) error {
	log.WithContext(ctx).Errorf("failed to %s peer login approval: %v", op, err)
	return status.Errorf(status.Internal, "failed to %s peer login approval", op)
}

// CreatePeerLoginMFAApproval inserts a new approval record.
func (s *SqlStore) CreatePeerLoginMFAApproval(ctx context.Context, approval *types.PeerLoginMFAApproval) error {
	if err := s.db.Create(approval).Error; err != nil {
		return peerLoginMFAInternal(ctx, "create", err)
	}
	return nil
}

// GetPeerLoginMFAApprovalByID returns one record.
func (s *SqlStore) GetPeerLoginMFAApprovalByID(ctx context.Context, lockStrength LockingStrength,
	id string) (*types.PeerLoginMFAApproval, error) {
	tx := s.db
	if lockStrength != LockingStrengthNone {
		tx = tx.Clauses(clause.Locking{Strength: string(lockStrength)})
	}
	var approval types.PeerLoginMFAApproval
	if err := tx.Take(&approval, idQueryCondition, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPeerLoginMFAApprovalNotFound
		}
		return nil, peerLoginMFAInternal(ctx, "get", err)
	}
	return &approval, nil
}

// GetUserPeerLoginMFAApprovals returns the user's records that have not
// expired at now, in any state.
func (s *SqlStore) GetUserPeerLoginMFAApprovals(ctx context.Context, lockStrength LockingStrength,
	accountID, userID string, now time.Time) ([]*types.PeerLoginMFAApproval, error) {
	tx := s.db
	if lockStrength != LockingStrengthNone {
		tx = tx.Clauses(clause.Locking{Strength: string(lockStrength)})
	}
	var approvals []*types.PeerLoginMFAApproval
	err := tx.Where("account_id = ? AND user_id = ? AND expires_at > ?", accountID, userID, now).
		Order("created_at").Find(&approvals).Error
	if err != nil {
		return nil, peerLoginMFAInternal(ctx, "list", err)
	}
	return approvals, nil
}

// ApprovePeerLoginMFAApproval moves a pending record to approved and sets
// its expiry. It reports whether the record was pending at now.
func (s *SqlStore) ApprovePeerLoginMFAApproval(ctx context.Context, id string, now, expiresAt time.Time) (bool, error) {
	result := s.db.Model(&types.PeerLoginMFAApproval{}).
		Where("id = ? AND "+pendingApproval, id, now).
		Updates(map[string]any{"approved_at": now, "expires_at": expiresAt})
	if result.Error != nil {
		return false, peerLoginMFAInternal(ctx, "approve", result.Error)
	}
	return result.RowsAffected == 1, nil
}

// DenyPeerLoginMFAApproval moves a pending record to denied. It reports
// whether the record was pending at now.
func (s *SqlStore) DenyPeerLoginMFAApproval(ctx context.Context, id string, now time.Time) (bool, error) {
	result := s.db.Model(&types.PeerLoginMFAApproval{}).
		Where("id = ? AND "+pendingApproval, id, now).
		Update("denied_at", now)
	if result.Error != nil {
		return false, peerLoginMFAInternal(ctx, "deny", result.Error)
	}
	return result.RowsAffected == 1, nil
}

// ConsumePeerLoginMFAApproval consumes the approved, unconsumed, unexpired
// record of this key, if there is one. It runs in the login transaction:
// the oldest such record is locked and then consumed by a conditional
// update, which counts only when it changes exactly that row.
func (s *SqlStore) ConsumePeerLoginMFAApproval(ctx context.Context, accountID, userID, wgPubKey string,
	now time.Time) (bool, error) {
	usable := "account_id = ? AND user_id = ? AND wg_pub_key = ? AND approved_at IS NOT NULL AND " +
		"denied_at IS NULL AND consumed_at IS NULL AND expires_at > ?"
	var candidate types.PeerLoginMFAApproval
	err := s.db.Clauses(clause.Locking{Strength: string(LockingStrengthUpdate)}).
		Where(usable, accountID, userID, wgPubKey, now).Order("created_at").Take(&candidate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, peerLoginMFAInternal(ctx, "find consumable", err)
	}
	result := s.db.Model(&types.PeerLoginMFAApproval{}).
		Where("id = ? AND "+usable, candidate.ID, accountID, userID, wgPubKey, now).
		Update("consumed_at", now)
	if result.Error != nil {
		return false, peerLoginMFAInternal(ctx, "consume", result.Error)
	}
	return result.RowsAffected == 1, nil
}

// GetExpiredPeerLoginMFAApprovals returns up to limit records expired at
// now, for the sweep.
func (s *SqlStore) GetExpiredPeerLoginMFAApprovals(ctx context.Context, now time.Time,
	limit int) ([]*types.PeerLoginMFAApproval, error) {
	var approvals []*types.PeerLoginMFAApproval
	err := s.db.Where("expires_at <= ?", now).Order("expires_at").Limit(limit).Find(&approvals).Error
	if err != nil {
		return nil, peerLoginMFAInternal(ctx, "list expired", err)
	}
	return approvals, nil
}

// DeleteExpiredPeerLoginMFAApproval deletes a record expired at now and
// reports whether this call deleted it, so of replicas sweeping the same
// record exactly one reports it.
func (s *SqlStore) DeleteExpiredPeerLoginMFAApproval(ctx context.Context, id string, now time.Time) (bool, error) {
	result := s.db.Where("id = ? AND expires_at <= ?", id, now).Delete(&types.PeerLoginMFAApproval{})
	if result.Error != nil {
		return false, peerLoginMFAInternal(ctx, "sweep", result.Error)
	}
	return result.RowsAffected == 1, nil
}

// DeleteUntouchedExpiredPeerLoginMFAApproval deletes a record expired at
// now that nobody approved, denied or consumed, and reports whether this
// call deleted it. Its WHERE is evaluated against the row as it stands
// when the DELETE gets the row, so a denial that committed meanwhile makes
// it delete nothing.
func (s *SqlStore) DeleteUntouchedExpiredPeerLoginMFAApproval(ctx context.Context, id string, now time.Time) (bool, error) {
	result := s.db.Where("id = ? AND expires_at <= ? AND approved_at IS NULL AND denied_at IS NULL AND consumed_at IS NULL",
		id, now).Delete(&types.PeerLoginMFAApproval{})
	if result.Error != nil {
		return false, peerLoginMFAInternal(ctx, "sweep", result.Error)
	}
	return result.RowsAffected == 1, nil
}

// DeleteUserPeerLoginMFAApprovals deletes every record of the user, as a
// 2FA reset, a password reset or the user's deletion requires.
func (s *SqlStore) DeleteUserPeerLoginMFAApprovals(ctx context.Context, accountID, userID string) (int64, error) {
	result := s.db.Where("account_id = ? AND user_id = ?", accountID, userID).
		Delete(&types.PeerLoginMFAApproval{})
	if result.Error != nil {
		return 0, peerLoginMFAInternal(ctx, "delete user", result.Error)
	}
	return result.RowsAffected, nil
}

// peerLoginGate is the per-account row that orders device logins against
// settings saves for the device-login 2FA gate (ADR-0028 D3). A login with
// a JWT share-locks it and a settings save locks it exclusively, each as
// its first lock, and nothing else takes it, so it adds no lock-order edge.
// It is not the account row: SaveAccount deletes and re-inserts that row,
// and many writers lock it after groups or users. Rows are created on first
// use and removed with the account.
type peerLoginGate struct {
	AccountID string `gorm:"primaryKey;size:255"`
}

// TableName pins the table name.
func (peerLoginGate) TableName() string { return "peer_login_gates" }

// LockPeerLoginGate locks the account's gate row at lockStrength, creating
// it on first use. It looks the row up before inserting: an upsert on an
// existing row would lock it exclusively on MySQL, serializing every login.
// Concurrent first inserts wait on each other, and the loser inserts
// nothing, so both lock the same committed row.
func (s *SqlStore) LockPeerLoginGate(ctx context.Context, lockStrength LockingStrength, accountID string) error {
	lock := func() (bool, error) {
		var row peerLoginGate
		err := s.db.Clauses(clause.Locking{Strength: string(lockStrength)}).
			Where("account_id = ?", accountID).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return err == nil, err
	}
	locked, err := lock()
	if err == nil && !locked {
		err = s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&peerLoginGate{AccountID: accountID}).Error
		if err == nil {
			locked, err = lock()
		}
	}
	if err != nil {
		return peerLoginMFAInternal(ctx, "lock gate", err)
	}
	if !locked {
		return status.Errorf(status.Internal, "peer login gate for account %s missing after create", accountID)
	}
	return nil
}
