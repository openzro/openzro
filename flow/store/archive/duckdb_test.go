//go:build archive_duckdb

package archive

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/flow/store"
)

// TestNew_RequiresBucketAndProvider exercises the input guards on
// New(). Catches accidental drift if a future refactor moves these
// checks to a config layer that's not exercised by the federated
// wrapper.
func TestNew_RequiresBucketAndProvider(t *testing.T) {
	_, err := New(Config{Provider: "s3"})
	assert.Error(t, err, "missing Bucket should fail")

	_, err = New(Config{Bucket: "b"})
	assert.Error(t, err, "missing Provider should fail")

	_, err = New(Config{Provider: "ftp", Bucket: "b"})
	assert.Error(t, err, "unsupported Provider should fail")

	_, err = New(Config{Provider: "s3", Bucket: "b"})
	assert.NoError(t, err, "valid s3 config should succeed")

	_, err = New(Config{Provider: "gcs", Bucket: "b"})
	assert.NoError(t, err, "valid gcs config should succeed")
}

// TestBuildQuery_AccountIDOnly produces the simplest viable query —
// just `WHERE 1=1 ORDER BY ... LIMIT MaxRowsPerQuery`. Verifies the
// glob URL and the safety-net LIMIT both land in the SQL.
func TestBuildQuery_AccountIDOnly(t *testing.T) {
	q, args := buildQuery("s3://b/year=*/month=*/day=*/account=acct-1/*.parquet", store.Filter{})
	assert.Contains(t, q, "FROM read_parquet(?, hive_partitioning=true)")
	assert.Contains(t, q, "WHERE 1=1")
	assert.Contains(t, q, "ORDER BY received_at DESC")
	assert.Contains(t, q, "LIMIT ?")
	require.Len(t, args, 2)
	assert.Equal(t, "s3://b/year=*/month=*/day=*/account=acct-1/*.parquet", args[0])
	assert.Equal(t, MaxRowsPerQuery, args[1].(int))
}

// TestBuildQuery_AllFilters exercises every Filter field. Catches
// the easy regression of forgetting a clause when adding a column to
// the schema, and pins the parameter ordering.
func TestBuildQuery_AllFilters(t *testing.T) {
	srcPort := uint32(51322)
	dstPort := uint32(22)
	proto := uint16(6)
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC)
	f := store.Filter{
		AccountID:  "acct-1",
		PeerID:     "peer-alice",
		SourceIP:   "100.65.0.10",
		DestIP:     "100.65.0.40",
		SourcePort: &srcPort,
		DestPort:   &dstPort,
		Protocol:   &proto,
		Since:      since,
		Until:      until,
		RuleID:     []byte{0x01, 0x02, 0x03},
		Limit:      50,
		Offset:     100,
	}
	q, args := buildQuery("s3://b/account=acct-1/*.parquet", f)
	for _, want := range []string{
		"peer_id = ?",
		"source_ip = ?",
		"dest_ip = ?",
		"source_port = ?",
		"dest_port = ?",
		"protocol = ?",
		"received_at >= ?",
		"received_at <= ?",
		"rule_id = ?",
		"LIMIT ?",
		"OFFSET ?",
	} {
		assert.Contains(t, q, want)
	}
	// 12 args: url + 9 filters + limit + offset
	assert.Len(t, args, 12)
}

// TestBuildQuery_LimitClamping ensures a caller asking for "give me
// everything" or a million rows gets clamped to MaxRowsPerQuery —
// the safety net that prevents a malformed Filter from scanning a
// year of archive into memory.
func TestBuildQuery_LimitClamping(t *testing.T) {
	for _, in := range []int{0, -1, MaxRowsPerQuery + 1, 1_000_000} {
		_, args := buildQuery("u", store.Filter{Limit: in})
		assert.Equal(t, MaxRowsPerQuery, args[len(args)-1].(int),
			"input limit=%d should clamp to %d", in, MaxRowsPerQuery)
	}
}

// TestBuildQuery_RuleIDIsHexEncoded mirrors the wire format the
// Parquet schema persists (hex-encoded BYTE_ARRAY). Without this
// encoding the SELECT would compare raw bytes against a string
// column and silently match nothing.
func TestBuildQuery_RuleIDIsHexEncoded(t *testing.T) {
	_, args := buildQuery("u", store.Filter{RuleID: []byte{0xab, 0xcd}})
	// args = [url, rule_id, limit] — find rule_id at index 1
	assert.Equal(t, "abcd", args[1].(string))
}

// TestParquetURL_HivePartition checks the URL template lines up with
// what flow/sinks/{s3,gcs}.go writes (Hive-style year/month/day
// account partitioning). DuckDB's hive_partitioning option depends
// on this exact layout to prune directories before opening any
// objects.
func TestParquetURL_HivePartition(t *testing.T) {
	d := &duckdbStore{cfg: Config{
		Provider: "s3",
		Bucket:   "my-bucket",
		Prefix:   "openzro",
	}}
	url := d.parquetURL("acct-42")
	assert.True(t,
		strings.Contains(url, "s3://my-bucket/openzro/year=*/month=*/day=*/account=acct-42/*.parquet"),
		"got %q", url)
}

// TestParquetURL_PrefixSlashNormalisation accepts both `openzro` and
// `openzro/` for the same Prefix value. Catches a common operator
// fat-finger.
func TestParquetURL_PrefixSlashNormalisation(t *testing.T) {
	for _, prefix := range []string{"openzro", "openzro/"} {
		d := &duckdbStore{cfg: Config{
			Provider: "s3", Bucket: "b", Prefix: prefix,
		}}
		url := d.parquetURL("a")
		assert.False(t, strings.Contains(url, "//year="),
			"prefix %q produced double slash: %q", prefix, url)
	}
}

// TestQuery_RequiresAccountID guards the cross-account isolation
// boundary. A Filter with no AccountID must not produce a query that
// would scan every account's prefix.
func TestQuery_RequiresAccountID(t *testing.T) {
	d := &duckdbStore{cfg: Config{Provider: "s3", Bucket: "b"}}
	_, err := d.Query(t.Context(), store.Filter{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AccountID is required")
}
