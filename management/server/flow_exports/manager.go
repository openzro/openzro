package flow_exports

import (
	"context"
	"fmt"
	"os"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/flow/sinks"
	"github.com/openzro/openzro/flow/store"
)

// envArchiveFormat mirrors the same env var the boot-time sinks
// factory reads (flow/sinks/factory.go). Dashboard-configured GCS
// and S3 exports inherit the operator-level default unless they
// override it on the row itself — see archiveFormatFor.
const envArchiveFormat = "OPENZRO_FLOW_ARCHIVE_FORMAT"

// archiveFormatFor returns the format string to pass to the sink
// constructor for a dashboard-configured GCS / S3 export. Row-level
// overrides win when set; otherwise we fall through to the
// OPENZRO_FLOW_ARCHIVE_FORMAT env var (the same operator-level knob
// the boot-time sinks already honour); empty fallback lets the sink
// itself default to ndjson as it has historically.
func archiveFormatFor(rowFormat string) string {
	if rowFormat != "" {
		return rowFormat
	}
	return os.Getenv(envArchiveFormat)
}

// Manager glues persisted exports to the running FlowService. It
// owns:
//
//   - the env-var-derived baseline (the Sinks the operator configured
//     via OPENZRO_FLOW_EXPORT_* / OPENZRO_FLOW_ARCHIVE_* before the
//     process started)
//   - the store-derived dynamic set (the rows admins create through
//     the dashboard)
//   - the optional hot store (the queryable flow.Store from
//     flow/store/factory)
//
// Whenever the dynamic set changes, the Manager rebuilds the full
// destination list and hands it to the FlowService via SetSinks.
//
// The Manager is the only place that decrypts ConfigCipher and
// constructs Sinks from it; the API layer only ever sees the
// public (sanitized) shape.
type Manager struct {
	store        *Store
	flowSvc      sinkSetter
	hotStore     store.Sink   // queryable hot tier (optional)
	envSinks     []store.Sink // baseline from env vars (immutable)
	mu           sync.Mutex
	dynamicSinks []store.Sink // sinks built from DB rows; closed on rebuild
}

// sinkSetter is the slice of *FlowService that Manager actually
// uses. Defining the interface here lets us unit-test Manager
// without spinning up a gRPC server.
type sinkSetter interface {
	SetSinks([]store.Sink)
}

// NewManager constructs a Manager and immediately runs ApplyAll so
// the FlowService starts with the union of env baseline + persisted
// rows. Errors loading individual rows are logged and the row is
// skipped — a single malformed row never takes the whole pipeline
// down.
func NewManager(
	ctx context.Context,
	cfgStore *Store,
	flowSvc sinkSetter,
	hotStore store.Sink,
	envSinks []store.Sink,
) (*Manager, error) {
	if cfgStore == nil {
		return nil, fmt.Errorf("flow_exports: config store is required")
	}
	if flowSvc == nil {
		return nil, fmt.Errorf("flow_exports: FlowService is required")
	}
	m := &Manager{
		store:    cfgStore,
		flowSvc:  flowSvc,
		hotStore: hotStore,
		envSinks: envSinks,
	}
	if err := m.ApplyAll(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

// ApplyAll rebuilds the dynamic Sinks from every Enabled row, merges
// with the env baseline + hot store, and hands the union to the
// FlowService. Called at startup and after every Save/Delete.
//
// On error from a single row (decrypt failure, malformed config),
// the row is logged and skipped — partial application is preferred
// to a total outage.
func (m *Manager) ApplyAll(ctx context.Context) error {
	rows, err := m.store.List(ctx)
	if err != nil {
		return fmt.Errorf("flow_exports: list: %w", err)
	}

	next := []store.Sink{}
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		sink, err := m.buildSink(ctx, &row)
		if err != nil {
			log.WithContext(ctx).Errorf(
				"flow_exports: skipping export #%d (%s/%s): %v",
				row.ID, row.Type, row.Name, err)
			continue
		}
		next = append(next, sink)
	}

	m.mu.Lock()
	old := m.dynamicSinks
	m.dynamicSinks = next
	m.mu.Unlock()

	merged := m.merged()
	m.flowSvc.SetSinks(merged)

	// Close the previous dynamic Sinks AFTER the new set is in place
	// so we never have an empty period mid-swap.
	for _, sink := range old {
		_ = sink.Close()
	}
	return nil
}

// merged returns env baseline + hot store + dynamic Sinks. The order
// matters only for log output; functionally all sinks are independent.
func (m *Manager) merged() []store.Sink {
	out := make([]store.Sink, 0, 1+len(m.envSinks)+len(m.dynamicSinks))
	if m.hotStore != nil {
		out = append(out, m.hotStore)
	}
	out = append(out, m.envSinks...)
	m.mu.Lock()
	out = append(out, m.dynamicSinks...)
	m.mu.Unlock()
	return out
}

// buildSink decrypts a row's config and constructs the right Sink.
// Reuses the same constructors that the env-var factory uses, so a
// row created via the API is bit-identical to one configured via env.
func (m *Manager) buildSink(ctx context.Context, row *FlowExport) (store.Sink, error) {
	plain, err := m.store.Decrypt(row)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	switch row.Type {
	case TypeElastic:
		c := plain.(*ElasticDestConfig)
		return sinks.NewElastic(sinks.ElasticConfig{
			URL:           c.URL,
			Index:         c.Index,
			APIKey:        c.APIKey,
			Username:      c.Username,
			Password:      c.Password,
			BatchSize:     c.BatchSize,
			FlushInterval: c.FlushInterval,
			BufferSize:    c.BufferSize,
		})

	case TypeS3:
		c := plain.(*S3DestConfig)
		return sinks.NewS3(ctx, sinks.S3Config{
			Bucket:           c.Bucket,
			Region:           c.Region,
			Endpoint:         c.Endpoint,
			AccessKey:        c.AccessKey,
			SecretKey:        c.SecretKey,
			Prefix:           c.Prefix,
			FlushInterval:    c.FlushInterval,
			MaxEventsPerFile: c.MaxEventsPerFile,
			BufferSize:       c.BufferSize,
			Format:           archiveFormatFor(c.Format),
		})

	case TypeHTTP:
		c := plain.(*HTTPDestConfig)
		return sinks.NewHTTP(sinks.HTTPConfig{
			URL:            c.URL,
			Headers:        c.Headers,
			Timeout:        c.Timeout,
			MaxAttempts:    c.MaxAttempts,
			InitialBackoff: c.InitialBackoff,
		})

	case TypeDatadog:
		c := plain.(*DatadogDestConfig)
		return sinks.NewDatadog(sinks.DatadogConfig{
			Site:          c.Site,
			URL:           c.URL,
			APIKey:        c.APIKey,
			Service:       c.Service,
			Source:        c.Source,
			Tags:          c.Tags,
			BatchSize:     c.BatchSize,
			FlushInterval: c.FlushInterval,
			BufferSize:    c.BufferSize,
		})

	case TypeGCS:
		c := plain.(*GCSDestConfig)
		gcsCfg := sinks.GCSConfig{
			Bucket:           c.Bucket,
			Prefix:           c.Prefix,
			CredentialsFile:  c.CredentialsFile,
			ProjectID:        c.ProjectID,
			Endpoint:         c.Endpoint,
			FlushInterval:    c.FlushInterval,
			MaxEventsPerFile: c.MaxEventsPerFile,
			BufferSize:       c.BufferSize,
			Format:           archiveFormatFor(c.Format),
		}
		if c.CredentialsJSON != "" {
			gcsCfg.CredentialsJSON = []byte(c.CredentialsJSON)
		}
		return sinks.NewGCS(ctx, gcsCfg)
	}
	return nil, fmt.Errorf("unsupported type %q", row.Type)
}
