// Package flow_exports persists operator-configured flow event
// destinations (SIEM streams, S3 archives, generic HTTP webhooks) in
// the management's primary store, with sensitive fields encrypted at
// rest using the existing DataStoreEncryptionKey.
//
// This package is the runtime alternative to the env-var-driven
// configuration in flow/sinks/factory.go. Both can coexist: env vars
// provide the boot-time baseline (useful for ops/CI), while the DB
// persists any additions an admin makes through the dashboard UI.
// The flow_exports.Manager merges both at process start and on every
// settings change.
//
// Threat model: the encrypted column protects credentials from a
// read-only DB compromise. It does not protect against a process
// memory dump — at runtime, every credential is decrypted into Go
// strings. That trade-off matches the activity log's existing
// envelope; raising the bar further requires a KMS, which is
// out of scope for self-host.
package flow_exports

import (
	"time"
)

// ExportType is the destination kind. Stable enum; do not renumber.
type ExportType string

const (
	TypeElastic ExportType = "elastic"
	TypeS3      ExportType = "s3"
	TypeHTTP    ExportType = "http"
	TypeDatadog ExportType = "datadog"
	TypeGCS     ExportType = "gcs"
)

// FlowExport is the GORM-managed row. The whole credential payload
// lives in ConfigCipher as encrypted JSON, never in plaintext columns.
// PublicConfig holds the non-sensitive subset that we render back to
// the API for display (e.g. URL, bucket name) — this lets the UI show
// what's configured without round-tripping the secret.
type FlowExport struct {
	ID      uint64     `gorm:"primaryKey;autoIncrement"`
	Name    string     `gorm:"size:128;not null"`
	Type    ExportType `gorm:"size:32;not null;index"`
	Enabled bool       `gorm:"not null;default:true"`

	// PublicConfig is the non-secret JSON view of the destination —
	// safe to return to API callers. Concrete shape depends on Type.
	PublicConfig []byte `gorm:"type:bytea"`

	// ConfigCipher is the AES-GCM-encrypted JSON of the full config
	// (including credentials). Never returned by the API.
	ConfigCipher []byte `gorm:"type:bytea;not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (FlowExport) TableName() string { return "flow_exports" }

// ElasticDestConfig is the full Elastic configuration as stored
// (encrypted) in ConfigCipher. The Public projection of this struct
// (URL, Index, BatchSize, FlushInterval) goes in PublicConfig.
type ElasticDestConfig struct {
	URL           string        `json:"url"`
	Index         string        `json:"index,omitempty"`
	APIKey        string        `json:"api_key,omitempty"`
	Username      string        `json:"username,omitempty"`
	Password      string        `json:"password,omitempty"`
	BatchSize     int           `json:"batch_size,omitempty"`
	FlushInterval time.Duration `json:"flush_interval,omitempty"`
	BufferSize    int           `json:"buffer_size,omitempty"`
}

// PublicView returns the safe subset for API responses. APIKey,
// Password are NEVER included.
func (c ElasticDestConfig) PublicView() ElasticDestConfigPublic {
	authMode := ""
	if c.APIKey != "" {
		authMode = "api_key"
	} else if c.Username != "" {
		authMode = "basic"
	}
	return ElasticDestConfigPublic{
		URL:           c.URL,
		Index:         c.Index,
		AuthMode:      authMode,
		BatchSize:     c.BatchSize,
		FlushInterval: c.FlushInterval,
		BufferSize:    c.BufferSize,
	}
}

// ElasticDestConfigPublic is what the API returns. Authentication
// material is reduced to a mode string — the caller knows what kind
// of credentials are configured but cannot read them.
type ElasticDestConfigPublic struct {
	URL           string        `json:"url"`
	Index         string        `json:"index,omitempty"`
	AuthMode      string        `json:"auth_mode,omitempty"`
	BatchSize     int           `json:"batch_size,omitempty"`
	FlushInterval time.Duration `json:"flush_interval,omitempty"`
	BufferSize    int           `json:"buffer_size,omitempty"`
}

// S3DestConfig mirrors flow/sinks.S3Config plus a couple of fields
// used only at the management layer.
type S3DestConfig struct {
	Bucket           string        `json:"bucket"`
	Region           string        `json:"region,omitempty"`
	Endpoint         string        `json:"endpoint,omitempty"`
	AccessKey        string        `json:"access_key,omitempty"`
	SecretKey        string        `json:"secret_key,omitempty"`
	Prefix           string        `json:"prefix,omitempty"`
	FlushInterval    time.Duration `json:"flush_interval,omitempty"`
	MaxEventsPerFile int           `json:"max_events_per_file,omitempty"`
	BufferSize       int           `json:"buffer_size,omitempty"`
	// Format selects the file format: "parquet" or "ndjson". Empty
	// inherits the OPENZRO_FLOW_ARCHIVE_FORMAT env var (operator-level
	// default — see flow_exports/manager.go::archiveFormatFor). Older
	// rows persisted before this field existed deserialize with
	// Format="" and pick up the env default transparently.
	Format string `json:"format,omitempty"`
}

func (c S3DestConfig) PublicView() S3DestConfigPublic {
	return S3DestConfigPublic{
		Bucket:           c.Bucket,
		Region:           c.Region,
		Endpoint:         c.Endpoint,
		HasCredentials:   c.AccessKey != "",
		Prefix:           c.Prefix,
		FlushInterval:    c.FlushInterval,
		MaxEventsPerFile: c.MaxEventsPerFile,
		BufferSize:       c.BufferSize,
	}
}

type S3DestConfigPublic struct {
	Bucket           string        `json:"bucket"`
	Region           string        `json:"region,omitempty"`
	Endpoint         string        `json:"endpoint,omitempty"`
	HasCredentials   bool          `json:"has_credentials"`
	Prefix           string        `json:"prefix,omitempty"`
	FlushInterval    time.Duration `json:"flush_interval,omitempty"`
	MaxEventsPerFile int           `json:"max_events_per_file,omitempty"`
	BufferSize       int           `json:"buffer_size,omitempty"`
}

// HTTPDestConfig is the generic webhook config — same shape as the
// activity HTTPWebhookConfig but stored at the management layer.
type HTTPDestConfig struct {
	URL            string            `json:"url"`
	Headers        map[string]string `json:"headers,omitempty"`
	Timeout        time.Duration     `json:"timeout,omitempty"`
	MaxAttempts    int               `json:"max_attempts,omitempty"`
	InitialBackoff time.Duration     `json:"initial_backoff,omitempty"`
}

func (c HTTPDestConfig) PublicView() HTTPDestConfigPublic {
	headerNames := make([]string, 0, len(c.Headers))
	for k := range c.Headers {
		headerNames = append(headerNames, k)
	}
	return HTTPDestConfigPublic{
		URL:            c.URL,
		HeaderNames:    headerNames,
		Timeout:        c.Timeout,
		MaxAttempts:    c.MaxAttempts,
		InitialBackoff: c.InitialBackoff,
	}
}

type HTTPDestConfigPublic struct {
	URL            string        `json:"url"`
	HeaderNames    []string      `json:"header_names,omitempty"`
	Timeout        time.Duration `json:"timeout,omitempty"`
	MaxAttempts    int           `json:"max_attempts,omitempty"`
	InitialBackoff time.Duration `json:"initial_backoff,omitempty"`
}

// DatadogDestConfig is the full Datadog Logs Intake config as stored
// (encrypted) in ConfigCipher. The Public projection drops APIKey;
// HasAPIKey reports presence.
type DatadogDestConfig struct {
	Site          string        `json:"site,omitempty"`
	URL           string        `json:"url,omitempty"`
	APIKey        string        `json:"api_key,omitempty"`
	Service       string        `json:"service,omitempty"`
	Source        string        `json:"source,omitempty"`
	Tags          string        `json:"tags,omitempty"`
	BatchSize     int           `json:"batch_size,omitempty"`
	FlushInterval time.Duration `json:"flush_interval,omitempty"`
	BufferSize    int           `json:"buffer_size,omitempty"`
}

func (c DatadogDestConfig) PublicView() DatadogDestConfigPublic {
	return DatadogDestConfigPublic{
		Site:          c.Site,
		URL:           c.URL,
		HasAPIKey:     c.APIKey != "",
		Service:       c.Service,
		Source:        c.Source,
		Tags:          c.Tags,
		BatchSize:     c.BatchSize,
		FlushInterval: c.FlushInterval,
		BufferSize:    c.BufferSize,
	}
}

type DatadogDestConfigPublic struct {
	Site          string        `json:"site,omitempty"`
	URL           string        `json:"url,omitempty"`
	HasAPIKey     bool          `json:"has_api_key"`
	Service       string        `json:"service,omitempty"`
	Source        string        `json:"source,omitempty"`
	Tags          string        `json:"tags,omitempty"`
	BatchSize     int           `json:"batch_size,omitempty"`
	FlushInterval time.Duration `json:"flush_interval,omitempty"`
	BufferSize    int           `json:"buffer_size,omitempty"`
}

// GCSDestConfig configures the native Google Cloud Storage sink.
// Distinct from S3 mode: this uses Google's SDK and authenticates
// via Application Default Credentials, a Service Account JSON
// path, or inline JSON. See flow/sinks/gcs.go.
type GCSDestConfig struct {
	Bucket           string        `json:"bucket"`
	Prefix           string        `json:"prefix,omitempty"`
	CredentialsJSON  string        `json:"credentials_json,omitempty"`
	CredentialsFile  string        `json:"credentials_file,omitempty"`
	ProjectID        string        `json:"project_id,omitempty"`
	Endpoint         string        `json:"endpoint,omitempty"`
	FlushInterval    time.Duration `json:"flush_interval,omitempty"`
	MaxEventsPerFile int           `json:"max_events_per_file,omitempty"`
	BufferSize       int           `json:"buffer_size,omitempty"`
	// Format selects the file format: "parquet" or "ndjson". Empty
	// inherits the OPENZRO_FLOW_ARCHIVE_FORMAT env var (operator-level
	// default — see flow_exports/manager.go::archiveFormatFor). Older
	// rows persisted before this field existed deserialize with
	// Format="" and pick up the env default transparently.
	Format string `json:"format,omitempty"`
}

func (c GCSDestConfig) PublicView() GCSDestConfigPublic {
	authMode := "adc"
	switch {
	case c.CredentialsJSON != "":
		authMode = "inline-json"
	case c.CredentialsFile != "":
		authMode = "file"
	}
	return GCSDestConfigPublic{
		Bucket:           c.Bucket,
		Prefix:           c.Prefix,
		AuthMode:         authMode,
		ProjectID:        c.ProjectID,
		Endpoint:         c.Endpoint,
		FlushInterval:    c.FlushInterval,
		MaxEventsPerFile: c.MaxEventsPerFile,
		BufferSize:       c.BufferSize,
	}
}

type GCSDestConfigPublic struct {
	Bucket           string        `json:"bucket"`
	Prefix           string        `json:"prefix,omitempty"`
	AuthMode         string        `json:"auth_mode"`
	ProjectID        string        `json:"project_id,omitempty"`
	Endpoint         string        `json:"endpoint,omitempty"`
	FlushInterval    time.Duration `json:"flush_interval,omitempty"`
	MaxEventsPerFile int           `json:"max_events_per_file,omitempty"`
	BufferSize       int           `json:"buffer_size,omitempty"`
}
