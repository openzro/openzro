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

	return out, nil
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
