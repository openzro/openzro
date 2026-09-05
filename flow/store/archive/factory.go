package archive

import (
	"os"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/flow/store"
)

// Env-var contract — mirrors flow/sinks/factory.go so an operator
// configures the bucket once and both write + read paths read the
// same bucket/prefix values. S3 can rely on provider defaults when no
// explicit key is supplied; GCS reads additionally require HMAC
// interoperability keys because DuckDB cannot authenticate with the
// service-account JSON used by the write-side sink.
const (
	envS3Bucket    = "OPENZRO_FLOW_ARCHIVE_S3_BUCKET"
	envS3Region    = "OPENZRO_FLOW_ARCHIVE_S3_REGION"
	envS3Endpoint  = "OPENZRO_FLOW_ARCHIVE_S3_ENDPOINT"
	envS3AccessKey = "OPENZRO_FLOW_ARCHIVE_S3_ACCESS_KEY"
	envS3SecretKey = "OPENZRO_FLOW_ARCHIVE_S3_SECRET_KEY"
	envS3Prefix    = "OPENZRO_FLOW_ARCHIVE_S3_PREFIX"
	// The sink's flush interval, read here only to size the
	// partition-pruning margin. See Config.MaxBatchSpan.
	envS3FlushInterval = "OPENZRO_FLOW_ARCHIVE_S3_FLUSH_INTERVAL"

	envGCSBucket          = "OPENZRO_FLOW_ARCHIVE_GCS_BUCKET"
	envGCSPrefix          = "OPENZRO_FLOW_ARCHIVE_GCS_PREFIX"
	envGCSCredentialsFile = "OPENZRO_FLOW_ARCHIVE_GCS_CREDENTIALS_FILE"
	envGCSCredentialsJSON = "OPENZRO_FLOW_ARCHIVE_GCS_CREDENTIALS_JSON"
	envGCSProjectID       = "OPENZRO_FLOW_ARCHIVE_GCS_PROJECT_ID"
	// DuckDB reads GCS through httpfs, whose GCS secret takes an HMAC
	// key pair and nothing else — service account JSON is rejected with
	// "Unknown parameter 'credential_chain' for secret type 'gcs'". The
	// sink keeps using the JSON to *write*; only the read path needs
	// these. Create them in the console under Cloud Storage →
	// Interoperability.
	envGCSHMACKeyID  = "OPENZRO_FLOW_ARCHIVE_GCS_HMAC_KEY_ID"
	envGCSHMACSecret = "OPENZRO_FLOW_ARCHIVE_GCS_HMAC_SECRET"
	envGCSEndpoint   = "OPENZRO_FLOW_ARCHIVE_GCS_ENDPOINT"
	// As above: the pruning margin has to cover what the writer could
	// have produced, not what it produces by default.
	envGCSFlushInterval = "OPENZRO_FLOW_ARCHIVE_GCS_FLUSH_INTERVAL"

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
//   - (nil, nil) when the format is empty, "ndjson", or otherwise not
//     "parquet" (the sink defaults those values to NDJSON, and the read
//     path doesn't support NDJSON yet — log it and operate as if no
//     archive).
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
	cfg, ok := configFromEnv()
	if !ok {
		// No bucket configured → operator opted out, not a failure.
		return nil, nil
	}

	format := os.Getenv(envFormat)
	if format != "parquet" {
		// NDJSON archives exist but the read path does not target them
		// yet. Treat as "no archive" so the federated wrapper stays on
		// hot-only; enabling the DuckDB store here would query
		// *.parquet while the sink is writing *.ndjson.gz.
		log.Warnf(
			"flow archive: bucket configured but %s=%q resolves to ndjson; "+
				"dashboard archive reads require %s=parquet, so older events remain hot-store only",
			envFormat, format, envFormat)
		return nil, nil
	}

	return NewParquet(cfg)
}

// NewParquet constructs an archive reader for a source whose effective
// write format is already known to be Parquet. It applies the same
// runtime bounds as NewFromEnv so env-configured and dashboard-
// configured archives share timeout, memory and concurrency behavior.
// GCS dashboard rows still write through service-account credentials;
// the DuckDB reader takes its HMAC interoperability pair from env.
func NewParquet(cfg Config) (store.Store, error) {
	cfg.QueryTimeout = parseTimeout(os.Getenv(envQueryTimeout))
	cfg.MemoryLimit = os.Getenv(envMemoryLimit)
	cfg.Threads = parseInt(os.Getenv(envThreads))
	cfg.MaxConcurrentQueries = parseInt(os.Getenv(envMaxConcurrentQueries))
	if cfg.Provider == "gcs" {
		if cfg.AccessKeyID == "" {
			cfg.AccessKeyID = os.Getenv(envGCSHMACKeyID)
		}
		if cfg.SecretAccessKey == "" {
			cfg.SecretAccessKey = os.Getenv(envGCSHMACSecret)
		}
	}
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
			AccessKeyID:     os.Getenv(envGCSHMACKeyID),
			SecretAccessKey: os.Getenv(envGCSHMACSecret),
			MaxBatchSpan:    parseTimeout(os.Getenv(envGCSFlushInterval)),
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
			MaxBatchSpan:    parseTimeout(os.Getenv(envS3FlushInterval)),
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
