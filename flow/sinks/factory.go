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

	return out, nil
}

func newS3FromEnv(ctx context.Context) (store.Sink, error) {
	bucket := os.Getenv(envS3Bucket)
	if bucket == "" {
		return nil, nil
	}
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
	})
	if err != nil {
		return nil, err
	}
	log.WithContext(ctx).Infof(
		"flow archive enabled: S3-compatible bucket %q (endpoint=%s)",
		bucket, displayEndpoint(os.Getenv(envS3Endpoint)))
	return exp, nil
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
