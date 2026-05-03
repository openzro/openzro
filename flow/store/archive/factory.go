package archive

import (
	"os"
	"strconv"
	"time"

	"github.com/openzro/openzro/flow/store"
)

// Env-var contract — mirrors flow/sinks/factory.go so an operator
// configures the bucket once and both write + read paths read the
// same values. Only OPENZRO_FLOW_ARCHIVE_S3_BUCKET (or its GCS
// counterpart) is required to enable the archive store; the rest
// inherit defaults.
const (
	envS3Bucket    = "OPENZRO_FLOW_ARCHIVE_S3_BUCKET"
	envS3Region    = "OPENZRO_FLOW_ARCHIVE_S3_REGION"
	envS3Endpoint  = "OPENZRO_FLOW_ARCHIVE_S3_ENDPOINT"
	envS3AccessKey = "OPENZRO_FLOW_ARCHIVE_S3_ACCESS_KEY"
	envS3SecretKey = "OPENZRO_FLOW_ARCHIVE_S3_SECRET_KEY"
	envS3Prefix    = "OPENZRO_FLOW_ARCHIVE_S3_PREFIX"

	envGCSBucket          = "OPENZRO_FLOW_ARCHIVE_GCS_BUCKET"
	envGCSPrefix          = "OPENZRO_FLOW_ARCHIVE_GCS_PREFIX"
	envGCSCredentialsFile = "OPENZRO_FLOW_ARCHIVE_GCS_CREDENTIALS_FILE"
	envGCSCredentialsJSON = "OPENZRO_FLOW_ARCHIVE_GCS_CREDENTIALS_JSON"
	envGCSProjectID       = "OPENZRO_FLOW_ARCHIVE_GCS_PROJECT_ID"
	envGCSEndpoint        = "OPENZRO_FLOW_ARCHIVE_GCS_ENDPOINT"

	envFormat       = "OPENZRO_FLOW_ARCHIVE_FORMAT"
	envQueryTimeout = "OPENZRO_FLOW_ARCHIVE_QUERY_TIMEOUT"

	// Memory bounds — tuned to fit the management's typical 1Gi pod
	// limit. Operators on bigger pods can lift these without code
	// changes; on smaller pods they should LOWER memory_limit and
	// max_concurrent so the archive footprint stays well below the
	// cgroup ceiling.
	envMemoryLimit          = "OPENZRO_FLOW_ARCHIVE_MEMORY_LIMIT"
	envThreads              = "OPENZRO_FLOW_ARCHIVE_THREADS"
	envMaxConcurrentQueries = "OPENZRO_FLOW_ARCHIVE_MAX_CONCURRENT_QUERIES"
)

// NewFromEnv constructs the archive Store from the operator's env
// vars. Returns:
//
//   - (store, nil) when the bucket is configured AND the binary was
//     built with `archive_duckdb` AND the format is "parquet".
//   - (nil, nil) when the bucket is not configured at all (operator
//     opted out — federated falls back to hot-only).
//   - (nil, nil) when the format is "ndjson" (read path doesn't
//     support that yet — log it and operate as if no archive).
//   - (nil, ErrUnavailable) when the binary was built without
//     archive_duckdb. Caller should fall back to hot-only with a
//     warning rather than fail.
//   - (nil, err) on misconfiguration (bucket without provider, etc).
//
// Provider precedence: GCS native takes priority when both buckets
// are set, mirroring how the sinks factory tries GCS first then S3.
// In practice an operator picks one, so the precedence rarely
// matters.
func NewFromEnv() (store.Store, error) {
	format := os.Getenv(envFormat)
	if format != "" && format != "parquet" {
		// NDJSON archives exist but the read path does not target
		// them yet. Treat as "no archive" so the federated wrapper
		// stays on hot-only and the operator gets a clear log line
		// rather than a spurious error during boot.
		return nil, nil
	}

	cfg, ok := configFromEnv()
	if !ok {
		// No bucket configured → operator opted out, not a failure.
		return nil, nil
	}

	cfg.QueryTimeout = parseTimeout(os.Getenv(envQueryTimeout))
	cfg.MemoryLimit = os.Getenv(envMemoryLimit)
	cfg.Threads = parseInt(os.Getenv(envThreads))
	cfg.MaxConcurrentQueries = parseInt(os.Getenv(envMaxConcurrentQueries))
	return New(cfg)
}

// parseInt parses an env-supplied integer, returning 0 on empty or
// malformed input. The constructor substitutes its default when the
// caller's value is non-positive, so a malformed env yields the
// safe default rather than a startup failure.
func parseInt(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// configFromEnv inspects the env once and returns the populated
// Config plus a "configured" flag. Separated from NewFromEnv so the
// tests can exercise the env-parsing layer without going through
// the DuckDB constructor.
func configFromEnv() (Config, bool) {
	if bucket := os.Getenv(envGCSBucket); bucket != "" {
		cfg := Config{
			Provider:        "gcs",
			Bucket:          bucket,
			Prefix:          os.Getenv(envGCSPrefix),
			Endpoint:        os.Getenv(envGCSEndpoint),
			ProjectID:       os.Getenv(envGCSProjectID),
			CredentialsFile: os.Getenv(envGCSCredentialsFile),
		}
		if v := os.Getenv(envGCSCredentialsJSON); v != "" {
			cfg.CredentialsJSON = []byte(v)
		}
		return cfg, true
	}
	if bucket := os.Getenv(envS3Bucket); bucket != "" {
		return Config{
			Provider:        "s3",
			Bucket:          bucket,
			Prefix:          os.Getenv(envS3Prefix),
			Endpoint:        os.Getenv(envS3Endpoint),
			Region:          os.Getenv(envS3Region),
			AccessKeyID:     os.Getenv(envS3AccessKey),
			SecretAccessKey: os.Getenv(envS3SecretKey),
		}, true
	}
	return Config{}, false
}

// parseTimeout reads a Go duration string from the env, falling back
// to zero when empty / malformed (the duckdb store substitutes its
// own default in that case). Operators usually don't touch this;
// the default 60s is calibrated to give DuckDB enough time to scan
// a typical month of partitions.
func parseTimeout(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0
	}
	return d
}
