//go:build archive_duckdb

package archive

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/flow/store"
)

// buildQuery interpolates the parquet glob rather than binding it, and that
// is not a style choice. DuckDB resolves the file list while binding the
// statement, so a parameter in that position leaves the binder nothing to
// expand.
//
// The failure is conditional, which is why it reached production: a query
// whose only parameter is the path prepares fine. Add any other parameter —
// a time window, which every dashboard request carries — and the whole
// prepare fails with "could not bind parameter". No test had ever run a real
// query with both, so the archive read path shipped broken twice: first the
// non-existent gcs extension, then this.
func TestBuildQueryPreparesWithFilters(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// A partitioned tree, because that is the only shape the reader ever
	// builds: parquetURL always globs year/month/day above the account.
	root := t.TempDir()
	seedEventParquet(t, db, partitionPath(root, "2026", "05", "12", "events.parquet"), "2026-05-12 10:00:00")

	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	port := uint32(443)
	proto := uint16(6)

	// The shape a dashboard request produces: a window plus predicates.
	q, args := buildQuery(partitionGlob(root), store.Filter{
		PeerID:   "peer-a",
		DestPort: &port,
		Protocol: &proto,
		Since:    since,
		Until:    until,
	})

	rows, err := db.QueryContext(ctx, q, args...)
	require.NoError(t, err, "the query must prepare; binding the glob fails here as soon as a second parameter exists")
	t.Cleanup(func() { _ = rows.Close() })
	require.True(t, rows.Next(), "the seeded row is inside the window and matches every predicate")
	require.NoError(t, rows.Err())
}

// A path holding a single quote must not break the statement, since the glob
// is now interpolated. Bucket and prefix come from operator configuration
// rather than request input, so this is defence in depth, not a live hole.
func TestBuildQueryQuotesTheGlob(t *testing.T) {
	q, args := buildQuery("gcs://bucket/it's/**/*.parquet", store.Filter{})
	require.Contains(t, q, "it''s", "a single quote has to be doubled for DuckDB")
	require.NotContains(t, args, "gcs://bucket/it's/**/*.parquet", "the glob must not be an argument")
}
