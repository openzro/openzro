package store

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

// Column names of the local-credential fields on the users table.
const (
	colLocalCredentialState = "local_credential_state"
	colLocalCleanupPending  = "local_cleanup_pending"
)

// persistedLocalState is the slice of a user row the generic-save guard
// compares against.
type persistedLocalState struct {
	ID                   string
	LocalCredentialState types.LocalCredentialState
	LocalCleanupPending  bool
}

// guardLocalUserWrites enforces ADR-0024 D3 on every generic user write
// (SaveUser, SaveUsers, SaveAccount). It must run inside the write's
// transaction: it locks and reads the persisted state of each user, and
// refuses the write if it would
//   - unblock a pending, deactivated or tombstoned local user, or
//   - change LocalCredentialState or LocalCleanupPending at all, including
//     inserting a new user with a non-empty state.
//
// Those transitions belong to the dedicated conditional operations below.
// There is deliberately no flag to bypass this check.
func guardLocalUserWrites(tx *gorm.DB, users []*types.User) error {
	if len(users) == 0 {
		return nil
	}
	ids := make([]string, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.Id)
	}

	var rows []persistedLocalState
	result := tx.Clauses(clause.Locking{Strength: string(LockingStrengthUpdate)}).
		Model(&types.User{}).
		Select("id", colLocalCredentialState, colLocalCleanupPending).
		Where("id IN ?", ids).
		Find(&rows)
	if result.Error != nil {
		return status.Errorf(status.Internal, "failed to read local user state: %v", result.Error)
	}
	persisted := make(map[string]persistedLocalState, len(rows))
	for _, r := range rows {
		persisted[r.ID] = r
	}

	for _, u := range users {
		p, exists := persisted[u.Id]
		if !exists {
			p = persistedLocalState{ID: u.Id}
		}
		if u.LocalCredentialState != p.LocalCredentialState || u.LocalCleanupPending != p.LocalCleanupPending {
			log.Errorf("refused generic save changing local credential state of user %s (%q -> %q)",
				u.Id, p.LocalCredentialState, u.LocalCredentialState)
			return status.Errorf(status.Internal,
				"local credential state of user %s can only change through its dedicated operations", u.Id)
		}
		if p.LocalCredentialState.MustStayBlocked() && !u.Blocked {
			return status.Errorf(status.BadRequest,
				"local user %s is %s and cannot be unblocked: re-activation is not supported",
				u.Id, p.LocalCredentialState)
		}
	}
	return nil
}

// CreatePendingLocalUser inserts the reservation for a new local user
// (ADR-0024 D4 step 2): state pending, always blocked. It fails if a user
// with the same ID already exists.
func (s *SqlStore) CreatePendingLocalUser(ctx context.Context, user *types.User) error {
	user.LocalCredentialState = types.LocalCredentialPending
	user.LocalCleanupPending = false
	user.Blocked = true
	if result := s.db.WithContext(ctx).Create(user); result.Error != nil {
		log.WithContext(ctx).Errorf("failed to create pending local user %s: %s", user.Id, result.Error)
		return status.Errorf(status.Internal, "failed to create pending local user")
	}
	return nil
}

// DeletePendingLocalUser discards a reservation that never became a user.
// It only deletes a record that is still pending.
func (s *SqlStore) DeletePendingLocalUser(ctx context.Context, accountID, userID string) error {
	result := s.db.WithContext(ctx).
		Where(accountAndIDQueryCondition+" AND "+colLocalCredentialState+" = ?",
			accountID, userID, types.LocalCredentialPending).
		Delete(&types.User{})
	return fencedResult(ctx, result, "delete pending local user", userID)
}

// FinalizeLocalUser completes a create (ADR-0024 D4 step 4): pending and
// blocked becomes active and unblocked. It is the only operation that may
// unblock a local user.
func (s *SqlStore) FinalizeLocalUser(ctx context.Context, accountID, userID string) error {
	result := s.db.WithContext(ctx).Model(&types.User{}).
		Where(accountAndIDQueryCondition+" AND "+colLocalCredentialState+" = ? AND blocked = ? AND "+
			colLocalCleanupPending+" = ?", accountID, userID, types.LocalCredentialPending, true, false).
		Updates(map[string]any{colLocalCredentialState: types.LocalCredentialActive, "blocked": false})
	return fencedResult(ctx, result, "finalize local user", userID)
}

// MarkLocalUserTerminal starts a terminal transition (ADR-0024 D7 step 1):
// active -> deactivated, or active/deactivated -> tombstoned. The user is
// blocked and LocalCleanupPending is set until the cleanup converges.
func (s *SqlStore) MarkLocalUserTerminal(ctx context.Context, accountID, userID string, to types.LocalCredentialState) error {
	var from []types.LocalCredentialState
	switch to {
	case types.LocalCredentialDeactivated:
		from = []types.LocalCredentialState{types.LocalCredentialActive}
	case types.LocalCredentialTombstoned:
		from = []types.LocalCredentialState{types.LocalCredentialActive, types.LocalCredentialDeactivated}
	default:
		return status.Errorf(status.InvalidArgument, "%q is not a terminal local user state", to)
	}
	result := s.db.WithContext(ctx).Model(&types.User{}).
		Where(accountAndIDQueryCondition+" AND "+colLocalCredentialState+" IN ?", accountID, userID, from).
		Updates(map[string]any{colLocalCredentialState: to, "blocked": true, colLocalCleanupPending: true})
	return fencedResult(ctx, result, fmt.Sprintf("mark local user %s", to), userID)
}

// ClearLocalUserCleanup records that the cleanup of a terminal transition
// converged (ADR-0024 D7 step 6). It is fenced on the expected state and on
// a cleanup actually being pending.
func (s *SqlStore) ClearLocalUserCleanup(ctx context.Context, accountID, userID string, state types.LocalCredentialState) error {
	result := s.db.WithContext(ctx).Model(&types.User{}).
		Where(accountAndIDQueryCondition+" AND "+colLocalCredentialState+" = ? AND "+colLocalCleanupPending+" = ?",
			accountID, userID, state, true).
		Update(colLocalCleanupPending, false)
	return fencedResult(ctx, result, "clear local user cleanup", userID)
}

// fencedResult maps a conditional write's outcome: a database error is
// Internal, and zero affected rows means the record was not in the expected
// state (or does not exist), reported as PreconditionFailed so the caller
// aborts instead of writing further.
func fencedResult(ctx context.Context, result *gorm.DB, op, userID string) error {
	if result.Error != nil {
		log.WithContext(ctx).Errorf("failed to %s %s: %s", op, userID, result.Error)
		return status.Errorf(status.Internal, "failed to %s", op)
	}
	if result.RowsAffected == 0 {
		return status.Errorf(status.PreconditionFailed, "%s %s: user is not in the expected state", op, userID)
	}
	return nil
}
