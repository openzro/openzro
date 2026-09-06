package archive

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The shape a dashboard-configured deployment actually has: the bucket
// lives in a flow_exports row, so no OPENZRO_FLOW_ARCHIVE_*_BUCKET is
// set, and configFromEnv therefore reports "not configured" and returns
// an empty Config. A caller that supplies the bucket by flag -- which is
// what the compaction command does -- would otherwise start from that
// empty Config with every env-supplied credential already discarded.
//
// For reads this was survivable, because the HMAC pair is restored. For
// writes it is not: they go through the GCS SDK with the service-account
// credential, and losing it falls back to ambient credentials. On a GKE
// node without Workload Identity those are the node's service account,
// which is typically read-only, so the first symptom is a permission
// error on write that points nowhere near the discarded configuration.
func TestConfigWithRuntimeEnvKeepsGCSCredentialsWithoutBucketEnv(t *testing.T) {
	t.Setenv(envGCSCredentialsJSON, `{"type":"service_account","project_id":"p"}`)
	t.Setenv(envGCSHMACKeyID, "hmac-key")
	t.Setenv(envGCSHMACSecret, "hmac-secret")

	// Exactly what the CLI does: read the env, find nothing configured,
	// then fill the provider and bucket in from flags.
	cfg, ok := configFromEnv()
	require.False(t, ok, "no bucket in env is the premise of this test")
	require.Empty(t, cfg.CredentialsJSON, "and the empty Config is what the caller starts from")

	cfg.Provider = "gcs"
	cfg.Bucket = "flow-archive"

	got := configWithRuntimeEnv(cfg)
	require.Equal(t, `{"type":"service_account","project_id":"p"}`, string(got.CredentialsJSON),
		"the credential that writes and deletes must survive the flag path")
	require.Equal(t, "hmac-key", got.AccessKeyID, "and the read credential alongside it")
	require.Equal(t, "hmac-secret", got.SecretAccessKey)
}

// A credential passed explicitly must not be replaced by the
// environment. The env is a fallback, not an override.
func TestConfigWithRuntimeEnvDoesNotOverrideExplicitCredentials(t *testing.T) {
	t.Setenv(envGCSCredentialsJSON, `{"from":"env"}`)
	t.Setenv(envGCSCredentialsFile, "/from/env.json")

	got := configWithRuntimeEnv(Config{
		Provider:        "gcs",
		Bucket:          "b",
		CredentialsJSON: []byte(`{"from":"caller"}`),
		CredentialsFile: "/from/caller.json",
	})
	require.Equal(t, `{"from":"caller"}`, string(got.CredentialsJSON))
	require.Equal(t, "/from/caller.json", got.CredentialsFile)
}

// S3 must not pick up GCS credentials, which would be a confusing way to
// fail: the AWS SDK would ignore them and fall back to its own chain.
func TestConfigWithRuntimeEnvLeavesS3Alone(t *testing.T) {
	t.Setenv(envGCSCredentialsJSON, `{"from":"env"}`)
	got := configWithRuntimeEnv(Config{Provider: "s3", Bucket: "b"})
	require.Empty(t, got.CredentialsJSON)
}

// S3 has the same hole, and it costs more: one key pair signs both the
// DuckDB reads and the SDK's writes, so losing it takes the whole
// operation rather than half of it.
//
// The symptom is also worse. The AWS SDK falls through its own chain to
// the instance metadata service, so the failure reads "no EC2 IMDS role
// found" on a machine that has no IMDS -- observed while testing a GCS
// bucket through its S3-compatible endpoint, and pointing nowhere near
// the configuration that was dropped.
func TestConfigWithRuntimeEnvKeepsS3CredentialsWithoutBucketEnv(t *testing.T) {
	t.Setenv(envS3AccessKey, "s3-key")
	t.Setenv(envS3SecretKey, "s3-secret")

	cfg, ok := configFromEnv()
	require.False(t, ok)
	require.Empty(t, cfg.AccessKeyID)

	cfg.Provider = "s3"
	cfg.Bucket = "flow-archive"

	got := configWithRuntimeEnv(cfg)
	require.Equal(t, "s3-key", got.AccessKeyID)
	require.Equal(t, "s3-secret", got.SecretAccessKey)
}

// And GCS must not pick up the S3 variables, which would silently sign
// httpfs requests with the wrong pair.
func TestConfigWithRuntimeEnvDoesNotCrossProviders(t *testing.T) {
	t.Setenv(envS3AccessKey, "s3-key")
	t.Setenv(envS3SecretKey, "s3-secret")
	got := configWithRuntimeEnv(Config{Provider: "gcs", Bucket: "b"})
	require.Empty(t, got.AccessKeyID, "GCS reads authenticate with the HMAC pair, not the S3 one")
}
