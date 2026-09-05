//go:build archive_duckdb

package archive

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The detection queries published in docs/operator/upgrade-notes.md.
// They go in a test because an unverified query in operator docs is how
// this whole read path got into production broken.
func TestOperatorDetectionQueries(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	root := t.TempDir()
	// Correctly filed.
	seedEventParquetAs(t, db, partitionPath(root, "2026", "06", "10", "ok.parquet"),
		"2026-06-10 12:00:00", testAccount)
	// Misfiled account: path says acct-1, row says acct-B.
	seedEventParquetAs(t, db, partitionPath(root, "2026", "06", "10", "bad-acct.parquet"),
		"2026-06-10 12:00:00", "acct-B")
	// Misfiled day: path says 06-10, row is 06-12.
	seedEventParquetAs(t, db, partitionPath(root, "2026", "06", "10", "bad-day.parquet"),
		"2026-06-12 12:00:00", testAccount)

	glob := filepath.Join(root, "year=*", "month=*", "day=*", "account=*", "*.parquet")

	t.Run("misfiled accounts", func(t *testing.T) {
		var n int
		require.NoError(t, db.QueryRowContext(context.Background(),
			`SELECT count(*) FROM read_parquet('`+glob+`', hive_partitioning=true)
			 WHERE account_id <> account`).Scan(&n))
		require.Equal(t, 1, n)
	})

	t.Run("misfiled days", func(t *testing.T) {
		var n int
		require.NoError(t, db.QueryRowContext(context.Background(),
			`SELECT count(*) FROM read_parquet('`+glob+`', hive_partitioning=true)
			 WHERE date_trunc('day', received_at)
			       <> make_date(year::INT, month::INT, day::INT)`).Scan(&n))
		require.Equal(t, 1, n)
	})
}
