package exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
)

// Environment variable names. Stable; do not rename without a release
// note — operators bake these into deployment configs.
const (
	// Generic HTTP webhook
	envURL            = "OPENZRO_ACTIVITY_EXPORT_URL"
	envHeadersJSON    = "OPENZRO_ACTIVITY_EXPORT_HEADERS"
	envTimeout        = "OPENZRO_ACTIVITY_EXPORT_TIMEOUT"
	envMaxAttempts    = "OPENZRO_ACTIVITY_EXPORT_MAX_ATTEMPTS"
	envInitialBackoff = "OPENZRO_ACTIVITY_EXPORT_INITIAL_BACKOFF"

	// Elastic SIEM (Bulk API + ECS shape)
	envElasticURL           = "OPENZRO_ACTIVITY_EXPORT_ELASTIC_URL"
	envElasticIndex         = "OPENZRO_ACTIVITY_EXPORT_ELASTIC_INDEX"
	envElasticAPIKey        = "OPENZRO_ACTIVITY_EXPORT_ELASTIC_API_KEY"
	envElasticUsername      = "OPENZRO_ACTIVITY_EXPORT_ELASTIC_USERNAME"
	envElasticPassword      = "OPENZRO_ACTIVITY_EXPORT_ELASTIC_PASSWORD"
	envElasticBatchSize     = "OPENZRO_ACTIVITY_EXPORT_ELASTIC_BATCH_SIZE"
	envElasticFlushInterval = "OPENZRO_ACTIVITY_EXPORT_ELASTIC_FLUSH_INTERVAL"
	envElasticBufferSize    = "OPENZRO_ACTIVITY_EXPORT_ELASTIC_BUFFER_SIZE"
)

// NewFromEnv constructs the configured set of exporters from
// environment variables. Returns an empty slice (and nil error) when
// nothing is configured — that is the default and means "no
// streaming".
//
// Multiple exporters can run simultaneously (e.g. Generic HTTP +
// Elastic). Each is independent: failures in one never stall another.
func NewFromEnv(ctx context.Context) ([]Exporter, error) {
	out := []Exporter{}

	if exp, err := newHTTPWebhookFromEnv(ctx); err != nil {
		return nil, err
	} else if exp != nil {
		out = append(out, exp)
	}

	if exp, err := newElasticFromEnv(ctx); err != nil {
		return nil, err
	} else if exp != nil {
		out = append(out, exp)
	}

	return out, nil
}

func newHTTPWebhookFromEnv(ctx context.Context) (Exporter, error) {
	url := os.Getenv(envURL)
	if url == "" {
		return nil, nil
	}
	headers, err := parseHeaders(os.Getenv(envHeadersJSON))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envHeadersJSON, err)
	}
	exp, err := NewHTTPWebhook(HTTPWebhookConfig{
		URL:            url,
		Headers:        headers,
		Timeout:        envDuration(envTimeout),
		MaxAttempts:    envInt(envMaxAttempts),
		InitialBackoff: envDuration(envInitialBackoff),
	})
	if err != nil {
		return nil, err
	}
	log.WithContext(ctx).Infof("activity export enabled: HTTP webhook → %s", url)
	return exp, nil
}

func newElasticFromEnv(ctx context.Context) (Exporter, error) {
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
	log.WithContext(ctx).Infof("activity export enabled: Elastic Bulk → %s", url)
	return exp, nil
}

func parseHeaders(s string) (map[string]string, error) {
	if s == "" {
		return nil, nil
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return out, nil
}

func envDuration(name string) time.Duration {
	v := os.Getenv(name)
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Warnf("activity exporter: ignoring invalid %s=%q: %v", name, v, err)
		return 0
	}
	return d
}

func envInt(name string) int {
	v := os.Getenv(name)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Warnf("activity exporter: ignoring invalid %s=%q: %v", name, v, err)
		return 0
	}
	return n
}
