package activity_exporters

import (
	"context"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/activity/exporter"
)

// Manager owns the live per-account exporter set built from DB rows
// and exposes ExportEvent for StoreEvent to fan out into. The
// process-wide env-var exporters (factory.NewFromEnv) keep flowing
// through the legacy event.go path; this package only adds the
// dynamic, per-tenant layer on top.
type Manager struct {
	store *Store

	mu        sync.RWMutex
	byAccount map[string][]activeExporter // accountID → live instances
}

// activeExporter is one live Exporter plus the row metadata we need
// to log meaningful errors. Closed on Refresh + Stop.
type activeExporter struct {
	id       uint64
	name     string
	exporter exporter.Exporter
}

// NewManager constructs a Manager and immediately runs Refresh so
// every active row is materialized. Errors building a single row are
// logged at Error and the row is skipped — partial application is
// preferred to a total outage.
func NewManager(ctx context.Context, store *Store) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("activity_exporters: store is required")
	}
	m := &Manager{
		store:     store,
		byAccount: map[string][]activeExporter{},
	}
	if err := m.Refresh(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

// Refresh rebuilds the in-memory exporter set from the DB. Called at
// startup and after every CRUD mutation. The previous instances are
// closed AFTER the new ones are in place so there is never an empty
// window mid-swap.
func (m *Manager) Refresh(ctx context.Context) error {
	rows, err := m.store.List(ctx, "")
	if err != nil {
		return fmt.Errorf("activity_exporters: list: %w", err)
	}
	next := map[string][]activeExporter{}
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		exp, err := m.build(&row)
		if err != nil {
			log.WithContext(ctx).Errorf(
				"activity_exporters: skipping #%d (%s/%s) for account %s: %v",
				row.ID, row.Type, row.Name, row.AccountID, err)
			continue
		}
		next[row.AccountID] = append(next[row.AccountID], activeExporter{
			id:       row.ID,
			name:     row.Name,
			exporter: exp,
		})
	}

	m.mu.Lock()
	old := m.byAccount
	m.byAccount = next
	m.mu.Unlock()

	for _, list := range old {
		for _, ae := range list {
			_ = ae.exporter.Close()
		}
	}
	return nil
}

// ExportEvent fans the event out to every active per-account
// exporter for ev.AccountID. Best-effort: failures inside individual
// exporters are logged and never bubble up.
func (m *Manager) ExportEvent(ctx context.Context, ev *activity.Event) {
	if ev == nil {
		return
	}
	m.mu.RLock()
	list := m.byAccount[ev.AccountID]
	m.mu.RUnlock()
	for _, ae := range list {
		if err := ae.exporter.Export(ctx, ev); err != nil {
			log.WithContext(ctx).Errorf(
				"activity exporter %s (#%d/%s): %v",
				ae.exporter.Name(), ae.id, ae.name, err)
		}
	}
}

// Stop releases every active exporter. Safe to call multiple times.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, list := range m.byAccount {
		for _, ae := range list {
			_ = ae.exporter.Close()
		}
	}
	m.byAccount = map[string][]activeExporter{}
}

// build constructs an exporter.Exporter from a row. The credential
// blob is decrypted here — the API layer never sees it.
func (m *Manager) build(row *ActivityExporter) (exporter.Exporter, error) {
	plain, err := m.store.Decrypt(row)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	var tmpl *exporter.PayloadTemplate
	if row.Template != "" {
		tmpl, err = exporter.NewPayloadTemplate(row.Template)
		if err != nil {
			return nil, fmt.Errorf("template: %w", err)
		}
	}
	switch row.Type {
	case TypeHTTP:
		c := plain.(*HTTPDestConfig)
		return exporter.NewHTTPWebhook(exporter.HTTPWebhookConfig{
			URL:            c.URL,
			Headers:        c.Headers,
			Timeout:        time.Duration(c.TimeoutMs) * time.Millisecond,
			MaxAttempts:    c.MaxAttempts,
			InitialBackoff: time.Duration(c.BackoffMs) * time.Millisecond,
			Template:       tmpl,
		})
	case TypeDatadog:
		c := plain.(*DatadogDestConfig)
		return exporter.NewDatadog(exporter.DatadogConfig{
			Site:          c.Site,
			URL:           c.URL,
			APIKey:        c.APIKey,
			Service:       c.Service,
			Source:        c.Source,
			Tags:          c.Tags,
			Hostname:      c.Hostname,
			BatchSize:     c.BatchSize,
			FlushInterval: time.Duration(c.FlushMs) * time.Millisecond,
			BufferSize:    c.BufferSize,
		})
	case TypeElastic:
		c := plain.(*ElasticDestConfig)
		return exporter.NewElastic(exporter.ElasticConfig{
			URL:           c.URL,
			Index:         c.Index,
			APIKey:        c.APIKey,
			Username:      c.Username,
			Password:      c.Password,
			BatchSize:     c.BatchSize,
			FlushInterval: time.Duration(c.FlushMs) * time.Millisecond,
			BufferSize:    c.BufferSize,
		})
	}
	return nil, fmt.Errorf("unsupported type %q", row.Type)
}
