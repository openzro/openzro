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
	Blocked              bool
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
//
// lockRows makes the state read a locking read (FOR UPDATE). Generic user
// saves need it. SaveAccount does not: it already holds the account mutex
// that every local-state operation takes, and locking user rows ahead of
// its own statements would add lock-order edges (users before accounts)
// that its whole-tree rewrite does not otherwise have.
func guardLocalUserWrites(tx *gorm.DB, users []*types.User, lockRows bool) error {
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
		read := tx
		if lockRows {
			read = tx.Clauses(clause.Locking{Strength: string(LockingStrengthUpdate)})
		}
		result := read.Model(&types.User{}).
			Select("id", colLocalUserState, colLocalUserCleanup, "blocked").
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
		// An active local user is only ever blocked by a terminal
		// transition (SCIM deactivate/delete). Blocking it through a generic
		// save would let a later unblock emulate re-activation.
		if p.LocalCredentialState == types.LocalCredentialActive && u.Blocked != p.Blocked {
			return status.Errorf(status.BadRequest,
				"local user %s can only be blocked by deactivating it through SCIM", u.Id)
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
// (ADR-0024 D3). It takes the account mutex and nothing else up front: every
// operation that changes local user state takes the same mutex, so the
// state read here is stable without row locks. SaveAccount's own statements
// then lock rows exactly as they do without local users, so this adds no
// lock-order edge beyond a leading mutex no other flow holds while waiting.
//
// It refuses a snapshot missing any persisted local user (deleting one
// would leave its Dex credential without a user record), and checks the
// snapshot's local users with guardLocalUserWrites. A new account
// (SaveAccount also creates accounts) simply has no local users.
func guardAccountSnapshot(tx *gorm.DB, account *types.Account) error {
	if err := lockAccount(tx, account.Id); err != nil {
		return err
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
	return guardLocalUserWrites(tx, snapshotUsers(account), false)
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
	return s.underAccountLock(ctx, accountID, func(tx *gorm.DB) error {
		result := tx.Where(accountAndIDQueryCondition+" AND "+colLocalUserState+" = ?",
			accountID, userID, types.LocalCredentialPending).
			Delete(&types.User{})
		return fencedResult(ctx, result, "delete pending local user", userID)
	})
}

// underAccountLock runs op in a transaction holding the account mutex. Every
// operation that changes local user state goes through it, which is what
// lets SaveAccount read that state without row locks.
//
// The mutex must be the first lock a transaction takes: a caller that runs
// one of these operations inside an outer transaction must not already hold
// user or account row locks, or it inverts the order SaveAccount relies on.
func (s *SqlStore) underAccountLock(ctx context.Context, accountID string, op func(tx *gorm.DB) error) error {
	return s.transaction(ctx, s.db, func(tx *gorm.DB) error {
		if err := lockAccount(tx, accountID); err != nil {
			return err
		}
		return op(tx)
	})
}

// FinalizeLocalUser completes a create (ADR-0024 D4 step 4): pending and
// blocked becomes active and unblocked. It is the only operation that may
// unblock a local user.
func (s *SqlStore) FinalizeLocalUser(ctx context.Context, accountID, userID string) error {
	return s.underAccountLock(ctx, accountID, func(tx *gorm.DB) error {
		result := tx.Model(&types.User{}).
			Where(accountAndIDQueryCondition+" AND "+colLocalUserState+" = ? AND blocked = ? AND "+
				colLocalUserCleanup+" = ?", accountID, userID, types.LocalCredentialPending, true, false).
			Updates(map[string]any{colLocalUserState: types.LocalCredentialActive, "blocked": false})
		return fencedResult(ctx, result, "finalize local user", userID)
	})
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
	return s.underAccountLock(ctx, accountID, func(tx *gorm.DB) error {
		// The owner fence is part of the UPDATE, so an ownership transfer
		// that commits after any earlier read still cannot leave the account
		// with a permanently blocked owner (ADR-0026 D1).
		result := tx.Model(&types.User{}).
			Where(accountAndIDQueryCondition+" AND "+colLocalUserState+" IN ? AND role <> ?",
				accountID, userID, from, types.UserRoleOwner).
			Updates(map[string]any{colLocalUserState: to, "blocked": true, colLocalUserCleanup: true})
		if result.Error == nil && result.RowsAffected == 0 {
			if err := refuseOwnerTermination(tx, accountID, userID); err != nil {
				return err
			}
		}
		return fencedResult(ctx, result, fmt.Sprintf("mark local user %s", to), userID)
	})
}

// refuseOwnerTermination explains a terminal UPDATE that matched no row:
// when the user is an owner, the refusal is a permission error rather than
// a fencing failure.
func refuseOwnerTermination(tx *gorm.DB, accountID, userID string) error {
	var roles []types.UserRole
	result := tx.Model(&types.User{}).Where(accountAndIDQueryCondition, accountID, userID).Pluck("role", &roles)
	if result.Error != nil {
		return status.Errorf(status.Internal, "failed to read the role of user %s: %v", userID, result.Error)
	}
	if len(roles) == 1 && roles[0] == types.UserRoleOwner {
		return status.Errorf(status.PermissionDenied, "can't deactivate or delete a user with the owner role")
	}
	return nil
}

// ClearLocalUserCleanup records that the cleanup of a terminal transition
// converged (ADR-0024 D7 step 6). It is fenced on the expected state and on
// a cleanup actually being pending.
func (s *SqlStore) ClearLocalUserCleanup(ctx context.Context, accountID, userID string, state types.LocalCredentialState) error {
	return s.underAccountLock(ctx, accountID, func(tx *gorm.DB) error {
		result := tx.Model(&types.User{}).
			Where(accountAndIDQueryCondition+" AND "+colLocalUserState+" = ? AND "+colLocalUserCleanup+" = ?",
				accountID, userID, state, true).
			Update(colLocalUserCleanup, false)
		return fencedResult(ctx, result, "clear local user cleanup", userID)
	})
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

// refuseLocalUserDelete keeps the generic user delete away from local users
// until their delete saga exists: removing the record would leave the Dex
// credential behind and reopen JIT for its sub (ADR-0024, #205). A user's
// local state is fixed at insert (it never moves between "" and non-empty),
// so reading it on the row the delete locks anyway is stable and adds no
// lock-order edge.
func refuseLocalUserDelete(tx *gorm.DB, accountID, userID string) error {
	var states []types.LocalCredentialState
	result := tx.Clauses(clause.Locking{Strength: string(LockingStrengthUpdate)}).
		Model(&types.User{}).Where(accountAndIDQueryCondition, accountID, userID).
		Pluck(colLocalUserState, &states)
	if result.Error != nil {
		return status.Errorf(status.Internal, "failed to read user %s: %v", userID, result.Error)
	}
	if len(states) > 0 && states[0] != types.LocalCredentialNone {
		return status.Errorf(status.PreconditionFailed,
			"user %s is a local user: deactivate or delete it through SCIM", userID)
	}
	return nil
}

// refuseAccountDeleteWithLocalUsers refuses to delete an account that still
// has local users, for the same reason (ADR-0024, #205). It takes the
// account mutex first and the caller keeps it until the delete commits:
// CreatePendingLocalUser takes the same mutex, so a reservation either
// committed before this count (refused here) or waits and then fails on the
// missing account. Without the mutex a reservation could commit between the
// count and the account-wide deletes, which would remove the new user and
// orphan its Dex credential.
func refuseAccountDeleteWithLocalUsers(tx *gorm.DB, accountID string) error {
	if err := lockAccount(tx, accountID); err != nil {
		return err
	}
	var n int64
	result := tx.Model(&types.User{}).
		Where("account_id = ? AND "+colLocalUserState+" <> ?", accountID, types.LocalCredentialNone).
		Count(&n)
	if result.Error != nil {
		return status.Errorf(status.Internal, "failed to read local users of account %s: %v", accountID, result.Error)
	}
	if n > 0 {
		return status.Errorf(status.PreconditionFailed,
			"account %s has %d local user(s): delete them through SCIM first", accountID, n)
	}
	return nil
}
