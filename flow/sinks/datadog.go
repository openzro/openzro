package sinks

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/flow/store"
	"github.com/openzro/openzro/safedial"
)

// DatadogConfig configures the Datadog Logs Intake sink for flow
// events. Same Logs Intake API as the activity exporter
// (management/server/activity/exporter/datadog.go) — the difference
// is what we put on the wire: flow records use Datadog Network
// Performance Monitoring conventions under the network.* namespace
// rather than openzro.*, so the events show up natively in
// Datadog's NPM views without a custom pipeline.
type DatadogConfig struct {
	// Site selects the destination region by preset. Recognised:
	//   us1 (default), us3, us5, eu1, ap1.
	// Empty defaults to us1. Ignored when URL is set explicitly.
	Site string

	// URL overrides the site preset. Use only when proxying through
	// an internal log forwarder. The /api/v2/logs path is appended
	// automatically.
	URL string

	// APIKey is the Datadog API key. Required.
	APIKey string

	// Service tags every event with the same service name. Defaults
	// to "openzro-flow" — distinct from the activity exporter's
	// "openzro" so an operator filtering by service can see the two
	// streams separately.
	Service string

	// Source is the Datadog `ddsource` field — Datadog's parser key.
	// Defaults to "openzro-flow".
	Source string

	// Tags are appended to every event's `ddtags`.
	// Format: "env:prod,team:secops".
	Tags string

	// BatchSize bounds events per request. Datadog hard-limits at
	// 1000; the constructor floors to that. Default 500 — larger
	// than the activity exporter because flow events are smaller
	// per-record and we expect volumes orders of magnitude higher.
	BatchSize int

	// FlushInterval bounds staleness. Default 30s — quicker than
	// the activity exporter because flow ingestion latency matters
	// for active investigation.
	FlushInterval time.Duration

	// BufferSize is the in-memory queue capacity. Default 100000 —
	// flow events come in bursts; sized to absorb a 1k-events/s
	// peer fleet's 100s of accumulated events without dropping.
	BufferSize int

	// Timeout per HTTP attempt. Default 15s.
	Timeout time.Duration

	// HTTPClient overrides the default. Test seam.
	HTTPClient *http.Client
}

// Datadog ships flow events to Datadog Logs Intake as one log entry
// per flow record, keyed under network.* so Datadog's NPM features
// pick them up automatically. Ingestion is best-effort: failures are
// logged and dropped — durability lives in the hot store and (when
// configured) the S3 archive.
type Datadog struct {
	cfg    DatadogConfig
	client *http.Client
	intake string

	queue  chan *store.Event
	wg     sync.WaitGroup
	stopCh chan struct{}
	closed sync.Once
}

// datadogFlowSiteToHost mirrors the activity exporter's mapping. Same
// hosts; duplicated locally so flow/sinks does not import the
// management package.
var datadogFlowSiteToHost = map[string]string{
	"us1": "http-intake.logs.datadoghq.com",
	"us3": "http-intake.logs.us3.datadoghq.com",
	"us5": "http-intake.logs.us5.datadoghq.com",
	"eu1": "http-intake.logs.datadoghq.eu",
	"ap1": "http-intake.logs.ap1.datadoghq.com",
}

// NewDatadog constructs and starts the Datadog flow sink.
func NewDatadog(cfg DatadogConfig) (*Datadog, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("flow sink Datadog: API key is required")
	}
	intake, err := resolveDatadogFlowIntake(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Service == "" {
		cfg.Service = "openzro-flow"
	}
	if cfg.Source == "" {
		cfg.Source = "openzro-flow"
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 500
	}
	if cfg.BatchSize > 1000 {
		// Datadog hard limit. Floor — bumping further silently
		// produces 413s on every flush.
		cfg.BatchSize = 1000
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 30 * time.Second
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 100000
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	client := cfg.HTTPClient
	if client == nil {
		client = safedial.Client(cfg.Timeout)
	}
	d := &Datadog{
		cfg:    cfg,
		client: client,
		intake: intake,
		queue:  make(chan *store.Event, cfg.BufferSize),
		stopCh: make(chan struct{}),
	}
	d.wg.Add(1)
	go d.loop()
	return d, nil
}

func resolveDatadogFlowIntake(cfg DatadogConfig) (string, error) {
	if cfg.URL != "" {
		return strings.TrimRight(cfg.URL, "/") + "/api/v2/logs", nil
	}
	site := cfg.Site
	if site == "" {
		site = "us1"
	}
	host, ok := datadogFlowSiteToHost[strings.ToLower(site)]
	if !ok {
		return "", fmt.Errorf("flow sink Datadog: unknown site %q (valid: us1, us3, us5, eu1, ap1)", cfg.Site)
	}
	return "https://" + host + "/api/v2/logs", nil
}

// Save enqueues a batch. Non-blocking; drops on full queue.
func (d *Datadog) Save(_ context.Context, events []*store.Event) error {
	for _, ev := range events {
		select {
		case d.queue <- ev:
		default:
			log.Errorf("flow sink Datadog: queue full (size=%d), dropping events", d.cfg.BufferSize)
			return nil
		}
	}
	return nil
}

// Close stops the loop and flushes whatever is buffered.
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

	batch := make([]*store.Event, 0, d.cfg.BatchSize)
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

func (d *Datadog) flush(ctx context.Context, batch []*store.Event) {
	body, err := buildDatadogFlowBody(batch, d.cfg)
	if err != nil {
		log.Errorf("flow sink Datadog: build body: %v", err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.intake, bytes.NewReader(body))
	if err != nil {
		log.Errorf("flow sink Datadog: build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DD-API-KEY", d.cfg.APIKey)

	resp, err := d.client.Do(req)
	if err != nil {
		log.Errorf("flow sink Datadog: transport: %v", err)
		return
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode >= 300 {
		log.Errorf("flow sink Datadog: intake returned HTTP %d (events=%d)",
			resp.StatusCode, len(batch))
		return
	}
}

// buildDatadogFlowBody assembles the Logs Intake JSON array. The
// per-event shape uses Datadog's NPM/network conventions:
//
//   network.client.ip / network.client.port — initiator side
//   network.destination.ip / network.destination.port — responder
//   network.bytes_read / network.bytes_written
//   network.transport — protocol name
//
// plus the standard Datadog log fields (timestamp, ddsource, service,
// host) and an `openzro_flow` namespace for fields Datadog NPM does
// not have a canonical name for (peer_id, rule_id, event type).
func buildDatadogFlowBody(batch []*store.Event, cfg DatadogConfig) ([]byte, error) {
	entries := make([]map[string]any, 0, len(batch))
	for _, ev := range batch {
		entries = append(entries, toDatadogFlowEntry(ev, cfg))
	}
	return json.Marshal(entries)
}

func toDatadogFlowEntry(e *store.Event, cfg DatadogConfig) map[string]any {
	entry := map[string]any{
		"timestamp": e.ReceivedAt.UTC().Format(time.RFC3339Nano),
		"ddsource":  cfg.Source,
		"service":   cfg.Service,
		// Datadog NPM-style network.* attributes
		"network": map[string]any{
			"client": map[string]any{
				"ip":   e.SourceIP,
				"port": e.SourcePort,
			},
			"destination": map[string]any{
				"ip":   e.DestIP,
				"port": e.DestPort,
			},
			"bytes_read":    e.RxBytes,
			"bytes_written": e.TxBytes,
			"transport":     protocolName(e.Protocol),
			"direction":     dirString(e.Direction),
		},
		// openzro-specific extras Datadog NPM does not have a slot for
		"openzro_flow": map[string]any{
			"event_id":      hex.EncodeToString(e.EventID),
			"flow_id":       hex.EncodeToString(e.FlowID),
			"peer_id":       e.PeerID,
			"account_id":    e.AccountID,
			"type":          typeString(e.Type),
			"is_initiator":  e.IsInitiator,
			"occurred_at":   e.OccurredAt.UTC().Format(time.RFC3339Nano),
			"rx_packets":    e.RxPackets,
			"tx_packets":    e.TxPackets,
			"icmp_type":     e.ICMPType,
			"icmp_code":     e.ICMPCode,
			"rule_id":       hex.EncodeToString(e.RuleID),
			"src_resource":  hex.EncodeToString(e.SourceResource),
			"dest_resource": hex.EncodeToString(e.DestResource),
		},
		// Use AccountID as host so flows from different tenants
		// separate cleanly in Datadog's host map.
		"host":    e.AccountID,
		"message": fmt.Sprintf("flow %s %s:%d → %s:%d %s",
			typeString(e.Type), e.SourceIP, e.SourcePort,
			e.DestIP, e.DestPort, protocolName(e.Protocol)),
	}
	if cfg.Tags != "" {
		entry["ddtags"] = cfg.Tags
	}
	return entry
}

// protocolName maps the IANA protocol number to a name Datadog NPM
// recognises. Anything not in the small set of common ones falls back
// to "proto-<n>" so the field is still searchable.
func protocolName(proto uint16) string {
	switch proto {
	case 0:
		return ""
	case 1:
		return "icmp"
	case 6:
		return "tcp"
	case 17:
		return "udp"
	case 47:
		return "gre"
	case 50:
		return "esp"
	case 58:
		return "icmpv6"
	}
	return fmt.Sprintf("proto-%d", proto)
}

// typeString / dirString already live in s3.go for the existing
// archive sink — reused here so the wire shape stays consistent
// across destinations.
