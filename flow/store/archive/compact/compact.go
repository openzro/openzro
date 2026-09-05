package compact

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Fingerprint identifies a set of rows independently of how they are
// grouped into files or ordered within them, which is what makes it
// comparable across a rewrite that changes both.
//
// Three numbers, because none of them is sufficient alone:
//
//   - Rows catches loss, and nothing else. A rewrite that replaced one
//     row with a different one keeps the count exactly.
//   - XOR catches substitution, and is order-independent as required --
//     but identical rows cancel each other out, so losing a matched pair
//     leaves it unchanged. Measured, not assumed: two copies of the same
//     row hash to 0.
//   - Sum catches what XOR cancels, since duplicates accumulate rather
//     than annihilate.
//
// The partition columns are excluded on both sides. They are derived
// from the path on the way in and rebuilt from received_at on the way
// out, and repartitioning changes them on purpose -- including them
// would report every corrected row as a corrupted one.
type Fingerprint struct {
	Rows int64
	// XOR and Sum are text because neither fits a Go integer: bit_xor
	// returns UBIGINT and sum returns HUGEINT. They are compared, never
	// arithmetic, so the width does not need to travel.
	XOR string
	Sum string
}

// Equal reports whether two fingerprints describe the same rows.
func (f Fingerprint) Equal(o Fingerprint) bool {
	return f.Rows == o.Rows && f.XOR == o.XOR && f.Sum == o.Sum
}

func (f Fingerprint) String() string {
	return fmt.Sprintf("rows=%d xor=%s sum=%s", f.Rows, f.XOR, f.Sum)
}

// Result reports what one day's compaction did. Every field is measured
// rather than assumed, because the operation ends in a delete and an
// operator asked to trust it deserves the arithmetic.
type Result struct {
	Day           time.Time
	ObjectsBefore int
	ObjectsAfter  int
	Rows          int64
	BytesWritten  int64
	Accounts      []string
	// Orphans names replacements this run wrote and then could not
	// remove after refusing to finish. They are the operator's to delete;
	// nothing else will, and the reader will trip over them.
	Orphans        []string
	Fingerprint    Fingerprint
	Skipped        bool   // already compact; nothing was written or deleted
	SkippedBecause string //nolint:revive // reported to the operator, not branched on
}

// Compactor rewrites one archive day at a time.
//
// It is deliberately not a service. Compaction reads a day, writes it
// back, and deletes the originals; doing that on a schedule from outside
// the request path keeps it away from the memory and CPU the API needs,
// and makes a bad run something an operator can stop.
type Compactor struct {
	// DB is a DuckDB handle with the archive's credentials already
	// applied, so read_parquet can reach the same objects the reader
	// reads.
	DB *sql.DB
	// Store writes and deletes. See ObjectStore for why this is not
	// DuckDB.
	Store ObjectStore
	// ReadRoot is the URL prefix DuckDB globs under, e.g.
	// "gs://bucket/flows" or a local directory in tests.
	ReadRoot string
	// KeyPrefix is the same location expressed as an object key prefix,
	// e.g. "flows". Empty for a bucket root.
	KeyPrefix string
	// Now supplies the clock for output names. Tests pin it.
	Now func() time.Time

	// rewriteFn is a seam. The row-count check below is the only thing
	// standing between a bad rewrite and deleted history, and a
	// guard that cannot be made to fire is a guard nobody has tested.
	// Production leaves this nil and gets the real rewrite.
	rewriteFn func(ctx context.Context, glob, outDir string) error
}

// CompactDay rewrites every object for one UTC day into one object per
// account, then deletes what it replaced.
//
// The order is the safety property, and it only reads one way: nothing
// is deleted until the replacement is written AND its row count matches
// what was read. A compaction that loses rows leaves the originals in
// place and says so.
func (c *Compactor) CompactDay(ctx context.Context, day time.Time) (res Result, err error) {
	day = day.UTC()
	res = Result{Day: day}

	rel := dayPath(day)
	dayPrefix := c.keyPrefix(rel)
	sources, err := c.Store.List(ctx, dayPrefix)
	if err != nil {
		return res, fmt.Errorf("list %s: %w", dayPrefix, err)
	}
	sources = onlyParquet(sources)
	res.ObjectsBefore = len(sources)

	// One object per account is the goal, so a day already at or below
	// the number of accounts in it has nothing to gain and would only
	// churn the bucket. Checking the count first also means a rerun over
	// an already-compacted day is free rather than merely harmless.
	if len(sources) <= 1 {
		res.Skipped = true
		res.SkippedBecause = "nothing to merge"
		res.ObjectsAfter = len(sources)
		return res, nil
	}

	tmp, err := os.MkdirTemp("", "openzro-compact-")
	if err != nil {
		return res, fmt.Errorf("temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	glob := c.url(rel + "/account=*/*.parquet")
	before, err := c.fingerprint(ctx, glob)
	if err != nil {
		return res, fmt.Errorf("fingerprint sources: %w", err)
	}
	rowsBefore := before.Rows
	if rowsBefore == 0 {
		res.Skipped = true
		res.SkippedBecause = "no rows"
		res.ObjectsAfter = len(sources)
		return res, nil
	}

	out := filepath.Join(tmp, "out")
	rewrite := c.rewrite
	if c.rewriteFn != nil {
		rewrite = c.rewriteFn
	}
	if err := rewrite(ctx, glob, out); err != nil {
		return res, fmt.Errorf("rewrite: %w", err)
	}

	outputs, err := collectOutputs(out)
	if err != nil {
		return res, fmt.Errorf("collect outputs: %w", err)
	}
	if len(outputs) == 0 {
		return res, fmt.Errorf("rewrite produced no files for %d rows", rowsBefore)
	}

	// Fingerprint what is about to replace the originals, from the files
	// themselves rather than from the statement that produced them. A
	// COPY that dropped or altered a row would otherwise be discovered
	// by the operator months later, as missing or wrong history, with
	// the originals long gone.
	after, err := c.fingerprint(ctx, filepath.Join(out, "**", "*.parquet"))
	if err != nil {
		return res, fmt.Errorf("fingerprint rewrite: %w", err)
	}
	if !before.Equal(after) {
		return res, fmt.Errorf(
			"refusing to delete: read [%s], rewrote [%s]; originals left untouched",
			before, after)
	}
	res.Rows = after.Rows
	res.Fingerprint = after

	stamp := c.now().UTC().UnixNano()
	// Every key written this run, so a failure before the delete can
	// take them back out. A half-written replacement left in the bucket
	// is not inert: the reader globs the partition and opens whatever it
	// finds, so a truncated parquet fails every query that touches the
	// day -- turning a compaction that safely refused into an outage.
	var writtenKeys []string
	defer func() {
		if err == nil {
			return
		}
		for _, k := range writtenKeys {
			if delErr := c.Store.Delete(ctx, k); delErr != nil {
				// Best effort: the originals are intact either way, and
				// the operator needs the name to finish the cleanup.
				res.Orphans = append(res.Orphans, k)
			}
		}
	}()

	for _, w := range outputs {
		var body []byte
		body, err = os.ReadFile(w.path)
		if err != nil {
			return res, fmt.Errorf("read %s: %w", w.path, err)
		}
		// Keyed by the partition DuckDB derived from received_at. This
		// is where the zero padding in rewrite becomes load-bearing:
		// these strings go straight into the object key the reader
		// prunes on, and "month=7" is a directory no query will match.
		key := c.keyPrefix(w.partition() + fmt.Sprintf("/compact-%d.parquet", stamp))
		if err = c.Store.Write(ctx, key, body); err != nil {
			// Nothing has been deleted yet, so a failure here costs a
			// stray object and no data. The next run overwrites it.
			return res, fmt.Errorf("write %s: %w", key, err)
		}
		writtenKeys = append(writtenKeys, key)
		res.BytesWritten += int64(len(body))
		res.Accounts = append(res.Accounts, w.account)
		res.ObjectsAfter++
	}

	// Read the replacements back out of the store and fingerprint what
	// actually landed there. Everything up to here proves the rewrite
	// was right; this proves the write was. A Write that reported
	// success while truncating, or a store that accepted bytes and
	// returned different ones, is indistinguishable from success at
	// every earlier step -- and the next step deletes the only other
	// copy.
	var written Fingerprint
	written, err = c.fingerprint(ctx, c.url(fmt.Sprintf("year=*/month=*/day=*/account=*/compact-%d.parquet", stamp)))
	if err != nil {
		// Unreadable is as disqualifying as different, and says the same
		// thing to the operator: the originals are still the only good
		// copy, and they are still there.
		return res, fmt.Errorf(
			"refusing to delete: stored objects could not be read back; originals left untouched: %w", err)
	}
	if !before.Equal(written) {
		return res, fmt.Errorf(
			"refusing to delete: read [%s], stored [%s]; originals left untouched",
			before, written)
	}

	// Only the keys listed at the start. Anything the sink wrote while
	// this ran is not in that list and survives, which is why the list
	// is taken once and not refreshed -- and why a re-listed prefix at
	// the end would be a way to delete data nobody inspected.
	for _, key := range sources {
		if err = c.Store.Delete(ctx, key); err != nil {
			return res, fmt.Errorf("delete %s: %w", key, err)
		}
	}
	return res, nil
}

// rewrite is the whole merge. DuckDB reads the day, regroups by the
// account each row actually carries, and writes one file per partition.
//
// The partition columns are rebuilt from received_at rather than carried
// over, because carrying them over would preserve the misfiling this is
// meant to undo. They are formatted, not cast: DuckDB's PARTITION_BY
// writes an integer as "month=7", and the sinks write "month=07". The
// reader compares those as strings, where '7' sorts after '07', so an
// unpadded directory would be silently excluded from every query whose
// window it should match.
func (c *Compactor) rewrite(ctx context.Context, glob, outDir string) error {
	_, err := c.DB.ExecContext(ctx, `COPY (
		SELECT * EXCLUDE (year, month, day, account),
		       printf('%04d', year(received_at))  AS year,
		       printf('%02d', month(received_at)) AS month,
		       printf('%02d', day(received_at))   AS day,
		       account_id                         AS account
		FROM read_parquet(`+quote(glob)+`, hive_partitioning=true)
	) TO `+quote(outDir)+` (FORMAT PARQUET, PARTITION_BY (year, month, day, account), OVERWRITE_OR_IGNORE)`)
	return err
}

// fingerprint reads a glob and summarises its rows. See Fingerprint for
// why it is three numbers and why the partition columns are excluded.
func (c *Compactor) fingerprint(ctx context.Context, glob string) (Fingerprint, error) {
	var f Fingerprint
	// Both as text: bit_xor returns UBIGINT and sum returns HUGEINT,
	// and neither fits an int64 the driver would hand back.
	var xor sql.NullString
	var sum sql.NullString
	err := c.DB.QueryRowContext(ctx, `SELECT count(*),
		    CAST(bit_xor(hash(r)) AS VARCHAR),
		    CAST(sum(hash(r)::HUGEINT) AS VARCHAR)
		FROM (
		    SELECT * EXCLUDE (year, month, day, account)
		    FROM read_parquet(`+quote(glob)+`, hive_partitioning=true)
		) r`).Scan(&f.Rows, &xor, &sum)
	if err != nil {
		return Fingerprint{}, err
	}
	f.XOR = xor.String
	f.Sum = sum.String
	return f, nil
}

// dayPath is the day's location relative to the archive root, and the
// only place the layout is spelled out. Both the object key and the
// DuckDB URL are built by prefixing it, so neither is ever recovered by
// trimming the other apart.
func dayPath(day time.Time) string {
	return fmt.Sprintf("year=%04d/month=%02d/day=%02d", day.Year(), int(day.Month()), day.Day())
}

// keyPrefix turns a root-relative path into an object key.
func (c *Compactor) keyPrefix(rel string) string {
	if c.KeyPrefix == "" {
		return rel
	}
	return strings.TrimSuffix(c.KeyPrefix, "/") + "/" + rel
}

// url turns a root-relative path into something DuckDB can read.
func (c *Compactor) url(rel string) string {
	return strings.TrimSuffix(c.ReadRoot, "/") + "/" + rel
}

func (c *Compactor) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// output is one file DuckDB's PARTITION_BY produced, with its partition
// read back out of the directory it landed in.
//
// The partition is taken from the output and not from the day that was
// asked for, and the difference is not academic. A day's objects can
// hold rows belonging to another day: batches that crossed midnight were
// filed under the earlier day before the sinks split by date. Writing
// those rows back under the requested day would move the misfiling
// rather than undo it -- and undoing it is the point.
type output struct {
	path             string
	year, month, day string
	account          string
}

// partition returns the object-key fragment this output belongs under,
// which is DuckDB's own answer rather than the caller's assumption.
func (o output) partition() string {
	return fmt.Sprintf("year=%s/month=%s/day=%s/account=%s", o.year, o.month, o.day, o.account)
}

func collectOutputs(root string) ([]output, error) {
	var out []output
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".parquet") {
			return nil
		}
		o := output{path: p}
		for _, part := range strings.Split(filepath.ToSlash(p), "/") {
			k, v, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			switch k {
			case "year":
				o.year = v
			case "month":
				o.month = v
			case "day":
				o.day = v
			case "account":
				o.account = v
			}
		}
		if o.year == "" || o.month == "" || o.day == "" || o.account == "" {
			return fmt.Errorf("output %s is missing a partition component", p)
		}
		out = append(out, o)
		return nil
	})
	return out, err
}

func onlyParquet(keys []string) []string {
	out := keys[:0:0]
	for _, k := range keys {
		if strings.HasSuffix(k, ".parquet") {
			out = append(out, k)
		}
	}
	return out
}

// quote single-quotes a path for DuckDB, doubling any quote inside it.
// Paths come from operator configuration, never from request input, so
// this is defence in depth rather than a live hole -- the same posture
// the reader takes.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
