package store

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/openzro/openzro/management/server/migration"
	resourceTypes "github.com/openzro/openzro/management/server/networks/resources/types"
	"github.com/openzro/openzro/management/server/posture"
	"github.com/openzro/openzro/management/server/types"
)

// #159 and #160 narrowed these two name columns to varchar(128), and
// AutoMigrate applies that to databases that already hold longer values. The
// engines disagree about what that means, and the disagreement is the bug:
//
//	postgres  the change succeeds and the value is cut to 128 — silently
//	mysql     the change fails with 1406 and the value is untouched
//
// PostgreSQL truncates because the narrowing is applied as an explicit cast to
// varchar(128), and an explicit cast truncates where an assignment would have
// raised "value too long". So the upgrade completes, the service starts, and
// a name is now a different name with nothing said about it.
//
// The guard runs before AutoMigrate and refuses, so both engines behave like
// the one that fails loudly. Documentation cannot cover this: the operator who
// forgets the pre-check query is exactly the one who loses data.
func TestRefuseOversizedColumn(t *testing.T) {
	runTestForAllEngines(t, "", func(t *testing.T, store Store) {
		ctx := context.Background()
		db := store.(*SqlStore).GetDB()

		account := newAccountWithId(ctx, "guard-acc", "guard-user", "")
		require.NoError(t, store.SaveAccount(ctx, account))

		t.Run("passes when every name fits", func(t *testing.T) {
			require.NoError(t, migration.RefuseOversizedColumn[posture.Checks](ctx, db, "name", migration.MaxNameLength))
			require.NoError(t, migration.RefuseOversizedColumn[resourceTypes.NetworkResource](ctx, db, "name", migration.MaxNameLength))
		})

		t.Run("passes at exactly the limit", func(t *testing.T) {
			require.NoError(t, db.Exec(
				"INSERT INTO posture_checks (id, account_id, name) VALUES (?, ?, ?)",
				"exactly-128", account.Id, strings.Repeat("a", migration.MaxNameLength)).Error)
			t.Cleanup(func() { db.Exec("DELETE FROM posture_checks WHERE id = ?", "exactly-128") })

			require.NoError(t, migration.RefuseOversizedColumn[posture.Checks](ctx, db, "name", migration.MaxNameLength),
				"a name of exactly the limit fits and must not be refused")
		})

		t.Run("refuses one character over, and says which row", func(t *testing.T) {
			// The guard runs before AutoMigrate, so the state it sees is the
			// one from before the narrowing: a column still wide enough to
			// hold the offending value. Reproduced here, because the current
			// schema would reject the insert and the test would be measuring
			// the column instead of the guard.
			restore := widenNameColumn(t, db, "posture_checks")
			t.Cleanup(restore)

			require.NoError(t, db.Exec(
				"INSERT INTO posture_checks (id, account_id, name) VALUES (?, ?, ?)",
				"one-over", account.Id, strings.Repeat("a", migration.MaxNameLength+1)).Error)
			t.Cleanup(func() { db.Exec("DELETE FROM posture_checks WHERE id = ?", "one-over") })

			err := migration.RefuseOversizedColumn[posture.Checks](ctx, db, "name", migration.MaxNameLength)
			require.Error(t, err, "a name over the limit must stop the upgrade")
			require.ErrorContains(t, err, "one-over", "the operator has to be told which row to fix")
			require.ErrorContains(t, err, "upgrade-notes.md")
		})

		t.Run("counts characters, not bytes", func(t *testing.T) {
			// 100 three-byte characters: 100 long, 300 bytes. Under the limit
			// by the only measure that matters, and over it by the wrong one.
			require.NoError(t, db.Exec(
				"INSERT INTO posture_checks (id, account_id, name) VALUES (?, ?, ?)",
				"multibyte", account.Id, strings.Repeat("日", 100)).Error)
			t.Cleanup(func() { db.Exec("DELETE FROM posture_checks WHERE id = ?", "multibyte") })

			require.NoError(t, migration.RefuseOversizedColumn[posture.Checks](ctx, db, "name", migration.MaxNameLength),
				"a 100-character name must fit even though it is 300 bytes")
		})

		_ = types.PrivateCategory
	})
}

// widenNameColumn puts the name column back to the shape it had before the
// size:128 tag, so a test can hold a value the current schema would refuse.
// Returns a function that narrows it again.
//
// The unique index has to come off first on MySQL, which will not accept a
// TEXT column in a key without a prefix length — the same reason the column
// was sized in the first place.
func widenNameColumn(t *testing.T, db *gorm.DB, table string) func() {
	t.Helper()

	index := "idx_" + table + "_account_name"
	switch db.Dialector.Name() {
	case "mysql":
		require.NoError(t, db.Exec("DROP INDEX "+index+" ON "+table).Error)
		require.NoError(t, db.Exec("ALTER TABLE "+table+" MODIFY name longtext").Error)
		return func() {
			db.Exec("ALTER TABLE " + table + " MODIFY name varchar(128)")
			db.Exec("CREATE UNIQUE INDEX " + index + " ON " + table + " (account_id, name)")
		}
	case "postgres":
		require.NoError(t, db.Exec("ALTER TABLE "+table+" ALTER COLUMN name TYPE text").Error)
		return func() {
			db.Exec("ALTER TABLE " + table + " ALTER COLUMN name TYPE varchar(128)")
		}
	default:
		// SQLite does not enforce a varchar width, so the column already
		// accepts the value and there is nothing to widen.
		return func() {}
	}
}
