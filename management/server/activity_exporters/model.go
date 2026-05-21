// Package activity_exporters persists per-account activity event
// streamers (audit log → SIEM) in the management's primary store,
// with credentials encrypted at rest.
//
// Two configuration layers coexist by design:
//
//  1. Process-wide env-var config (OPENZRO_ACTIVITY_EXPORT_*) — the
//     operator's baseline, applied to every account's events. Useful
//     for SaaS instances that ship one canonical pipeline regardless
//     of tenant.
//  2. Per-account DB rows managed via the dashboard. Each account's
//     events fan out to that account's configured exporters in
//     addition to the global baseline.
//
// This split mirrors how flow_exports works (env baseline + DB rows)
// and means a self-host operator can stay env-only without touching
// the DB, while a multi-tenant SaaS gets per-tenant streaming for free.
//
// Threat model: ConfigCipher protects credentials from a read-only DB
// dump — at runtime, every credential is decrypted into Go strings,
// so a memory dump still leaks. That trade-off matches flow_exports
// and the activity store; raising the bar requires a KMS, out of
// scope for self-host.
package activity_exporters

import (
	"time"
)

// ExporterType is the destination kind. Stable enum; do not renumber.
type ExporterType string

const (
	TypeHTTP    ExporterType = "http"
	TypeDatadog ExporterType = "datadog"
	TypeElastic ExporterType = "elastic"
)

// ActivityExporter is the GORM-managed row. The whole credential
// payload lives in ConfigCipher as encrypted JSON, never plaintext.
// PublicConfig holds the non-sensitive subset for API GETs; the UI
// shows what is configured without round-tripping the secret.
type ActivityExporter struct {
	ID        uint64       `gorm:"primaryKey;autoIncrement"`
	AccountID string       `gorm:"size:64;not null;index"`
	Name      string       `gorm:"size:128;not null"`
	Type      ExporterType `gorm:"size:32;not null;index"`
	Enabled   bool         `gorm:"not null;default:true"`

	// Template, when non-empty, is the Go text/template applied to the
	// activity event before sending. Empty means the exporter ships
	// its native default payload. Validated at save time via
	// exporter.ValidateTemplate so a misconfigured template never
	// reaches the live pipeline.
	Template string `gorm:"type:text"`

	// PublicConfig is the non-secret JSON projection of the per-type
	// config. Returned by the API; encoded by SaveInput.publicBlob.
	PublicConfig []byte `gorm:"type:bytea"`

	// ConfigCipher is the AES-GCM-encrypted JSON of the full config
	// (including credentials). Never returned by the API.
	ConfigCipher []byte `gorm:"type:bytea;not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (ActivityExporter) TableName() string { return "activity_exporters" }

// HTTPDestConfig is the operator-supplied config for the HTTP webhook
// activity exporter. Mirrors exporter.HTTPWebhookConfig but with
// time.Duration encoded as JSON-friendly milliseconds — ConfigCipher
// is JSON over the wire of the encryption envelope.
type HTTPDestConfig struct {
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers,omitempty"`
	TimeoutMs   int               `json:"timeout_ms,omitempty"`
	MaxAttempts int               `json:"max_attempts,omitempty"`
	BackoffMs   int               `json:"initial_backoff_ms,omitempty"`
}

func (c HTTPDestConfig) PublicView() HTTPDestConfigPublic {
	headerNames := make([]string, 0, len(c.Headers))
	for k := range c.Headers {
		headerNames = append(headerNames, k)
	}
	return HTTPDestConfigPublic{
		URL:         c.URL,
		HeaderNames: headerNames,
		TimeoutMs:   c.TimeoutMs,
		MaxAttempts: c.MaxAttempts,
		BackoffMs:   c.BackoffMs,
	}
}

type HTTPDestConfigPublic struct {
	URL         string   `json:"url"`
	HeaderNames []string `json:"header_names,omitempty"`
	TimeoutMs   int      `json:"timeout_ms,omitempty"`
	MaxAttempts int      `json:"max_attempts,omitempty"`
	BackoffMs   int      `json:"initial_backoff_ms,omitempty"`
}

// DatadogDestConfig configures the Datadog Logs Intake exporter.
// See exporter.DatadogConfig for field semantics.
type DatadogDestConfig struct {
	Site       string `json:"site,omitempty"`
	URL        string `json:"url,omitempty"`
	APIKey     string `json:"api_key,omitempty"`
	Service    string `json:"service,omitempty"`
	Source     string `json:"source,omitempty"`
	Tags       string `json:"tags,omitempty"`
	Hostname   string `json:"hostname,omitempty"`
	BatchSize  int    `json:"batch_size,omitempty"`
	FlushMs    int    `json:"flush_interval_ms,omitempty"`
	BufferSize int    `json:"buffer_size,omitempty"`
}

func (c DatadogDestConfig) PublicView() DatadogDestConfigPublic {
	return DatadogDestConfigPublic{
		Site:       c.Site,
		URL:        c.URL,
		HasAPIKey:  c.APIKey != "",
		Service:    c.Service,
		Source:     c.Source,
		Tags:       c.Tags,
		Hostname:   c.Hostname,
		BatchSize:  c.BatchSize,
		FlushMs:    c.FlushMs,
		BufferSize: c.BufferSize,
	}
}

type DatadogDestConfigPublic struct {
	Site       string `json:"site,omitempty"`
	URL        string `json:"url,omitempty"`
	HasAPIKey  bool   `json:"has_api_key"`
	Service    string `json:"service,omitempty"`
	Source     string `json:"source,omitempty"`
	Tags       string `json:"tags,omitempty"`
	Hostname   string `json:"hostname,omitempty"`
	BatchSize  int    `json:"batch_size,omitempty"`
	FlushMs    int    `json:"flush_interval_ms,omitempty"`
	BufferSize int    `json:"buffer_size,omitempty"`
}

// ElasticDestConfig configures the Elasticsearch SIEM exporter.
type ElasticDestConfig struct {
	URL        string `json:"url"`
	Index      string `json:"index,omitempty"`
	APIKey     string `json:"api_key,omitempty"`
	Username   string `json:"username,omitempty"`
	Password   string `json:"password,omitempty"`
	BatchSize  int    `json:"batch_size,omitempty"`
	FlushMs    int    `json:"flush_interval_ms,omitempty"`
	BufferSize int    `json:"buffer_size,omitempty"`
}

func (c ElasticDestConfig) PublicView() ElasticDestConfigPublic {
	authMode := ""
	if c.APIKey != "" {
		authMode = "api_key"
	} else if c.Username != "" {
		authMode = "basic"
	}
	return ElasticDestConfigPublic{
		URL:        c.URL,
		Index:      c.Index,
		AuthMode:   authMode,
		BatchSize:  c.BatchSize,
		FlushMs:    c.FlushMs,
		BufferSize: c.BufferSize,
	}
}

type ElasticDestConfigPublic struct {
	URL        string `json:"url"`
	Index      string `json:"index,omitempty"`
	AuthMode   string `json:"auth_mode,omitempty"`
	BatchSize  int    `json:"batch_size,omitempty"`
	FlushMs    int    `json:"flush_interval_ms,omitempty"`
	BufferSize int    `json:"buffer_size,omitempty"`
}
