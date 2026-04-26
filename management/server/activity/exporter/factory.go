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
	envURL            = "OPENZRO_ACTIVITY_EXPORT_URL"
	envHeadersJSON    = "OPENZRO_ACTIVITY_EXPORT_HEADERS"
	envTimeout        = "OPENZRO_ACTIVITY_EXPORT_TIMEOUT"
	envMaxAttempts    = "OPENZRO_ACTIVITY_EXPORT_MAX_ATTEMPTS"
	envInitialBackoff = "OPENZRO_ACTIVITY_EXPORT_INITIAL_BACKOFF"
)

// NewFromEnv constructs a slice of exporters from environment
// variables. Returns an empty slice (and nil error) when no exporter
// is configured — that is the default and means "no streaming".
//
// Today only one exporter is wired: HTTP webhook, configured via
// OPENZRO_ACTIVITY_EXPORT_URL. Adding a second exporter (e.g.,
// Datadog HEC) means a second URL/credentials env block here and a
// new file alongside http.go that implements Exporter.
func NewFromEnv(ctx context.Context) ([]Exporter, error) {
	url := os.Getenv(envURL)
	if url == "" {
		return nil, nil
	}

	headers, err := parseHeaders(os.Getenv(envHeadersJSON))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envHeadersJSON, err)
	}

	cfg := HTTPWebhookConfig{
		URL:            url,
		Headers:        headers,
		Timeout:        envDuration(envTimeout),
		MaxAttempts:    envInt(envMaxAttempts),
		InitialBackoff: envDuration(envInitialBackoff),
	}

	exp, err := NewHTTPWebhook(cfg)
	if err != nil {
		return nil, err
	}

	log.WithContext(ctx).Infof(
		"activity export enabled: HTTP webhook → %s", url)
	return []Exporter{exp}, nil
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
