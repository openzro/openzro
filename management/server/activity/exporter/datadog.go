package exporter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/safedial"
)

// DatadogConfig configures the Datadog Logs Intake exporter.
//
// Datadog's Logs Intake API (https://docs.datadoghq.com/api/latest/logs/)
// accepts a JSON array of log entries per request, signed with a
// DD-API-KEY header. Region matters because every Datadog tenant is
// pinned to one site (US1, EU1, US3, US5, AP1) and writing to the
// wrong site silently 401s. Pick the preset that matches the URL the
// operator sees in their Datadog UI.
type DatadogConfig struct {
	// Site selects the destination region by preset. Recognized values
	// match Datadog's site identifiers:
	//
	//   us1  → http-intake.logs.datadoghq.com    (datadoghq.com)
	//   us3  → http-intake.logs.us3.datadoghq.com
	//   us5  → http-intake.logs.us5.datadoghq.com
	//   eu1  → http-intake.logs.datadoghq.eu     (datadoghq.eu)
	//   ap1  → http-intake.logs.ap1.datadoghq.com
	//
	// Empty defaults to "us1". Ignored when URL is set explicitly.
	Site string

	// URL overrides the site preset. Use only when proxying through
	// an internal log forwarder or hitting Datadog's preview regions.
	// Example: https://http-intake.logs.datadoghq.com (without path —
	// the exporter appends /api/v2/logs).
	URL string

	// APIKey is the Datadog API key. Required. Send-only credential —
	// it is never logged. Generate at
	// https://app.datadoghq.com/organization-settings/api-keys.
	APIKey string

	// Service tags every event with the same service name. Defaults to
	// "openzro". Shows up as `service:` in the Datadog UI.
	Service string

	// Source is the Datadog `ddsource` field — a hint to the parser.
	// Defaults to "openzro". Don't change unless you have a custom
	// pipeline keyed off it.
	Source string

	// Tags are appended to every event's `ddtags`. Format:
	// "env:prod,team:security". Optional; useful for filtering by env.
	Tags string

	// Hostname is the value of the `hostname` field. Defaults to the
	// initiator's account ID, which usefully separates tenants in
	// Datadog when one Datadog account ingests from multiple openZro
	// instances. Leave empty for the default.
	Hostname string

	// BatchSize bounds the events per HTTP request. Datadog's limit is
	// 1000 logs per request. Default 100.
	BatchSize int

	// FlushInterval bounds staleness. Default 5s.
	FlushInterval time.Duration

	// BufferSize is the in-memory queue capacity. Default 10000.
	BufferSize int

	// Timeout per HTTP attempt. Default 10s.
	Timeout time.Duration

	// HTTPClient overrides the default. Test seam.
	HTTPClient *http.Client
}

// Datadog ships activity events to Datadog Logs Intake.
//
// Performance shape: identical to the Elastic exporter — Export() is
// non-blocking, a background goroutine drains a buffered channel,
// accumulates up to BatchSize events or until FlushInterval elapses,
// and POSTs the batch as a single JSON array. Per-event POSTs would
// burn Datadog rate limits at any real volume.
type Datadog struct {
	cfg    DatadogConfig
	client *http.Client
	intake string
	queue  chan *activity.Event
	wg     sync.WaitGroup
	stopCh chan struct{}
	closed sync.Once
}

// datadogSiteToHost maps Datadog region presets to their logs intake
// hostname. Sourced from Datadog's "Sites" docs; stable mapping.
var datadogSiteToHost = map[string]string{
	"us1": "http-intake.logs.datadoghq.com",
	"us3": "http-intake.logs.us3.datadoghq.com",
	"us5": "http-intake.logs.us5.datadoghq.com",
	"eu1": "http-intake.logs.datadoghq.eu",
	"ap1": "http-intake.logs.ap1.datadoghq.com",
}

// NewDatadog builds and starts a Datadog exporter. Returns an error if
// APIKey is empty or Site is unrecognized.
func NewDatadog(cfg DatadogConfig) (*Datadog, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("exporter: Datadog API key is required")
	}
	intake, err := resolveDatadogIntake(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Service == "" {
		cfg.Service = "openzro"
	}
	if cfg.Source == "" {
		cfg.Source = "openzro"
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.BatchSize > 1000 {
		// Datadog hard limit. Floor instead of error so an enthusiastic
		// operator value does not crash the exporter at boot.
		cfg.BatchSize = 1000
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 10000
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	client := cfg.HTTPClient
	if client == nil {
		client = safedial.Client(cfg.Timeout)
	}

	d := &Datadog{
		cfg:    cfg,
		client: client,
		intake: intake,
		queue:  make(chan *activity.Event, cfg.BufferSize),
		stopCh: make(chan struct{}),
	}
	d.wg.Add(1)
	go d.loop()
	return d, nil
}

func resolveDatadogIntake(cfg DatadogConfig) (string, error) {
	if cfg.URL != "" {
		return strings.TrimRight(cfg.URL, "/") + "/api/v2/logs", nil
	}
	site := cfg.Site
	if site == "" {
		site = "us1"
	}
	host, ok := datadogSiteToHost[strings.ToLower(site)]
	if !ok {
		return "", fmt.Errorf("exporter: unknown Datadog site %q (valid: us1, us3, us5, eu1, ap1)", cfg.Site)
	}
	return "https://" + host + "/api/v2/logs", nil
}

// Name returns the stable identifier used in log lines.
func (d *Datadog) Name() string { return "datadog" }

// Export non-blockingly enqueues the event.
func (d *Datadog) Export(_ context.Context, event *activity.Event) error {
	if event == nil {
		return nil
	}
	select {
	case d.queue <- event:
		return nil
	default:
		err := fmt.Errorf("datadog queue full (size=%d), dropping event", d.cfg.BufferSize)
		log.Error(err)
		return err
	}
}

// Close stops the background loop and flushes whatever is buffered.
func (d *Datadog) Close() error {
	d.closed.Do(func() {
		close(d.stopCh)
		d.wg.Wait()
	})
	return nil
}

func (d *Datadog) loop() {
	defer d.wg.Done()
	ticker := time.NewTicker(d.cfg.FlushInterval)
	defer ticker.Stop()

	batch := make([]*activity.Event, 0, d.cfg.BatchSize)
	for {
		select {
		case <-d.stopCh:
			for {
				select {
				case ev := <-d.queue:
					batch = append(batch, ev)
				default:
					if len(batch) > 0 {
						d.flush(context.Background(), batch)
					}
					return
				}
			}
		case ev := <-d.queue:
			batch = append(batch, ev)
			if len(batch) >= d.cfg.BatchSize {
				d.flush(context.Background(), batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				d.flush(context.Background(), batch)
				batch = batch[:0]
			}
		}
	}
}

func (d *Datadog) flush(ctx context.Context, batch []*activity.Event) {
	body, err := buildDatadogBody(batch, d.cfg)
	if err != nil {
		log.Errorf("datadog flush: build body: %v", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.intake, bytes.NewReader(body))
	if err != nil {
		log.Errorf("datadog flush: build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DD-API-KEY", d.cfg.APIKey)

	resp, err := d.client.Do(req)
	if err != nil {
		log.Errorf("datadog flush: transport: %v", err)
		return
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	// Datadog returns 202 Accepted on success. 4xx is a permanent error
	// (bad API key, bad payload); 5xx + 429 are transient and would
	// benefit from retry, but we keep the exporter simple — the queue
	// already buffers ~10k events and operators alert on the loud log.
	if resp.StatusCode >= 300 {
		log.Errorf("datadog flush: intake returned HTTP %d (events=%d)",
			resp.StatusCode, len(batch))
		return
	}
}

// buildDatadogBody constructs the Logs Intake JSON array.
//
// The shape that Datadog parses out of the box (no custom pipeline)
// keys off `ddsource`, `service`, `hostname`, `ddtags`, `message` and
// puts every other field on the top-level log entry. We put the openZro
// activity payload under `openzro.*` so it does not collide with
// reserved Datadog fields, plus a flat `message` for the Logs Explorer.
func buildDatadogBody(batch []*activity.Event, cfg DatadogConfig) ([]byte, error) {
	entries := make([]map[string]any, 0, len(batch))
	for _, ev := range batch {
		entries = append(entries, toDatadogEntry(ev, cfg))
	}
	return json.Marshal(entries)
}

func toDatadogEntry(ev *activity.Event, cfg DatadogConfig) map[string]any {
	entry := map[string]any{
		"timestamp": ev.Timestamp.UTC().Format(time.RFC3339Nano),
		"ddsource":  cfg.Source,
		"service":   cfg.Service,
		"message":   ev.Activity.Message(),
		"openzro": map[string]any{
			"id":            ev.ID,
			"activity":      ev.Activity.StringCode(),
			"activity_code": uint32(ev.Activity),
			"account_id":    ev.AccountID,
			"target_id":     ev.TargetID,
			"meta":          ev.Meta,
		},
	}
	if cfg.Tags != "" {
		entry["ddtags"] = cfg.Tags
	}
	hostname := cfg.Hostname
	if hostname == "" {
		hostname = ev.AccountID
	}
	if hostname != "" {
		entry["hostname"] = hostname
	}
	user := map[string]any{}
	if ev.InitiatorID != "" {
		user["id"] = ev.InitiatorID
	}
	if ev.InitiatorName != "" {
		user["name"] = ev.InitiatorName
	}
	if ev.InitiatorEmail != "" {
		user["email"] = ev.InitiatorEmail
	}
	if len(user) > 0 {
		entry["usr"] = user
	}
	return entry
}
