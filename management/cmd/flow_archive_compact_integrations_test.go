//go:build archive_duckdb

package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	flowExports "github.com/openzro/openzro/management/server/flow_exports"
	mgmtStore "github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// testEncryptionKey is a throwaway 32-byte key, base64 as the store
// expects. Not a secret: it exists only to let the row round-trip.
const testEncryptionKey = "vCKp1HLogZ09qiOFWAz+mDpieiuPsBRtBSOvZd9d1hk="

// writeMgmtConfig lays down the smallest management config that points at
// a SQLite store in its own directory, and aims the package-level path at
// it. Both are restored when the test ends.
func writeMgmtConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfg := map[string]any{
		"Datadir":                dir,
		"DataStoreEncryptionKey": testEncryptionKey,
		"StoreConfig":            map[string]any{"Engine": "sqlite"},
		// loadMgmtConfig dereferences HttpConfig unconditionally, so an
		// empty object is the floor for a loadable config.
		"HttpConfig": map[string]any{},
	}
	body, err := json.Marshal(cfg)
	require.NoError(t, err)

	path := filepath.Join(dir, "management.json")
	require.NoError(t, os.WriteFile(path, body, 0o600))

	oldPath := types.MgmtConfigPath
	types.MgmtConfigPath = path
	// loadMgmtConfig overrides Datadir with the --datadir flag whenever
	// it is set, and the flag defaults to /var/lib/openzro. Point it at
	// the temp dir so the config it returns describes this test.
	oldDir := mgmtDataDir
	mgmtDataDir = dir
	t.Cleanup(func() {
		types.MgmtConfigPath = oldPath
		mgmtDataDir = oldDir
	})
	return dir
}

// seedArchiveRow writes one enabled GCS archive destination, the way the
// dashboard would.
func seedArchiveRow(t *testing.T, ctx context.Context, dir, bucket, prefix string) {
	t.Helper()
	store, err := mgmtStore.NewStore(ctx, types.SqliteStoreEngine, dir, nil, false)
	require.NoError(t, err)
	sqlStore, ok := store.(*mgmtStore.SqlStore)
	require.True(t, ok)
	exports, err := flowExports.NewStore(sqlStore.GetGormDB(), testEncryptionKey)
	require.NoError(t, err)
	_, err = exports.Save(ctx, flowExports.SaveInput{
		Name: "archive", Type: flowExports.TypeGCS, Enabled: true,
		GCS: &flowExports.GCSDestConfig{
			Bucket: bucket, Prefix: prefix, Format: "parquet",
			CredentialsJSON: `{"type":"service_account"}`,
		},
	})
	require.NoError(t, err)
	require.NoError(t, store.Close(ctx))
}

// The point of reading the dashboard's row: a deployment that configured
// its archive under Integrations sets no OPENZRO_FLOW_ARCHIVE_*_BUCKET
// anywhere, and its credential exists only as an encrypted row. Without
// this the command needs a second copy of the service-account key, which
// drifts from the console the first time one of them is rotated.
func TestArchiveConfigFromIntegrationsReadsTheDashboardRow(t *testing.T) {
	ctx := context.Background()
	dir := writeMgmtConfig(t)

	store, err := mgmtStore.NewStore(ctx, types.SqliteStoreEngine, dir, nil, false)
	require.NoError(t, err)
	sqlStore, ok := store.(*mgmtStore.SqlStore)
	require.True(t, ok)

	exports, err := flowExports.NewStore(sqlStore.GetGormDB(), testEncryptionKey)
	require.NoError(t, err)
	_, err = exports.Save(ctx, flowExports.SaveInput{
		Name:    "archive",
		Type:    flowExports.TypeGCS,
		Enabled: true,
		GCS: &flowExports.GCSDestConfig{
			Bucket:          "bucket-from-console",
			Prefix:          "flows",
			Format:          "parquet",
			CredentialsJSON: `{"type":"service_account"}`,
		},
	})
	require.NoError(t, err)
	require.NoError(t, store.Close(ctx))

	cfg, source, found, err := archiveConfigFromIntegrations(ctx)
	require.NoError(t, err)
	require.True(t, found, "a configured archive must be found")
	require.Equal(t, "gcs", cfg.Provider)
	require.Equal(t, "bucket-from-console", cfg.Bucket)
	require.Equal(t, "flows", cfg.Prefix)
	require.Equal(t, `{"type":"service_account"}`, string(cfg.CredentialsJSON),
		"the credential must come with it; that is the whole reason for reading the row")
	require.Contains(t, source, "flow_exports")
}

// Flags still win, so an operator can point the command at a copy of the
// bucket without touching what the deployment is running on.
func TestFlowArchiveCompactConfigFlagsWinOverIntegrations(t *testing.T) {
	ctx := context.Background()
	dir := writeMgmtConfig(t)

	store, err := mgmtStore.NewStore(ctx, types.SqliteStoreEngine, dir, nil, false)
	require.NoError(t, err)
	sqlStore, _ := store.(*mgmtStore.SqlStore)
	exports, err := flowExports.NewStore(sqlStore.GetGormDB(), testEncryptionKey)
	require.NoError(t, err)
	_, err = exports.Save(ctx, flowExports.SaveInput{
		Name: "archive", Type: flowExports.TypeGCS, Enabled: true,
		GCS: &flowExports.GCSDestConfig{
			Bucket: "bucket-from-console", Prefix: "flows", Format: "parquet",
		},
	})
	require.NoError(t, err)
	require.NoError(t, store.Close(ctx))

	opts := &flowArchiveCompactOptions{concurrency: 1}
	cmd := newFlowArchiveCompactCommand(opts)
	require.NoError(t, cmd.Flags().Set("bucket", "bucket-from-flag"))

	cfg, err := flowArchiveCompactConfig(ctx, cmd, opts)
	require.NoError(t, err)
	require.Equal(t, "bucket-from-flag", cfg.Bucket)
	require.Equal(t, "gcs", cfg.Provider, "the rest of the row still applies")
}

// A management config that exists but cannot be read is a real
// misconfiguration and must be reported. Only its absence is treated as
// "not running beside a management deployment".
func TestArchiveConfigFromIntegrationsReportsABrokenConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "management.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))
	old := types.MgmtConfigPath
	types.MgmtConfigPath = path
	t.Cleanup(func() { types.MgmtConfigPath = old })

	_, _, found, err := archiveConfigFromIntegrations(context.Background())
	require.Error(t, err)
	require.False(t, found)
}

// The escape hatch. This command deletes objects, so an operator
// pointing it at a copy of the bucket -- or working around a broken
// deployment -- must not be blocked because the management config cannot
// be read. Flags that fully name the archive mean nothing else is
// consulted at all.
//
// The config here exists and is unparseable, which is the case that used
// to fail: the command returned that error before it ever reached the
// flags.
func TestFlowArchiveCompactConfigFlagsSkipABrokenManagementConfig(t *testing.T) {
	broken := filepath.Join(t.TempDir(), "management.json")
	require.NoError(t, os.WriteFile(broken, []byte("{not json"), 0o600))
	old := types.MgmtConfigPath
	types.MgmtConfigPath = broken
	t.Cleanup(func() { types.MgmtConfigPath = old })

	opts := &flowArchiveCompactOptions{concurrency: 1}
	cmd := newFlowArchiveCompactCommand(opts)
	require.NoError(t, cmd.Flags().Set("provider", "gcs"))
	require.NoError(t, cmd.Flags().Set("bucket", "a-copy-of-the-bucket"))

	cfg, err := flowArchiveCompactConfig(context.Background(), cmd, opts)
	require.NoError(t, err, "flags that name the archive must not require a readable config")
	require.Equal(t, "a-copy-of-the-bucket", cfg.Bucket)
	require.Equal(t, "gcs", cfg.Provider)
}

// A flag that only names part of the archive is an override, not an
// escape hatch: the row still supplies the rest, including the
// credential.
func TestFlowArchiveCompactConfigPartialFlagsStillReadIntegrations(t *testing.T) {
	ctx := context.Background()
	dir := writeMgmtConfig(t)
	seedArchiveRow(t, ctx, dir, "bucket-from-console", "flows")

	opts := &flowArchiveCompactOptions{concurrency: 1}
	cmd := newFlowArchiveCompactCommand(opts)
	require.NoError(t, cmd.Flags().Set("prefix", "prefix-from-flag"))

	cfg, err := flowArchiveCompactConfig(ctx, cmd, opts)
	require.NoError(t, err)
	require.Equal(t, "bucket-from-console", cfg.Bucket, "the row still names the bucket")
	require.Equal(t, "prefix-from-flag", cfg.Prefix, "and the flag still overrides what it names")
	require.NotEmpty(t, cfg.CredentialsJSON, "the credential comes with the row")
}

// The environment is all-or-nothing, and that is worth pinning because
// it surprises people. ConfigFromEnv reports "not configured" unless a
// bucket is set, and returns an empty Config -- so exporting only a
// prefix, expecting it to override the dashboard, does nothing.
//
// It is consistent with how the reader treats the same variables
// everywhere, which is why it stays this way rather than growing a
// special case. Use a flag to override one field.
func TestFlowArchiveCompactConfigEnvWithoutBucketDoesNotOverrideIntegrations(t *testing.T) {
	ctx := context.Background()
	dir := writeMgmtConfig(t)
	seedArchiveRow(t, ctx, dir, "bucket-from-console", "flows")
	t.Setenv("OPENZRO_FLOW_ARCHIVE_GCS_PREFIX", "ignored-without-a-bucket")

	opts := &flowArchiveCompactOptions{concurrency: 1}
	cmd := newFlowArchiveCompactCommand(opts)

	cfg, err := flowArchiveCompactConfig(ctx, cmd, opts)
	require.NoError(t, err)
	require.Equal(t, "bucket-from-console", cfg.Bucket)
	require.Equal(t, "flows", cfg.Prefix,
		"a prefix with no bucket beside it is not a configured archive")
}

// With a bucket beside it, the environment does describe an archive, and
// it wins outright -- the row is never read.
func TestFlowArchiveCompactConfigCompleteEnvWinsOverIntegrations(t *testing.T) {
	ctx := context.Background()
	dir := writeMgmtConfig(t)
	seedArchiveRow(t, ctx, dir, "bucket-from-console", "flows")
	t.Setenv("OPENZRO_FLOW_ARCHIVE_GCS_BUCKET", "bucket-from-env")
	t.Setenv("OPENZRO_FLOW_ARCHIVE_GCS_PREFIX", "prefix-from-env")

	opts := &flowArchiveCompactOptions{concurrency: 1}
	cmd := newFlowArchiveCompactCommand(opts)

	cfg, err := flowArchiveCompactConfig(ctx, cmd, opts)
	require.NoError(t, err)
	require.Equal(t, "bucket-from-env", cfg.Bucket)
	require.Equal(t, "prefix-from-env", cfg.Prefix)
}
