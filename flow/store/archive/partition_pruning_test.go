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
		AccountID: testAccount,
		Since:     time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Until:     time.Date(2026, 7, 31, 23, 59, 59, 0, time.UTC),
	}

	q, args := buildQuery(partitionGlob(root), f, 1)
	n, err := countRows(t, db, q, args)
	require.NoError(t, err,
		"September holds a corrupt file; reaching it means the window was not pruned")
	require.Equal(t, 2, n, "both June and July rows are inside the window")

	// The negative control: the same data, the same window, with the
	// partition predicates stripped. If this also passed, the assertion
	// above would be proving nothing about pruning.
	stripped, args := buildQuery(partitionGlob(root), f, 1)
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
		AccountID: testAccount,
		Since:     time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
		Until:     time.Date(2026, 6, 10, 23, 59, 59, 0, time.UTC),
	}, 1)
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
		AccountID: testAccount,
		Since:     time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
		Until:     time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
	}, 1)
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
		AccountID: testAccount,
		Since:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Until:     time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC),
	}, 1)
	n, err := countRows(t, db, q, args)
	require.NoError(t, err)
	require.Equal(t, 2, n,
		"the day before January 1 is December 31 of the prior year, and the day after January 31 is February 1")
}

// A filter with no window at all must not emit partition predicates:
// there is nothing to bound, and a bound built from a zero time would
// exclude the entire archive.
func TestPartitionBoundsAbsentWithoutAWindow(t *testing.T) {
	q, _ := buildQuery("gcs://b/year=*/month=*/day=*/account=acct-1/*.parquet", store.Filter{AccountID: "acct-1"}, 1)
	require.NotContains(t, q, " AND (year", "an unbounded query must read every partition")
	require.Contains(t, q, "year=*", "the glob still spans them; it is the predicate that must be absent")
}

// The one-day margin is a floor, not a constant. Objects written before
// the sinks split a batch by date took their whole path from the first
// event dequeued, so an object could carry events as far past its own
// name as the sink's flush interval — which is operator-configurable and
// has no upper bound. A 48h flush can put an event two days after the
// date in the path.
//
// A pruning predicate that stopped at one day would skip that object and
// report nothing, which is worse than the slow scan it replaced.
func TestMarginDaysForCoversTheConfiguredFlushInterval(t *testing.T) {
	for _, tc := range []struct {
		name string
		span time.Duration
		want int
	}{
		{"unset falls back to the floor", 0, 1},
		{"the 15m default needs no extra day", 15 * time.Minute, 2},
		{"just under a day still rounds up to one", 23 * time.Hour, 2},
		{"exactly a day", 24 * time.Hour, 2},
		{"48h spans two days", 48 * time.Hour, 3},
		{"a week", 7 * 24 * time.Hour, 8},
		{"a negative value cannot shrink the floor", -time.Hour, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, marginDaysFor(tc.span))
		})
	}
}

// The margin has to actually reach the file, not merely be computed.
// This plants an object whose name predates its contents by two days —
// what a 48h flush produced before the sinks were fixed — and requires
// the configured reader to find it.
func TestPruningFindsAnObjectOlderThanItsContents(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	root := t.TempDir()
	seedEventParquet(t, db, partitionPath(root, "2026", "06", "08", "x.parquet"), "2026-06-10 12:00:00")

	f := store.Filter{
		AccountID: testAccount,
		Since:     time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
		Until:     time.Date(2026, 6, 10, 23, 59, 59, 0, time.UTC),
	}

	q, args := buildQuery(partitionGlob(root), f, marginDaysFor(48*time.Hour))
	n, err := countRows(t, db, q, args)
	require.NoError(t, err)
	require.Equal(t, 1, n,
		"a reader configured for a 48h flush must reach the partition that flush could have written")

	// The control: the default margin does not reach it, which is why
	// MaxBatchSpan has to be configured rather than assumed.
	q, args = buildQuery(partitionGlob(root), f, marginDaysFor(0))
	n, err = countRows(t, db, q, args)
	require.NoError(t, err)
	require.Equal(t, 0, n)
}

// The margin has to travel from configuration to query. A store that
// computes the right number and then reads with the default one is
// indistinguishable, from the dashboard, from one that computes it
// wrong.
func TestStoreReadsWithTheConfiguredMargin(t *testing.T) {
	for _, tc := range []struct {
		span time.Duration
		want int
	}{
		{0, 1},
		{15 * time.Minute, 2},
		{48 * time.Hour, 3},
	} {
		d := &duckdbStore{cfg: Config{Provider: "s3", Bucket: "b", MaxBatchSpan: tc.span}}
		require.Equal(t, tc.want, d.marginDays(), "MaxBatchSpan=%s", tc.span)
	}
}

// And it has to reach the config in the first place. The flush interval
// is the writer's setting; the reader has to pick up the same value or
// the margin is sized for a sink nobody configured.
func TestConfigFromEnvCarriesTheFlushInterval(t *testing.T) {
	t.Run("gcs", func(t *testing.T) {
		t.Setenv(envGCSBucket, "b")
		t.Setenv(envGCSFlushInterval, "48h")
		cfg, ok := configFromEnv()
		require.True(t, ok)
		require.Equal(t, 48*time.Hour, cfg.MaxBatchSpan)
	})
	t.Run("s3", func(t *testing.T) {
		t.Setenv(envS3Bucket, "b")
		t.Setenv(envS3FlushInterval, "36h")
		cfg, ok := configFromEnv()
		require.True(t, ok)
		require.Equal(t, 36*time.Hour, cfg.MaxBatchSpan)
	})
	t.Run("unset leaves the floor to the reader", func(t *testing.T) {
		t.Setenv(envS3Bucket, "b")
		cfg, ok := configFromEnv()
		require.True(t, ok)
		require.Zero(t, cfg.MaxBatchSpan)
	})
}

// The archive path asserts an account; until #186 the writer did not
// keep that promise, and those objects are on disk now. An object under
// account=acct-A holding an acct-B row must return nothing for acct-A —
// restricting on the path alone hands one account another's events.
//
// This is not a pruning predicate: account_id lives inside the file, so
// the object is still opened. Opening it and returning nothing is the
// point.
func TestQueryNeverReturnsAnotherAccountsRows(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	root := t.TempDir()
	// Filed under acct-A, carrying acct-B's event: exactly what a mixed
	// batch produced.
	seedEventParquetAs(t, db, partitionPath(root, "2026", "06", "10", "x.parquet"),
		"2026-06-10 12:00:00", "acct-B")

	q, args := buildQuery(partitionGlob(root), store.Filter{
		AccountID: "acct-A",
		Since:     time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
		Until:     time.Date(2026, 6, 10, 23, 59, 59, 0, time.UTC),
	}, 1)
	n, err := countRows(t, db, q, args)
	require.NoError(t, err, "the object is still read; it is the row that must be rejected")
	require.Zero(t, n, "an object misfiled under acct-A leaked acct-B's event")

	// The control: the same object answers acct-B, so the predicate is
	// rejecting on identity rather than rejecting everything.
	q, args = buildQuery(partitionGlob(root), store.Filter{
		AccountID: "acct-B",
		Since:     time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
		Until:     time.Date(2026, 6, 10, 23, 59, 59, 0, time.UTC),
	}, 1)
	n, err = countRows(t, db, q, args)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
