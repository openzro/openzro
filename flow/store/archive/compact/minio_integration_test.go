//go:build archive_duckdb

package compact_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	_ "github.com/marcboeker/go-duckdb"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	flowArchive "github.com/openzro/openzro/flow/store/archive"
	"github.com/openzro/openzro/flow/store/archive/compact"
)

// Everything else in this package runs the compaction over a local
// directory, which proves the logic and proves nothing about the object
// store. These assumptions had never been executed anywhere:
//
//   - that List pages past the 1000-key limit,
//   - that Delete of an absent key succeeds rather than erroring,
//   - that Write is durable by the time it returns,
//   - that DuckDB can read the objects the SDK wrote, over HTTP, with
//     the same credentials.
//
// Four defects on this read path shipped to production having passed
// tests: a non-existent extension, a parameter that could not bind,
// absent partition pruning, and file sizes nobody measured. The
// difference here is that this command deletes data.
//
// MinIO is S3-compatible and is the image this repository already uses
// for S3 integration work.

const (
	minioAccessKey = "minioadmin"
	minioSecretKey = "minioadmin"
	testBucket     = "flow-archive"
	testPrefix     = "flows"
)

func startMinIO(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			// Pinned rather than :latest, for the reason the existing
			// MinIO test gives: rolling tags ship behavior changes.
			Image:        "minio/minio:RELEASE.2025-04-22T22-12-26Z",
			ExposedPorts: []string{"9000/tcp"},
			Cmd:          []string{"server", "/data"},
			Env: map[string]string{
				"MINIO_ROOT_USER":     minioAccessKey,
				"MINIO_ROOT_PASSWORD": minioSecretKey,
			},
			WaitingFor: wait.ForHTTP("/minio/health/ready").
				WithPort("9000/tcp").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("failed to start MinIO container: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	host, err := c.Host(ctx)
	require.NoError(t, err)
	port, err := c.MappedPort(ctx, "9000/tcp")
	require.NoError(t, err)
	endpoint := "http://" + host + ":" + port.Port()

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(minioAccessKey, minioSecretKey, "")))
	require.NoError(t, err)
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(testBucket)})
	require.NoError(t, err)
	return endpoint
}

// harness wires the two halves the way the CLI does: DuckDB reads the
// bucket, the SDK writes and deletes it.
type harness struct {
	store    *compact.S3Store
	db       *sql.DB
	local    *sql.DB
	endpoint string
	tmp      string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	endpoint := startMinIO(t)
	ctx := context.Background()

	store, err := compact.NewS3(ctx, compact.S3Config{
		Bucket: testBucket, Prefix: testPrefix, Region: "us-east-1",
		Endpoint: endpoint, AccessKey: minioAccessKey, SecretKey: minioSecretKey,
	})
	require.NoError(t, err)

	db, err := flowArchive.OpenParquetDB(ctx, flowArchive.Config{
		Provider: "s3", Bucket: testBucket, Prefix: testPrefix, Region: "us-east-1",
		Endpoint: endpoint, AccessKeyID: minioAccessKey, SecretAccessKey: minioSecretKey,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	local, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = local.Close() })

	return &harness{store: store, db: db, local: local, endpoint: endpoint, tmp: t.TempDir()}
}

// seed builds a parquet locally and puts the bytes through the store --
// the same order the sink uses, so the object under test is the one
// production would have produced.
func (h *harness) seed(t *testing.T, day, account, rowAccount, ts string, n int) string {
	t.Helper()
	path := filepath.Join(h.tmp, fmt.Sprintf("seed-%d.parquet", n))
	_, err := h.local.ExecContext(context.Background(), fmt.Sprintf(
		`COPY (SELECT TIMESTAMP '%s' AS received_at, '%s' AS account_id,
		        'peer-%d' AS peer_id, 'ev-%d' AS event_id) TO '%s' (FORMAT PARQUET)`,
		ts, rowAccount, n, n, path))
	require.NoError(t, err)
	body, err := os.ReadFile(path)
	require.NoError(t, err)

	key := fmt.Sprintf("%s/year=2026/month=07/day=%s/account=%s/%d.parquet", testPrefix, day, account, n)
	require.NoError(t, h.store.Write(context.Background(), key, body))
	return key
}

func (h *harness) compactor(dryRun bool) *compact.Compactor {
	return &compact.Compactor{
		DB:        h.db,
		Store:     h.store,
		ReadRoot:  "s3://" + testBucket + "/" + testPrefix,
		KeyPrefix: testPrefix,
		DryRun:    dryRun,
	}
}

func (h *harness) list(t *testing.T, prefix string) []string {
	t.Helper()
	keys, err := h.store.List(context.Background(), prefix)
	require.NoError(t, err)
	return keys
}

// The whole cycle against a real object store: many small objects in,
// one per account out, originals gone, and every row still readable
// through the archive reader afterwards.
func TestCompactDayAgainstObjectStore(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test; needs a container runtime")
	}
	h := newHarness(t)
	ctx := context.Background()

	for i := range 40 {
		account := "acct-A"
		if i%4 == 0 {
			account = "acct-B"
		}
		h.seed(t, "29", account, account, fmt.Sprintf("2026-07-29 %02d:00:00", i%24), i)
	}
	require.Len(t, h.list(t, testPrefix+"/year=2026/month=07/day=29"), 40)

	res, err := h.compactor(false).CompactDay(ctx, time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, 40, res.ObjectsBefore)
	require.Equal(t, 2, res.ObjectsAfter, "one object per account")
	require.Equal(t, int64(40), res.Rows)

	after := h.list(t, testPrefix+"/year=2026/month=07/day=29")
	require.Len(t, after, 2, "the originals must be gone from the bucket, not merely replaced")

	// And the reader still answers, which is the only thing an operator
	// actually cares about after a compaction.
	var rows int64
	require.NoError(t, h.db.QueryRowContext(ctx,
		`SELECT count(*) FROM read_parquet(
			's3://`+testBucket+`/`+testPrefix+`/year=*/month=*/day=*/account=*/*.parquet',
			hive_partitioning=true)`).Scan(&rows))
	require.Equal(t, int64(40), rows)
}

// List pages at 1000 keys. Nothing had ever exercised the second page,
// and a List that silently stopped there would leave objects
// uncompacted -- and, worse, undeleted while their rows were copied into
// a replacement, producing duplicates in every later query.
func TestListPagesPastTheThousandKeyLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test; needs a container runtime")
	}
	h := newHarness(t)

	// One payload, reused: this test is about the listing, not the
	// contents.
	path := filepath.Join(h.tmp, "one.parquet")
	_, err := h.local.Exec(
		`COPY (SELECT TIMESTAMP '2026-07-30 10:00:00' AS received_at, 'acct-A' AS account_id) TO '` +
			path + `' (FORMAT PARQUET)`)
	require.NoError(t, err)
	body, err := os.ReadFile(path)
	require.NoError(t, err)

	const objects = 1005
	prefix := testPrefix + "/year=2026/month=07/day=30/account=acct-A"
	for i := range objects {
		require.NoError(t, h.store.Write(context.Background(),
			fmt.Sprintf("%s/%04d.parquet", prefix, i), body))
	}

	keys := h.list(t, testPrefix+"/year=2026/month=07/day=30")
	require.Len(t, keys, objects,
		"a List that stops at the first page leaves objects undeleted while their rows are copied into the replacement")
}

// Delete of an absent key is treated as success throughout the
// compactor, so a retried run converges instead of failing on its own
// earlier progress. That was an assumption about the SDK, never checked.
func TestDeleteAbsentKeyIsNotAnError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test; needs a container runtime")
	}
	h := newHarness(t)
	require.NoError(t, h.store.Delete(context.Background(),
		testPrefix+"/year=2026/month=07/day=31/account=acct-A/never-written.parquet"))
}

// A dry-run must leave the bucket exactly as it found it. This is the
// promise the CLI makes by defaulting to it.
func TestDryRunAgainstObjectStoreWritesNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test; needs a container runtime")
	}
	h := newHarness(t)
	ctx := context.Background()

	var seeded []string
	for i := range 6 {
		seeded = append(seeded, h.seed(t, "28", "acct-A", "acct-A", "2026-07-28 10:00:00", i))
	}

	res, err := h.compactor(true).CompactDay(ctx, time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.True(t, res.DryRun)
	require.Equal(t, 6, res.ObjectsBefore)

	after := h.list(t, testPrefix+"/year=2026/month=07/day=28")
	require.ElementsMatch(t, seeded, after,
		"a dry-run must not add, replace or remove a single object")
}
