//go:build archive_duckdb

package compact

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/marcboeker/go-duckdb"
	"github.com/stretchr/testify/require"
)

const day29 = "2026-07-29"

func newFixture(t *testing.T) (*Compactor, *fsStore, *sql.DB) {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	root := t.TempDir()
	fs := &fsStore{root: root}
	return &Compactor{
		DB:        db,
		Store:     fs,
		ReadRoot:  root + "/flows",
		KeyPrefix: "flows",
		Now:       func() time.Time { return time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC) },
	}, fs, db
}

// seed writes one tiny object, the shape the sink produces: the account
// in the path and the account in the row are separate facts, so they can
// be made to disagree.
func seed(t *testing.T, db *sql.DB, root, pathAccount, rowAccount, ts string, n int) {
	t.Helper()
	dir := filepath.Join(root, "flows", "year=2026", "month=07", "day=29", "account="+pathAccount)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, fmt.Sprintf("%d-%s.parquet", n, rowAccount))
	_, err := db.ExecContext(context.Background(), fmt.Sprintf(
		`COPY (SELECT TIMESTAMP '%s' AS received_at, '%s' AS account_id,
		        'peer-%d' AS peer_id, 'ev-%d' AS event_id)
		 TO '%s' (FORMAT PARQUET)`, ts, rowAccount, n, n, file))
	require.NoError(t, err)
}

// The headline: many tiny objects become one per account, and no row is
// lost doing it. 40 objects of a few hundred bytes is the production
// shape in miniature -- a real day held ~150.
func TestCompactDayMergesIntoOneObjectPerAccount(t *testing.T) {
	c, fs, db := newFixture(t)
	for i := range 40 {
		acct := "acct-A"
		if i%4 == 0 {
			acct = "acct-B"
		}
		seed(t, db, fs.root, acct, acct, day29+fmt.Sprintf(" %02d:00:00", i%24), i)
	}
	require.Equal(t, 40, fs.count("flows"))

	res, err := c.CompactDay(context.Background(), time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.False(t, res.Skipped)
	require.Equal(t, 40, res.ObjectsBefore)
	require.Equal(t, 2, res.ObjectsAfter, "one object per account, not per flush")
	require.Equal(t, int64(40), res.Rows, "every row survives the rewrite")
	require.Equal(t, 2, fs.count("flows"), "the originals are gone, the replacements are not")
}

// The layout has to match what the sink writes and what the reader
// prunes on. DuckDB's PARTITION_BY writes month=7; the sinks write
// month=07; the reader compares them as strings, where '7' sorts after
// '07'. An unpadded directory would be silently excluded from every
// query whose window it should match -- compaction would have broken
// reads while appearing to succeed.
func TestCompactDayWritesZeroPaddedPartitions(t *testing.T) {
	c, fs, db := newFixture(t)
	for i := range 4 {
		seed(t, db, fs.root, "acct-A", "acct-A", day29+" 10:00:00", i)
	}
	_, err := c.CompactDay(context.Background(), time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	keys, err := fs.List(context.Background(), "flows")
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Contains(t, keys[0], "year=2026/month=07/day=29/account=acct-A/",
		"got %q -- an unpadded month or day is invisible to the reader's partition filter", keys[0])
}

// Rows written under the wrong account before the sinks split batches
// (openzro#186) must come back under the account that owns them. This is
// the half that closing the leak could not do: the reader globs by
// account, so a misfiled object is never opened by its rightful owner.
func TestCompactDayRepartitionsMisfiledRows(t *testing.T) {
	c, fs, db := newFixture(t)
	// Ten objects filed under acct-A; three of them hold acct-B's rows.
	for i := range 10 {
		rowAcct := "acct-A"
		if i < 3 {
			rowAcct = "acct-B"
		}
		seed(t, db, fs.root, "acct-A", rowAcct, day29+" 10:00:00", i)
	}

	res, err := c.CompactDay(context.Background(), time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"acct-A", "acct-B"}, res.Accounts)

	keys, err := fs.List(context.Background(), "flows")
	require.NoError(t, err)
	require.Len(t, keys, 2)

	var rows int64
	require.NoError(t, db.QueryRow(
		"SELECT count(*) FROM read_parquet('"+fs.root+
			"/flows/year=*/month=*/day=*/account=acct-B/*.parquet', hive_partitioning=true) "+
			"WHERE account_id = 'acct-B'").Scan(&rows))
	require.Equal(t, int64(3), rows, "acct-B's rows must now live under acct-B's path")

	// And the path must no longer contradict the rows inside it, which
	// is the invariant the whole layout rests on.
	var mismatched int64
	require.NoError(t, db.QueryRow(
		"SELECT count(*) FROM read_parquet('"+fs.root+
			"/flows/year=*/month=*/day=*/account=*/*.parquet', hive_partitioning=true) "+
			"WHERE account_id <> account").Scan(&mismatched))
	require.Zero(t, mismatched)
}

func TestCompactDayRepartitionsSingleMisfiledObject(t *testing.T) {
	c, fs, db := newFixture(t)
	seed(t, db, fs.root, "acct-A", "acct-B", day29+" 10:00:00", 1)

	res, err := c.CompactDay(context.Background(), time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.False(t, res.Skipped, "one source object can still be misfiled and need repair")
	require.ElementsMatch(t, []string{"acct-B"}, res.Accounts)

	keys, err := fs.List(context.Background(), "flows")
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Contains(t, keys[0], "account=acct-B/")

	var mismatched int64
	require.NoError(t, db.QueryRow(
		"SELECT count(*) FROM read_parquet('"+fs.root+
			"/flows/year=*/month=*/day=*/account=*/*.parquet', hive_partitioning=true) "+
			"WHERE account_id <> account").Scan(&mismatched))
	require.Zero(t, mismatched)
}

// The safety property, stated as a property rather than hoped for: a Put
// that fails must leave every original in place. Compaction is the one
// operation here that deletes data, so the failure mode that matters is
// deleting before the replacement exists.
func TestCompactDayKeepsOriginalsWhenWriteFails(t *testing.T) {
	c, fs, db := newFixture(t)
	for i := range 8 {
		seed(t, db, fs.root, "acct-A", "acct-A", day29+" 10:00:00", i)
	}
	fs.putErr = errors.New("bucket refused the write")

	_, err := c.CompactDay(context.Background(), time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
	require.Error(t, err)
	require.Empty(t, fs.deleted, "nothing may be deleted before the replacement is written")
	require.Equal(t, 8, fs.count("flows"), "every original must still be there")
}

// A day that is already one object per account must cost nothing to
// revisit. Without this a nightly job would rewrite and re-delete the
// same data forever, and a rerun after a partial failure would churn
// rather than converge.
func TestCompactDayIsIdempotent(t *testing.T) {
	c, fs, db := newFixture(t)
	for i := range 6 {
		acct := "acct-A"
		if i%2 == 0 {
			acct = "acct-B"
		}
		seed(t, db, fs.root, acct, acct, day29+" 10:00:00", i)
	}
	d := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)

	first, err := c.CompactDay(context.Background(), d)
	require.NoError(t, err)
	require.Equal(t, 2, first.ObjectsAfter)

	second, err := c.CompactDay(context.Background(), d)
	require.NoError(t, err)
	require.True(t, second.Skipped, "a compacted day must be recognised as compacted")
	require.Empty(t, second.Accounts)
	require.Equal(t, 2, fs.count("flows"))
	require.Len(t, fs.put, 2, "the second run must not write anything")
}

// A day with nothing in it is not an error. The nightly job will meet
// plenty of them.
func TestCompactDayEmptyIsNotAnError(t *testing.T) {
	c, _, _ := newFixture(t)
	res, err := c.CompactDay(context.Background(), time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.True(t, res.Skipped)
	require.Zero(t, res.ObjectsBefore)
}

// Compaction must not touch the neighbours. A job running for the 29th
// while the sink writes the 30th has to leave the 30th alone.
func TestCompactDayLeavesOtherDaysAlone(t *testing.T) {
	c, fs, db := newFixture(t)
	for i := range 5 {
		seed(t, db, fs.root, "acct-A", "acct-A", day29+" 10:00:00", i)
	}
	other := filepath.Join(fs.root, "flows", "year=2026", "month=07", "day=30", "account=acct-A")
	require.NoError(t, os.MkdirAll(other, 0o755))
	_, err := db.Exec("COPY (SELECT TIMESTAMP '2026-07-30 10:00:00' AS received_at, " +
		"'acct-A' AS account_id, 'p' AS peer_id, 'e' AS event_id) TO '" +
		filepath.Join(other, "x.parquet") + "' (FORMAT PARQUET)")
	require.NoError(t, err)

	_, err = c.CompactDay(context.Background(), time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	require.Equal(t, 1, fs.count("flows/year=2026/month=07/day=30"),
		"the next day belongs to the sink, not to this job")
	for _, d := range fs.deleted {
		require.NotContains(t, d, "day=30")
	}
}

// The row-count check is the only thing between a bad rewrite and
// deleted history, so it gets a test that makes it fire. A guard nobody
// has seen trip is a guard nobody has tested.
//
// The substituted rewrite drops rows the way a subtly wrong COPY would:
// it produces a valid, well-formed, correctly-partitioned output that is
// simply short. That is the shape that would otherwise be discovered
// months later as missing history, with the originals long gone.
func TestCompactDayRefusesToDeleteWhenRowsAreLost(t *testing.T) {
	c, fs, db := newFixture(t)
	for i := range 10 {
		seed(t, db, fs.root, "acct-A", "acct-A", day29+" 10:00:00", i)
	}

	c.rewriteFn = func(ctx context.Context, glob, outDir string) error {
		dir := filepath.Join(outDir, "year=2026", "month=07", "day=29", "account=acct-A")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		// Nine rows where ten were read.
		_, err := db.ExecContext(ctx,
			"COPY (SELECT * FROM read_parquet("+quote(glob)+", hive_partitioning=true) LIMIT 9) TO '"+
				filepath.Join(dir, "short.parquet")+"' (FORMAT PARQUET)")
		return err
	}

	_, err := c.CompactDay(context.Background(), time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
	require.Error(t, err)
	require.Contains(t, err.Error(), "rows=10")
	require.Contains(t, err.Error(), "rows=9")
	require.Contains(t, err.Error(), "originals left untouched")
	require.Empty(t, fs.deleted, "a short rewrite must not cost the originals")
	require.Equal(t, 10, fs.count("flows"))
}

// A day's objects can hold rows belonging to another day: before the
// sinks split a batch by date, a flush crossing midnight filed its later
// events under the earlier day. Compaction has to put those rows where
// their timestamps say they go, not where they were found.
//
// Writing them back under the requested day would move the misfiling
// instead of undoing it, and the result would look correct at every
// level except the one that matters -- the reader prunes by these
// directories, so the rows would stay unfindable by date.
func TestCompactDayFilesRowsByTheirOwnDate(t *testing.T) {
	c, fs, db := newFixture(t)
	// Nine rows genuinely on the 29th, one that belongs to the 28th but
	// was filed under the 29th by a batch that began before midnight.
	for i := range 9 {
		seed(t, db, fs.root, "acct-A", "acct-A", day29+" 10:00:00", i)
	}
	seed(t, db, fs.root, "acct-A", "acct-A", "2026-07-28 23:58:00", 99)

	_, err := c.CompactDay(context.Background(), time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	keys, err := fs.List(context.Background(), "flows")
	require.NoError(t, err)
	require.Len(t, keys, 2, "two dates in, two partitions out")

	var got []string
	for _, k := range keys {
		got = append(got, k[:len("flows/year=2026/month=07/day=29")])
	}
	require.ElementsMatch(t,
		[]string{"flows/year=2026/month=07/day=28", "flows/year=2026/month=07/day=29"},
		got, "the row from the 28th must land under the 28th")

	var stray int64
	require.NoError(t, db.QueryRow(
		"SELECT count(*) FROM read_parquet('"+fs.root+
			"/flows/year=*/month=*/day=*/account=*/*.parquet', hive_partitioning=true) "+
			"WHERE day(received_at) <> CAST(day AS INTEGER)").Scan(&stray))
	require.Zero(t, stray, "no row may sit under a day other than its own")
}

func TestCompactDayFilesSingleObjectRowsByTheirOwnDate(t *testing.T) {
	c, fs, db := newFixture(t)
	seed(t, db, fs.root, "acct-A", "acct-A", "2026-07-28 23:58:00", 99)

	res, err := c.CompactDay(context.Background(), time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.False(t, res.Skipped, "one source object can still belong under a different day")

	keys, err := fs.List(context.Background(), "flows")
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Contains(t, keys[0], "year=2026/month=07/day=28/account=acct-A/")

	var stray int64
	require.NoError(t, db.QueryRow(
		"SELECT count(*) FROM read_parquet('"+fs.root+
			"/flows/year=*/month=*/day=*/account=*/*.parquet', hive_partitioning=true) "+
			"WHERE day(received_at) <> CAST(day AS INTEGER)").Scan(&stray))
	require.Zero(t, stray)
}

func TestCompactDayLeavesParquetOutsideAccountPartitionAlone(t *testing.T) {
	c, fs, db := newFixture(t)
	seed(t, db, fs.root, "acct-A", "acct-A", day29+" 10:00:00", 1)

	strayDir := filepath.Join(fs.root, "flows", "year=2026", "month=07", "day=29")
	stray := filepath.Join(strayDir, "stray.parquet")
	_, err := db.Exec("COPY (SELECT TIMESTAMP '2026-07-29 10:00:00' AS received_at, " +
		"'acct-A' AS account_id, 'p' AS peer_id, 'e' AS event_id) TO '" +
		stray + "' (FORMAT PARQUET)")
	require.NoError(t, err)

	res, err := c.CompactDay(context.Background(), time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.True(t, res.Skipped, "the compactable object is already valid")

	_, err = os.Stat(stray)
	require.NoError(t, err, "objects outside account partitions are not read and must not be deleted")
	for _, d := range fs.deleted {
		require.NotContains(t, d, "stray.parquet")
	}
}

// A row count catches loss and nothing else. This is the case it cannot
// see: the rewrite emits exactly as many rows as it read, with one of
// them changed. For an archive read back as audit evidence, silently
// altered history is worse than missing history -- it is wrong and it
// looks right.
//
// The substituted rewrite is not a strawman. A wrong join, a cast that
// truncates an ID, a column mapped to its neighbour: all of them
// preserve the count.
func TestCompactDayRefusesWhenContentChangesButCountDoesNot(t *testing.T) {
	c, fs, db := newFixture(t)
	for i := range 10 {
		seed(t, db, fs.root, "acct-A", "acct-A", day29+" 10:00:00", i)
	}

	c.rewriteFn = func(ctx context.Context, glob, outDir string) error {
		dir := filepath.Join(outDir, "year=2026", "month=07", "day=29", "account=acct-A")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		// Ten rows in, ten rows out, one peer_id rewritten.
		_, err := db.ExecContext(ctx,
			"COPY (SELECT * REPLACE (CASE WHEN event_id = 'ev-3' THEN 'tampered' ELSE peer_id END AS peer_id) "+
				"FROM read_parquet("+quote(glob)+", hive_partitioning=true)) TO '"+
				filepath.Join(dir, "same-count.parquet")+"' (FORMAT PARQUET)")
		return err
	}

	_, err := c.CompactDay(context.Background(), time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
	require.Error(t, err, "a count-preserving corruption must still be refused")
	require.Contains(t, err.Error(), "rows=10", "both sides read ten rows; the count agreed")
	require.Contains(t, err.Error(), "originals left untouched")
	require.Empty(t, fs.deleted)
	require.Equal(t, 10, fs.count("flows"))
}

// The fingerprint must not fire on the reordering compaction always
// does. Merging 150 files into one changes row order by construction, so
// an order-sensitive check would refuse every correct run and the guard
// would be turned off within a week.
func TestFingerprintIgnoresRowOrder(t *testing.T) {
	c, fs, db := newFixture(t)
	for i := range 12 {
		acct := "acct-A"
		if i%3 == 0 {
			acct = "acct-B"
		}
		seed(t, db, fs.root, acct, acct, day29+fmt.Sprintf(" %02d:00:00", i), i)
	}
	res, err := c.CompactDay(context.Background(), time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err, "compaction reorders rows; that must not read as corruption")
	require.Equal(t, int64(12), res.Rows)
	require.NotEmpty(t, res.Fingerprint.XOR)
	require.NotEmpty(t, res.Fingerprint.Sum)
}

// Everything before this proves the rewrite was right. This proves the
// write was. A Write that reports success while storing something else
// -- truncated, empty, a different object -- looks identical to success
// at every earlier step, and the next step deletes the only other copy.
//
// The store here accepts the bytes and keeps a truncated prefix of them,
// which is what a partial upload leaves behind.
func TestCompactDayRefusesWhenTheStoredObjectDiffers(t *testing.T) {
	c, fs, db := newFixture(t)
	for i := range 10 {
		seed(t, db, fs.root, "acct-A", "acct-A", day29+" 10:00:00", i)
	}
	fs.truncateWrites = true

	_, err := c.CompactDay(context.Background(), time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
	require.Error(t, err, "what landed in the store has to be checked, not what was sent to it")
	require.Contains(t, err.Error(), "originals left untouched")
	require.Equal(t, 10, fs.count("flows"),
		"the originals must survive, and the bad replacement must not be left behind")
	for _, k := range fs.deleted {
		require.Contains(t, k, "compact-",
			"only this run's own replacements may be removed; no original may be")
	}
}
