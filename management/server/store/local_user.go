package store

import (
	"context"
	"fmt"
	"slices"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

// localGuardBatchSize bounds the IN list of the guard's state read, so a
// SaveAccount over a very large account stays under driver placeholder
// limits.
const localGuardBatchSize = 400

// Column names of the local-user fields on the users table.
const (
	colLocalUserState   = "local_credential_state"
	colLocalUserCleanup = "local_cleanup_pending"
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
	// Sorted and de-duplicated, and each batch read ORDER BY id, so
	// overlapping concurrent saves acquire row locks in the same order.
	ids := make([]string, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.Id)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)

	persisted := make(map[string]persistedLocalState, len(ids))
	for start := 0; start < len(ids); start += localGuardBatchSize {
		end := min(start+localGuardBatchSize, len(ids))
		var rows []persistedLocalState
		result := tx.Clauses(clause.Locking{Strength: string(LockingStrengthUpdate)}).
			Model(&types.User{}).
			Select("id", colLocalUserState, colLocalUserCleanup).
			Where("id IN ?", ids[start:end]).
			Order("id").
			Find(&rows)
		if result.Error != nil {
			return status.Errorf(status.Internal, "failed to read local user state: %v", result.Error)
		}
		for _, r := range rows {
			persisted[r.ID] = r
		}
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
	return s.transaction(ctx, s.db, func(tx *gorm.DB) error {
		// Serializes against SaveAccount, which rewrites the account's users
		// from a snapshot under the same lock (guardAccountSnapshot).
		if err := lockAccount(tx, user.AccountID); err != nil {
			return err
		}
		var accounts int64
		if result := tx.Model(&types.Account{}).Where(idQueryCondition, user.AccountID).Count(&accounts); result.Error != nil {
			return status.Errorf(status.Internal, "failed to read account %s: %v", user.AccountID, result.Error)
		}
		if accounts == 0 {
			return status.NewAccountNotFoundError(user.AccountID)
		}
		if result := tx.Create(user); result.Error != nil {
			log.WithContext(ctx).Errorf("failed to create pending local user %s: %s", user.Id, result.Error)
			return status.Errorf(status.Internal, "failed to create pending local user")
		}
		return nil
	})
}

// accountLock is the per-account mutex row that serializes SaveAccount
// against local user reservations (ADR-0024 D3). It lives in its own table
// because SaveAccount deletes and re-inserts the account row itself: a lock
// waiting on that row wakes, under Postgres READ COMMITTED, to a deleted
// tuple and finds nothing, so it cannot be the serialization point. Rows
// here are never deleted by SaveAccount.
type accountLock struct {
	AccountID string `gorm:"primaryKey;size:255"`
}

// TableName pins the table name.
func (accountLock) TableName() string { return "account_locks" }

// lockAccount takes the account's mutex row, creating it on first use.
// Concurrent first inserts of the same key wait on each other and the loser
// does nothing, so both sides end up locking the same committed row.
func lockAccount(tx *gorm.DB, accountID string) error {
	if result := tx.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&accountLock{AccountID: accountID}); result.Error != nil {
		return status.Errorf(status.Internal, "failed to create lock for account %s: %v", accountID, result.Error)
	}
	var row accountLock
	if result := tx.Clauses(clause.Locking{Strength: string(LockingStrengthUpdate)}).
		Where("account_id = ?", accountID).Take(&row); result.Error != nil {
		return status.Errorf(status.Internal, "failed to lock account %s: %v", accountID, result.Error)
	}
	return nil
}

// guardAccountSnapshot protects local users from SaveAccount, which deletes
// and re-inserts every user of the account from an in-memory snapshot
// (ADR-0024 D3). Lock order is account_locks -> accounts -> users. Under
// those locks it refuses a snapshot that is
// missing any persisted local user: deleting one would leave its Dex
// credential without a user record. The users present in the snapshot then
// go through guardLocalUserWrites. A new account (SaveAccount also creates
// accounts) simply has no local users.
func guardAccountSnapshot(tx *gorm.DB, account *types.Account) error {
	if err := lockAccount(tx, account.Id); err != nil {
		return err
	}
	// Then the account row itself, before any user row: SCIM and group
	// flows lock accounts and then users (LockAccount, then SaveUser), so
	// locking users first here would invert that order and deadlock. The
	// account_locks mutex already excludes other SaveAccounts, so this row
	// is not being deleted under us; a new account has no row yet, which is
	// fine.
	var existing []string
	if result := tx.Clauses(clause.Locking{Strength: string(LockingStrengthUpdate)}).
		Model(&types.Account{}).Where(idQueryCondition, account.Id).Pluck("id", &existing); result.Error != nil {
		return status.Errorf(status.Internal, "failed to lock account %s: %v", account.Id, result.Error)
	}
	var localIDs []string
	result := tx.Model(&types.User{}).
		Where("account_id = ? AND "+colLocalUserState+" <> ?", account.Id, types.LocalCredentialNone).
		Pluck("id", &localIDs)
	if result.Error != nil {
		return status.Errorf(status.Internal, "failed to read local users of account %s: %v", account.Id, result.Error)
	}
	for _, id := range localIDs {
		if _, ok := account.Users[id]; !ok {
			return status.Errorf(status.PreconditionFailed,
				"account %s snapshot is stale: it is missing local user %s", account.Id, id)
		}
	}
	return guardLocalUserWrites(tx, snapshotUsers(account))
}

func snapshotUsers(account *types.Account) []*types.User {
	users := make([]*types.User, 0, len(account.Users))
	for _, u := range account.Users {
		users = append(users, u)
	}
	return users
}

// DeletePendingLocalUser discards a reservation that never became a user.
// It only deletes a record that is still pending.
func (s *SqlStore) DeletePendingLocalUser(ctx context.Context, accountID, userID string) error {
	result := s.db.WithContext(ctx).
		Where(accountAndIDQueryCondition+" AND "+colLocalUserState+" = ?",
			accountID, userID, types.LocalCredentialPending).
		Delete(&types.User{})
	return fencedResult(ctx, result, "delete pending local user", userID)
}

// FinalizeLocalUser completes a create (ADR-0024 D4 step 4): pending and
// blocked becomes active and unblocked. It is the only operation that may
// unblock a local user.
func (s *SqlStore) FinalizeLocalUser(ctx context.Context, accountID, userID string) error {
	result := s.db.WithContext(ctx).Model(&types.User{}).
		Where(accountAndIDQueryCondition+" AND "+colLocalUserState+" = ? AND blocked = ? AND "+
			colLocalUserCleanup+" = ?", accountID, userID, types.LocalCredentialPending, true, false).
		Updates(map[string]any{colLocalUserState: types.LocalCredentialActive, "blocked": false})
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
		Where(accountAndIDQueryCondition+" AND "+colLocalUserState+" IN ?", accountID, userID, from).
		Updates(map[string]any{colLocalUserState: to, "blocked": true, colLocalUserCleanup: true})
	return fencedResult(ctx, result, fmt.Sprintf("mark local user %s", to), userID)
}

// ClearLocalUserCleanup records that the cleanup of a terminal transition
// converged (ADR-0024 D7 step 6). It is fenced on the expected state and on
// a cleanup actually being pending.
func (s *SqlStore) ClearLocalUserCleanup(ctx context.Context, accountID, userID string, state types.LocalCredentialState) error {
	result := s.db.WithContext(ctx).Model(&types.User{}).
		Where(accountAndIDQueryCondition+" AND "+colLocalUserState+" = ? AND "+colLocalUserCleanup+" = ?",
			accountID, userID, state, true).
		Update(colLocalUserCleanup, false)
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
