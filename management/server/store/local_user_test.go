package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

const localTestAccountID = "bf1c8084-ba50-4ce7-9439-34653001fc3b"

func requireStatus(t *testing.T, err error, want status.Type) {
	t.Helper()
	require.Error(t, err)
	sErr, ok := status.FromError(err)
	require.True(t, ok, "expected a status error, got %v", err)
	require.Equal(t, want, sErr.Type(), "unexpected error: %v", err)
}

func newLocalUser(id string) *types.User {
	u := types.NewRegularUser(id)
	u.AccountID = localTestAccountID
	u.SCIMUserName = id + "@example.com"
	return u
}

func getUser(t *testing.T, s Store, id string) *types.User {
	t.Helper()
	u, err := s.GetUserByUserID(context.Background(), LockingStrengthShare, id)
	require.NoError(t, err)
	return u
}

// ADR-0024 D3/D4: the local user lifecycle only moves through the
// dedicated conditional operations, each fenced on the expected state.
func TestSqlStore_LocalUserLifecycle(t *testing.T) {
	runTestForAllEngines(t, "../testdata/store.sql", func(t *testing.T, s Store) {
		ctx := context.Background()

		require.NoError(t, s.CreatePendingLocalUser(ctx, newLocalUser("ana")))
		u := getUser(t, s, "ana")
		require.Equal(t, types.LocalCredentialPending, u.LocalCredentialState)
		require.True(t, u.Blocked, "a pending local user is always blocked")

		// A pending reservation can be discarded; anything else cannot.
		require.NoError(t, s.CreatePendingLocalUser(ctx, newLocalUser("bea")))
		require.NoError(t, s.DeletePendingLocalUser(ctx, localTestAccountID, "bea"))
		_, err := s.GetUserByUserID(ctx, LockingStrengthShare, "bea")
		requireStatus(t, err, status.NotFound)

		require.NoError(t, s.FinalizeLocalUser(ctx, localTestAccountID, "ana"))
		u = getUser(t, s, "ana")
		require.Equal(t, types.LocalCredentialActive, u.LocalCredentialState)
		require.False(t, u.Blocked)

		// Fencing: finalize only applies to a pending, blocked record.
		requireStatus(t, s.FinalizeLocalUser(ctx, localTestAccountID, "ana"), status.PreconditionFailed)
		requireStatus(t, s.DeletePendingLocalUser(ctx, localTestAccountID, "ana"), status.PreconditionFailed)

		require.NoError(t, s.MarkLocalUserTerminal(ctx, localTestAccountID, "ana", types.LocalCredentialDeactivated))
		u = getUser(t, s, "ana")
		require.Equal(t, types.LocalCredentialDeactivated, u.LocalCredentialState)
		require.True(t, u.Blocked)
		require.True(t, u.LocalCleanupPending)

		require.NoError(t, s.ClearLocalUserCleanup(ctx, localTestAccountID, "ana", types.LocalCredentialDeactivated))
		require.False(t, getUser(t, s, "ana").LocalCleanupPending)
		// Clearing again (nothing pending) is fenced.
		requireStatus(t, s.ClearLocalUserCleanup(ctx, localTestAccountID, "ana", types.LocalCredentialDeactivated),
			status.PreconditionFailed)

		// deactivated -> tombstoned is allowed; tombstoned is final.
		require.NoError(t, s.MarkLocalUserTerminal(ctx, localTestAccountID, "ana", types.LocalCredentialTombstoned))
		u = getUser(t, s, "ana")
		require.Equal(t, types.LocalCredentialTombstoned, u.LocalCredentialState)
		require.True(t, u.LocalCleanupPending)
		requireStatus(t, s.MarkLocalUserTerminal(ctx, localTestAccountID, "ana", types.LocalCredentialDeactivated),
			status.PreconditionFailed)

		// Only terminal targets are accepted, and a pending user cannot be
		// terminated directly (its reservation is discarded instead).
		require.NoError(t, s.CreatePendingLocalUser(ctx, newLocalUser("cid")))
		requireStatus(t, s.MarkLocalUserTerminal(ctx, localTestAccountID, "cid", types.LocalCredentialActive),
			status.InvalidArgument)
		requireStatus(t, s.MarkLocalUserTerminal(ctx, localTestAccountID, "cid", types.LocalCredentialTombstoned),
			status.PreconditionFailed)

		// A reservation needs an existing account.
		missing := newLocalUser("orphan")
		missing.AccountID = "no-such-account"
		requireStatus(t, s.CreatePendingLocalUser(ctx, missing), status.NotFound)

		// The account scope is part of the fence.
		requireStatus(t, s.FinalizeLocalUser(ctx, "other-account", "cid"), status.PreconditionFailed)
	})
}

// ADR-0024 D3: generic user saves (SaveUser, the SaveUsers batch upsert
// behind REST, and SaveAccount) must never unblock a pending, deactivated
// or tombstoned local user, nor move a user between credential states.
func TestSqlStore_GenericSavesCannotReviveLocalUsers(t *testing.T) {
	runTestForAllEngines(t, "../testdata/store.sql", func(t *testing.T, s Store) {
		ctx := context.Background()

		require.NoError(t, s.CreatePendingLocalUser(ctx, newLocalUser("ana")))
		require.NoError(t, s.FinalizeLocalUser(ctx, localTestAccountID, "ana"))
		require.NoError(t, s.MarkLocalUserTerminal(ctx, localTestAccountID, "ana", types.LocalCredentialDeactivated))

		unblock := getUser(t, s, "ana")
		unblock.Blocked = false

		requireStatus(t, s.SaveUser(ctx, LockingStrengthUpdate, unblock), status.BadRequest)
		requireStatus(t, s.SaveUsers(ctx, LockingStrengthUpdate, []*types.User{unblock}), status.BadRequest)

		account, err := s.GetAccount(ctx, localTestAccountID)
		require.NoError(t, err)
		account.Users["ana"].Blocked = false
		requireStatus(t, s.SaveAccount(ctx, account), status.BadRequest)

		require.True(t, getUser(t, s, "ana").Blocked, "no generic save may have unblocked the user")

		// Changing the state field itself through a generic save is refused.
		promote := getUser(t, s, "ana")
		promote.LocalCredentialState = types.LocalCredentialActive
		promote.Blocked = false
		requireStatus(t, s.SaveUser(ctx, LockingStrengthUpdate, promote), status.Internal)

		// Inserting a local user through a generic save is refused too.
		requireStatus(t, s.SaveUser(ctx, LockingStrengthUpdate,
			&types.User{Id: "sneaky", AccountID: localTestAccountID,
				LocalCredentialState: types.LocalCredentialActive}), status.Internal)

		// Other edits keep working: a deactivated user stays editable while
		// it stays blocked, and ordinary users are untouched by the rule.
		edit := getUser(t, s, "ana")
		edit.AutoGroups = []string{"g1"}
		require.NoError(t, s.SaveUser(ctx, LockingStrengthUpdate, edit))
		require.NoError(t, s.SaveUsers(ctx, LockingStrengthUpdate, []*types.User{edit}))

		plain := newLocalUser("plain")
		plain.Blocked = true
		require.NoError(t, s.SaveUser(ctx, LockingStrengthUpdate, plain))
		plain.Blocked = false
		require.NoError(t, s.SaveUsers(ctx, LockingStrengthUpdate, []*types.User{plain}))
		require.False(t, getUser(t, s, "plain").Blocked)
	})
}

// #213 review: SaveAccount deletes and re-inserts the whole account tree
// from an in-memory snapshot. A snapshot read before another replica
// reserved a local user does not contain it, and deleting it would leave
// a Dex credential without a user record (ADR-0024 D3/D4). SaveAccount must
// refuse such a stale snapshot instead of deleting local users.
func TestSqlStore_SaveAccountRefusesSnapshotMissingLocalUsers(t *testing.T) {
	runTestForAllEngines(t, "../testdata/store.sql", func(t *testing.T, s Store) {
		ctx := context.Background()

		// Replica A reads its snapshot...
		snapshot, err := s.GetAccount(ctx, localTestAccountID)
		require.NoError(t, err)
		// ...then replica B reserves a local user.
		require.NoError(t, s.CreatePendingLocalUser(ctx, newLocalUser("pending")))

		requireStatus(t, s.SaveAccount(ctx, snapshot), status.PreconditionFailed)
		pending := getUser(t, s, "pending")
		require.Equal(t, types.LocalCredentialPending, pending.LocalCredentialState)

		// The same holds for an active local user dropped from a snapshot.
		require.NoError(t, s.FinalizeLocalUser(ctx, localTestAccountID, "pending"))
		snapshot, err = s.GetAccount(ctx, localTestAccountID)
		require.NoError(t, err)
		delete(snapshot.Users, "pending")
		requireStatus(t, s.SaveAccount(ctx, snapshot), status.PreconditionFailed)
		require.Equal(t, types.LocalCredentialActive, getUser(t, s, "pending").LocalCredentialState)

		// A fresh, complete snapshot still saves.
		snapshot, err = s.GetAccount(ctx, localTestAccountID)
		require.NoError(t, err)
		require.NoError(t, s.SaveAccount(ctx, snapshot))
		require.Equal(t, types.LocalCredentialActive, getUser(t, s, "pending").LocalCredentialState)
	})
}

// Rows that predate the local-user columns must read as "not a local
// user" in SQL predicates too, not only in Go. This drives the real
// migration on every engine: drop the columns, write a legacy row, let
// AutoMigrate add them back, and require the default to have filled the
// existing row instead of NULL.
func TestSqlStore_LegacyUsersMigrateToEmptyLocalState(t *testing.T) {
	runTestForAllEngines(t, "../testdata/store.sql", func(t *testing.T, s Store) {
		db := s.(*SqlStore).db
		migrator := db.Migrator()
		require.NoError(t, migrator.DropColumn(&types.User{}, colLocalUserState))
		require.NoError(t, migrator.DropColumn(&types.User{}, colLocalUserCleanup))

		legacy := newLocalUser("legacy")
		require.NoError(t, db.Omit(colLocalUserState, colLocalUserCleanup).Create(legacy).Error)

		require.NoError(t, db.AutoMigrate(&types.User{}))

		var nullRows int64
		require.NoError(t, db.Model(&types.User{}).
			Where(colLocalUserState+" IS NULL OR "+colLocalUserCleanup+" IS NULL").
			Count(&nullRows).Error)
		require.Zero(t, nullRows, "legacy rows must get the column defaults, not NULL")

		var localCount int64
		require.NoError(t, db.Model(&types.User{}).
			Where(colLocalUserState+" <> ?", types.LocalCredentialNone).Count(&localCount).Error)
		require.Zero(t, localCount, "a legacy row must not look like a local user")
	})
}
