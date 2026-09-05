//go:build archive_duckdb

package archive

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/flow/store"
)

// countRows drains the result set, so a lazily-surfaced read error on a
// file DuckDB opened during execution is reported rather than swallowed.
func countRows(t *testing.T, db *sql.DB, q string, args []any) (int, error) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), q, args...)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		n++
	}
	return n, rows.Err()
}

// The archive query used to filter only on received_at, a column inside
// the files, so DuckDB opened every object under the account to answer
// anything. A two-month window over a four-month archive read all four,
// ran past the ingress timeout, and returned 504 to the dashboard.
//
// This asserts the skip rather than the speed, which is the only way to
// state it as a property: the excluded partition holds a file that is
// not parquet at all, so any query that reaches it fails. Passing means
// it was never opened.
func TestPartitionPruningSkipsFilesOutsideTheWindow(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	root := t.TempDir()
	seedEventParquet(t, db, partitionPath(root, "2026", "06", "10", "x.parquet"), "2026-06-10 10:00:00")
	seedEventParquet(t, db, partitionPath(root, "2026", "07", "20", "x.parquet"), "2026-07-20 10:00:00")

	corrupt := partitionPath(root, "2026", "09", "05", "x.parquet")
	require.NoError(t, os.MkdirAll(corrupt[:len(corrupt)-len("/x.parquet")], 0o755))
	require.NoError(t, os.WriteFile(corrupt, []byte("not parquet"), 0o644))

	f := store.Filter{
		Since: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Until: time.Date(2026, 7, 31, 23, 59, 59, 0, time.UTC),
	}

	q, args := buildQuery(partitionGlob(root), f)
	n, err := countRows(t, db, q, args)
	require.NoError(t, err,
		"September holds a corrupt file; reaching it means the window was not pruned")
	require.Equal(t, 2, n, "both June and July rows are inside the window")

	// The negative control: the same data, the same window, with the
	// partition predicates stripped. If this also passed, the assertion
	// above would be proving nothing about pruning.
	stripped, args := buildQuery(partitionGlob(root), f)
	stripped = removePartitionBounds(t, stripped)
	_, err = countRows(t, db, stripped, args)
	require.Error(t, err,
		"without the partition predicates every file is opened, corrupt one included")
}

// removePartitionBounds rebuilds the pre-fix query: same select, same
// received_at window, no year/month/day.
func removePartitionBounds(t *testing.T, q string) string {
	t.Helper()
	out := q
	for {
		i := strings.Index(out, " AND (year")
		if i < 0 {
			return out
		}
		// The clause is one balanced parenthesised group after " AND ".
		depth, j := 0, i+len(" AND ")
		for ; j < len(out); j++ {
			switch out[j] {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth == 0 {
				break
			}
		}
		require.Equal(t, 0, depth, "unbalanced partition clause in %q", q)
		out = out[:i] + out[j+1:]
	}
}

// The bounds are a day wider on each side on purpose: the sink names a
// file after the ReceivedAt of the first event in the batch, so a batch
// crossing midnight files its later events under the earlier day. An
// event at 00:05 can therefore live in the previous day's partition, and
// a bound that stopped at the exact day would drop it — without an
// error, which is the failure mode that matters here.
func TestPartitionBoundsAreWidenedByADay(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Filed under the 9th because its batch began there; the event
	// itself is on the 10th.
	root := t.TempDir()
	seedEventParquet(t, db, partitionPath(root, "2026", "06", "09", "x.parquet"), "2026-06-10 00:05:00")

	q, args := buildQuery(partitionGlob(root), store.Filter{
		Since: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
		Until: time.Date(2026, 6, 10, 23, 59, 59, 0, time.UTC),
	})
	n, err := countRows(t, db, q, args)
	require.NoError(t, err)
	require.Equal(t, 1, n, "an event filed under the previous day's partition must still be found")
}

// A window spanning a year boundary must exclude neither side. Comparing
// month independently of year would exclude both: December to January is
// month >= 12 AND month <= 01, which matches nothing.
func TestPartitionBoundsSpanYearBoundary(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	root := t.TempDir()
	seedEventParquet(t, db, partitionPath(root, "2025", "12", "20", "x.parquet"), "2025-12-20 10:00:00")
	seedEventParquet(t, db, partitionPath(root, "2026", "01", "15", "x.parquet"), "2026-01-15 10:00:00")

	q, args := buildQuery(partitionGlob(root), store.Filter{
		Since: time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
		Until: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
	})
	n, err := countRows(t, db, q, args)
	require.NoError(t, err)
	require.Equal(t, 2, n, "both sides of the year boundary are inside the window")
}

// The widening crosses month and year boundaries by date arithmetic, not
// by decrementing a day number. A window starting on the 1st of January
// must reach back into December of the previous year.
func TestPartitionBoundsWideningCrossesMonthAndYear(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	root := t.TempDir()
	seedEventParquet(t, db, partitionPath(root, "2025", "12", "31", "x.parquet"), "2026-01-01 00:05:00")
	seedEventParquet(t, db, partitionPath(root, "2026", "02", "01", "x.parquet"), "2026-01-31 23:55:00")

	q, args := buildQuery(partitionGlob(root), store.Filter{
		Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Until: time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC),
	})
	n, err := countRows(t, db, q, args)
	require.NoError(t, err)
	require.Equal(t, 2, n,
		"the day before January 1 is December 31 of the prior year, and the day after January 31 is February 1")
}

// A filter with no window at all must not emit partition predicates:
// there is nothing to bound, and a bound built from a zero time would
// exclude the entire archive.
func TestPartitionBoundsAbsentWithoutAWindow(t *testing.T) {
	q, _ := buildQuery("gcs://b/year=*/month=*/day=*/account=acct-1/*.parquet", store.Filter{})
	require.NotContains(t, q, " AND (year", "an unbounded query must read every partition")
	require.Contains(t, q, "year=*", "the glob still spans them; it is the predicate that must be absent")
}
