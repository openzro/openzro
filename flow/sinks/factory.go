package sinks

import (
	"context"
	"os"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/flow/store"
)

// Environment variable names. Stable; do not rename.
const (
	envElasticURL           = "OPENZRO_FLOW_EXPORT_ELASTIC_URL"
	envElasticIndex         = "OPENZRO_FLOW_EXPORT_ELASTIC_INDEX"
	envElasticAPIKey        = "OPENZRO_FLOW_EXPORT_ELASTIC_API_KEY"
	envElasticUsername      = "OPENZRO_FLOW_EXPORT_ELASTIC_USERNAME"
	envElasticPassword      = "OPENZRO_FLOW_EXPORT_ELASTIC_PASSWORD"
	envElasticBatchSize     = "OPENZRO_FLOW_EXPORT_ELASTIC_BATCH_SIZE"
	envElasticFlushInterval = "OPENZRO_FLOW_EXPORT_ELASTIC_FLUSH_INTERVAL"
	envElasticBufferSize    = "OPENZRO_FLOW_EXPORT_ELASTIC_BUFFER_SIZE"

	envS3Bucket           = "OPENZRO_FLOW_ARCHIVE_S3_BUCKET"
	envS3Region           = "OPENZRO_FLOW_ARCHIVE_S3_REGION"
	envS3Endpoint         = "OPENZRO_FLOW_ARCHIVE_S3_ENDPOINT"
	envS3AccessKey        = "OPENZRO_FLOW_ARCHIVE_S3_ACCESS_KEY"
	envS3SecretKey        = "OPENZRO_FLOW_ARCHIVE_S3_SECRET_KEY"
	envS3Prefix           = "OPENZRO_FLOW_ARCHIVE_S3_PREFIX"
	envS3FlushInterval    = "OPENZRO_FLOW_ARCHIVE_S3_FLUSH_INTERVAL"
	envS3MaxEventsPerFile = "OPENZRO_FLOW_ARCHIVE_S3_MAX_EVENTS_PER_FILE"
	envS3BufferSize       = "OPENZRO_FLOW_ARCHIVE_S3_BUFFER_SIZE"

	// Datadog Logs Intake (streaming)
	envDDFlowAPIKey        = "OPENZRO_FLOW_EXPORT_DATADOG_API_KEY"
	envDDFlowSite          = "OPENZRO_FLOW_EXPORT_DATADOG_SITE"
	envDDFlowURL           = "OPENZRO_FLOW_EXPORT_DATADOG_URL"
	envDDFlowService       = "OPENZRO_FLOW_EXPORT_DATADOG_SERVICE"
	envDDFlowSource        = "OPENZRO_FLOW_EXPORT_DATADOG_SOURCE"
	envDDFlowTags          = "OPENZRO_FLOW_EXPORT_DATADOG_TAGS"
	envDDFlowBatchSize     = "OPENZRO_FLOW_EXPORT_DATADOG_BATCH_SIZE"
	envDDFlowFlushInterval = "OPENZRO_FLOW_EXPORT_DATADOG_FLUSH_INTERVAL"
	envDDFlowBufferSize    = "OPENZRO_FLOW_EXPORT_DATADOG_BUFFER_SIZE"

	// Google Cloud Storage native (cold archive). Distinct from the
	// S3 path — operators wanting GCS Interop mode keep using the
	// existing S3 vars; this set is for native auth (Workload
	// Identity, Service Account JSON).
	envGCSBucket           = "OPENZRO_FLOW_ARCHIVE_GCS_BUCKET"
	envGCSPrefix           = "OPENZRO_FLOW_ARCHIVE_GCS_PREFIX"
	envGCSCredentialsFile  = "OPENZRO_FLOW_ARCHIVE_GCS_CREDENTIALS_FILE"
	envGCSCredentialsJSON  = "OPENZRO_FLOW_ARCHIVE_GCS_CREDENTIALS_JSON"
	envGCSProjectID        = "OPENZRO_FLOW_ARCHIVE_GCS_PROJECT_ID"
	envGCSEndpoint         = "OPENZRO_FLOW_ARCHIVE_GCS_ENDPOINT"
	envGCSFlushInterval    = "OPENZRO_FLOW_ARCHIVE_GCS_FLUSH_INTERVAL"
	envGCSMaxEventsPerFile = "OPENZRO_FLOW_ARCHIVE_GCS_MAX_EVENTS_PER_FILE"
	envGCSBufferSize       = "OPENZRO_FLOW_ARCHIVE_GCS_BUFFER_SIZE"

	// Generic HTTP webhook (streaming). HEADERS is comma-separated
	// "Name: Value" pairs — that is where auth lives (Authorization,
	// X-Api-Key, …), so any scheme works without a dedicated knob.
	envHTTPURL            = "OPENZRO_FLOW_EXPORT_HTTP_URL"
	envHTTPHeaders        = "OPENZRO_FLOW_EXPORT_HTTP_HEADERS"
	envHTTPTimeout        = "OPENZRO_FLOW_EXPORT_HTTP_TIMEOUT"
	envHTTPMaxAttempts    = "OPENZRO_FLOW_EXPORT_HTTP_MAX_ATTEMPTS"
	envHTTPInitialBackoff = "OPENZRO_FLOW_EXPORT_HTTP_INITIAL_BACKOFF"
	envHTTPBatchSize      = "OPENZRO_FLOW_EXPORT_HTTP_BATCH_SIZE"
	envHTTPFlushInterval  = "OPENZRO_FLOW_EXPORT_HTTP_FLUSH_INTERVAL"
	envHTTPBufferSize     = "OPENZRO_FLOW_EXPORT_HTTP_BUFFER_SIZE"

	// Archive on-disk format. Per ADR-0012: shared between S3 and GCS
	// so an operator running both backends doesn't have to duplicate
	// the knob. Empty / unrecognized values default to "ndjson"
	// (back-compat with deployments pre-Parquet); set "parquet" to
	// opt into the federated read path served by flow/store/archive.
	envArchiveFormat = "OPENZRO_FLOW_ARCHIVE_FORMAT"
)

// NewFromEnv reads OPENZRO_FLOW_EXPORT_* variables and returns the
// configured set of sinks. Empty slice is the default and means "no
// streaming destinations" — pure hot-store behavior.
func NewFromEnv(ctx context.Context) ([]store.Sink, error) {
	out := []store.Sink{}

	if exp, err := newElasticFromEnv(ctx); err != nil {
		return nil, err
	} else if exp != nil {
		out = append(out, exp)
	}

	if exp, err := newS3FromEnv(ctx); err != nil {
		return nil, err
	} else if exp != nil {
		out = append(out, exp)
	}

	if exp, err := newDatadogFromEnv(ctx); err != nil {
		return nil, err
	} else if exp != nil {
		out = append(out, exp)
	}

	if exp, err := newGCSFromEnv(ctx); err != nil {
		return nil, err
	} else if exp != nil {
		out = append(out, exp)
	}

	if exp, err := newHTTPFromEnv(ctx); err != nil {
		return nil, err
	} else if exp != nil {
		out = append(out, exp)
	}

	return out, nil
}

func newHTTPFromEnv(ctx context.Context) (store.Sink, error) {
	url := os.Getenv(envHTTPURL)
	if url == "" {
		return nil, nil
	}
	exp, err := NewHTTP(HTTPConfig{
		URL:            url,
		Headers:        parseHTTPHeaders(os.Getenv(envHTTPHeaders)),
		Timeout:        envDuration(envHTTPTimeout),
		MaxAttempts:    envInt(envHTTPMaxAttempts),
		InitialBackoff: envDuration(envHTTPInitialBackoff),
		BatchSize:      envInt(envHTTPBatchSize),
		FlushInterval:  envDuration(envHTTPFlushInterval),
		BufferSize:     envInt(envHTTPBufferSize),
	})
	if err != nil {
		return nil, err
	}
	log.WithContext(ctx).Infof("flow streaming enabled: HTTP webhook → %s", url)
	return exp, nil
}

func newDatadogFromEnv(ctx context.Context) (store.Sink, error) {
	apiKey := os.Getenv(envDDFlowAPIKey)
	if apiKey == "" {
		return nil, nil
	}
	exp, err := NewDatadog(DatadogConfig{
		APIKey:        apiKey,
		Site:          os.Getenv(envDDFlowSite),
		URL:           os.Getenv(envDDFlowURL),
		Service:       os.Getenv(envDDFlowService),
		Source:        os.Getenv(envDDFlowSource),
		Tags:          os.Getenv(envDDFlowTags),
		BatchSize:     envInt(envDDFlowBatchSize),
		FlushInterval: envDuration(envDDFlowFlushInterval),
		BufferSize:    envInt(envDDFlowBufferSize),
	})
	if err != nil {
		return nil, err
	}
	target := os.Getenv(envDDFlowURL)
	if target == "" {
		site := os.Getenv(envDDFlowSite)
		if site == "" {
			site = "us1"
		}
		target = "site=" + site
	}
	log.WithContext(ctx).Infof("flow streaming enabled: Datadog Logs Intake → %s", target)
	return exp, nil
}

func newGCSFromEnv(ctx context.Context) (store.Sink, error) {
	bucket := os.Getenv(envGCSBucket)
	if bucket == "" {
		return nil, nil
	}
	format := os.Getenv(envArchiveFormat)
	cfg := GCSConfig{
		Bucket:           bucket,
		Prefix:           os.Getenv(envGCSPrefix),
		CredentialsFile:  os.Getenv(envGCSCredentialsFile),
		ProjectID:        os.Getenv(envGCSProjectID),
		Endpoint:         os.Getenv(envGCSEndpoint),
		FlushInterval:    envDuration(envGCSFlushInterval),
		MaxEventsPerFile: envInt(envGCSMaxEventsPerFile),
		BufferSize:       envInt(envGCSBufferSize),
		Format:           format,
	}
	if v := os.Getenv(envGCSCredentialsJSON); v != "" {
		cfg.CredentialsJSON = []byte(v)
	}
	exp, err := NewGCS(ctx, cfg)
	if err != nil {
		return nil, err
	}
	authMode := "ADC"
	switch {
	case len(cfg.CredentialsJSON) > 0:
		authMode = "inline-json"
	case cfg.CredentialsFile != "":
		authMode = "file"
	}
	log.WithContext(ctx).Infof(
		"flow archive enabled: GCS bucket %q (auth=%s, format=%s)",
		bucket, authMode, displayFormat(format))
	return exp, nil
}

func newS3FromEnv(ctx context.Context) (store.Sink, error) {
	bucket := os.Getenv(envS3Bucket)
	if bucket == "" {
		return nil, nil
	}
	format := os.Getenv(envArchiveFormat)
	exp, err := NewS3(ctx, S3Config{
		Bucket:           bucket,
		Region:           os.Getenv(envS3Region),
		Endpoint:         os.Getenv(envS3Endpoint),
		AccessKey:        os.Getenv(envS3AccessKey),
		SecretKey:        os.Getenv(envS3SecretKey),
		Prefix:           os.Getenv(envS3Prefix),
		FlushInterval:    envDuration(envS3FlushInterval),
		MaxEventsPerFile: envInt(envS3MaxEventsPerFile),
		BufferSize:       envInt(envS3BufferSize),
		Format:           format,
	})
	if err != nil {
		return nil, err
	}
	log.WithContext(ctx).Infof(
		"flow archive enabled: S3-compatible bucket %q (endpoint=%s, format=%s)",
		bucket, displayEndpoint(os.Getenv(envS3Endpoint)), displayFormat(format))
	return exp, nil
}

// displayFormat normalizes the format string for log lines so the
// startup message reflects the actual format the sink will use,
// including the "ndjson" fallback for empty / unrecognised values.
func displayFormat(s string) string {
	switch s {
	case string(formatParquet):
		return string(formatParquet)
	default:
		return string(formatNDJSON)
	}
}

func displayEndpoint(s string) string {
	if s == "" {
		return "AWS S3"
	}
	return s
}

func newElasticFromEnv(ctx context.Context) (store.Sink, error) {
	url := os.Getenv(envElasticURL)
	if url == "" {
		return nil, nil
	}
	exp, err := NewElastic(ElasticConfig{
		URL:           url,
		Index:         os.Getenv(envElasticIndex),
		APIKey:        os.Getenv(envElasticAPIKey),
		Username:      os.Getenv(envElasticUsername),
		Password:      os.Getenv(envElasticPassword),
		BatchSize:     envInt(envElasticBatchSize),
		FlushInterval: envDuration(envElasticFlushInterval),
		BufferSize:    envInt(envElasticBufferSize),
	})
	if err != nil {
		return nil, err
	}
	log.WithContext(ctx).Infof("flow streaming enabled: Elastic Bulk → %s", url)
	return exp, nil
}

func envInt(name string) int {
	v := os.Getenv(name)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Warnf("flow sinks: ignoring invalid %s=%q: %v", name, v, err)
		return 0
	}
	return n
}

func envDuration(name string) time.Duration {
	v := os.Getenv(name)
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Warnf("flow sinks: ignoring invalid %s=%q: %v", name, v, err)
		return 0
	}
	return d
}
