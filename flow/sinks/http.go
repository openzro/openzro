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

// Per-attempt backoff is capped so an operator-set InitialBackoff plus
// exponential growth can never wedge the drainer for minutes.
const httpMaxBackoffStep = 30 * time.Second

// httpMaxAttemptsCap bounds worst-case flush time to
// MaxAttempts*(Timeout+backoff). Without a ceiling a fat-fingered
// MaxAttempts=10000 against a dead endpoint would stall every
// subsequent batch and fill the buffer.
const httpMaxAttemptsCap = 10

// HTTPConfig configures the generic webhook sink for flow events. It
// is the runtime twin of flow_exports.HTTPDestConfig: the management
// layer decrypts a stored row into these fields. Unlike the Datadog
// sink (best-effort single-shot), this sink retries — that is the
// whole reason HTTPDestConfig carries MaxAttempts/InitialBackoff.
type HTTPConfig struct {
	// URL is the POST target. Required. Auth is expressed as a header
	// (e.g. Authorization), not a URL knob, so any scheme works.
	URL string

	// Headers are sent verbatim on every request. This is where bearer
	// tokens / HMAC / tenant ids live. PublicView at the model layer
	// already strips the values from API responses.
	Headers map[string]string

	// Timeout bounds a single HTTP attempt. Default 15s.
	Timeout time.Duration

	// MaxAttempts is the total number of tries per batch (not retries
	// on top of the first). <=0 means 1 — best-effort, matching the
	// Datadog sink. Floored to httpMaxAttemptsCap.
	MaxAttempts int

	// InitialBackoff is the pause before the 2nd attempt; it doubles
	// each subsequent attempt, capped at httpMaxBackoffStep. Default
	// 500ms. Only consulted when MaxAttempts > 1.
	InitialBackoff time.Duration

	// BatchSize bounds events per request. Default 500.
	BatchSize int

	// FlushInterval bounds staleness. Default 30s.
	FlushInterval time.Duration

	// BufferSize is the in-memory queue capacity. Default 100000.
	BufferSize int

	// HTTPClient overrides the default. Test seam.
	HTTPClient *http.Client
}

// HTTP ships flow events to an arbitrary HTTP endpoint as a JSON array
// of flat, destination-neutral records. Delivery is bounded-retry then
// best-effort: after MaxAttempts the batch is logged and dropped —
// durability lives in the hot store and (when configured) the archive,
// never in a webhook receiver.
type HTTP struct {
	cfg    HTTPConfig
	client *http.Client

	queue  chan *store.Event
	wg     sync.WaitGroup
	stopCh chan struct{}
	closed sync.Once

	// baseCtx is the parent of every request context; baseCancel fires
	// on Close so an in-flight request and any pending backoff abort
	// promptly instead of blocking shutdown for MaxAttempts*Timeout.
	baseCtx    context.Context
	baseCancel context.CancelFunc
}

// NewHTTP constructs and starts the generic HTTP flow sink.
func NewHTTP(cfg HTTPConfig) (*HTTP, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, fmt.Errorf("flow sink HTTP: URL is required")
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.MaxAttempts > httpMaxAttemptsCap {
		cfg.MaxAttempts = httpMaxAttemptsCap
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = 500 * time.Millisecond
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 500
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
	baseCtx, baseCancel := context.WithCancel(context.Background())
	h := &HTTP{
		cfg:        cfg,
		client:     client,
		queue:      make(chan *store.Event, cfg.BufferSize),
		stopCh:     make(chan struct{}),
		baseCtx:    baseCtx,
		baseCancel: baseCancel,
	}
	h.wg.Add(1)
	go h.loop()
	return h, nil
}

// Save enqueues a batch. Non-blocking; drops on full queue. The Sink
// contract (store.go) forbids back-pressuring the gRPC fan-out.
func (h *HTTP) Save(_ context.Context, events []*store.Event) error {
	for _, ev := range events {
		select {
		case h.queue <- ev:
		default:
			log.Errorf("flow sink HTTP: queue full (size=%d), dropping events", h.cfg.BufferSize)
			return nil
		}
	}
	return nil
}

// Close stops the loop, flushes whatever is buffered (best-effort,
// graceful), then cancels the base context to release resources. A
// wedged endpoint can make the final graceful flush take up to one
// Timeout — acceptable at shutdown and bounded.
func (h *HTTP) Close() error {
	h.closed.Do(func() {
		close(h.stopCh)
		h.wg.Wait()
		h.baseCancel()
	})
	return nil
}

func (h *HTTP) loop() {
	defer h.wg.Done()
	ticker := time.NewTicker(h.cfg.FlushInterval)
	defer ticker.Stop()

	batch := make([]*store.Event, 0, h.cfg.BatchSize)
	for {
		select {
		case <-h.stopCh:
			for {
				select {
				case ev := <-h.queue:
					batch = append(batch, ev)
				default:
					if len(batch) > 0 {
						h.flush(batch)
					}
					return
				}
			}
		case ev := <-h.queue:
			batch = append(batch, ev)
			if len(batch) >= h.cfg.BatchSize {
				h.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				h.flush(batch)
				batch = batch[:0]
			}
		}
	}
}

// flush delivers one batch with bounded, abortable retry. After
// MaxAttempts the batch is dropped (logged at Errorf per the hot-path
// logging rule) — the hot store remains the durable record.
func (h *HTTP) flush(batch []*store.Event) {
	body, err := json.Marshal(toHTTPEntries(batch))
	if err != nil {
		log.Errorf("flow sink HTTP: marshal body: %v", err)
		return
	}
	for attempt := 1; attempt <= h.cfg.MaxAttempts; attempt++ {
		if h.doRequest(body) {
			return
		}
		if attempt == h.cfg.MaxAttempts {
			break
		}
		if !h.backoff(attempt) {
			log.Errorf("flow sink HTTP: aborted during backoff, dropping %d events", len(batch))
			return
		}
	}
	log.Errorf("flow sink HTTP: %s unreachable after %d attempt(s), dropping %d events",
		h.cfg.URL, h.cfg.MaxAttempts, len(batch))
}

// doRequest performs a single attempt. Returns true on a 2xx. The
// per-attempt context derives from baseCtx so Close aborts in flight.
func (h *HTTP) doRequest(body []byte) bool {
	ctx, cancel := context.WithTimeout(h.baseCtx, h.cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.URL, bytes.NewReader(body))
	if err != nil {
		log.Errorf("flow sink HTTP: build request: %v", err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range h.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		log.Errorf("flow sink HTTP: transport: %v", err)
		return false
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode >= 300 {
		log.Errorf("flow sink HTTP: %s returned HTTP %d", h.cfg.URL, resp.StatusCode)
		return false
	}
	return true
}

// backoff sleeps before the next attempt, aborting immediately if the
// sink is closing. Returns false if it was aborted (caller drops).
func (h *HTTP) backoff(attempt int) bool {
	d := h.cfg.InitialBackoff << (attempt - 1)
	if d <= 0 || d > httpMaxBackoffStep {
		d = httpMaxBackoffStep
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-h.stopCh:
		return false
	case <-h.baseCtx.Done():
		return false
	}
}

// httpFlowEntry is the destination-neutral wire shape. Flat snake_case,
// hex for the binary ids, RFC3339Nano timestamps — parseable by any
// generic receiver without openzro-specific knowledge. Deliberately
// not Datadog's NPM namespacing: this sink targets arbitrary webhooks.
type httpFlowEntry struct {
	EventID        string `json:"event_id"`
	FlowID         string `json:"flow_id"`
	Type           string `json:"type"`
	Direction      string `json:"direction"`
	Protocol       string `json:"protocol"`
	IsInitiator    bool   `json:"is_initiator"`
	AccountID      string `json:"account_id"`
	PeerID         string `json:"peer_id"`
	OccurredAt     string `json:"occurred_at"`
	ReceivedAt     string `json:"received_at"`
	SourceIP       string `json:"source_ip"`
	SourcePort     uint32 `json:"source_port"`
	DestIP         string `json:"dest_ip"`
	DestPort       uint32 `json:"dest_port"`
	ICMPType       uint16 `json:"icmp_type"`
	ICMPCode       uint16 `json:"icmp_code"`
	RxPackets      uint64 `json:"rx_packets"`
	TxPackets      uint64 `json:"tx_packets"`
	RxBytes        uint64 `json:"rx_bytes"`
	TxBytes        uint64 `json:"tx_bytes"`
	RuleID         string `json:"rule_id"`
	SourceResource string `json:"source_resource"`
	DestResource   string `json:"dest_resource"`
}

func toHTTPEntries(batch []*store.Event) []httpFlowEntry {
	out := make([]httpFlowEntry, 0, len(batch))
	for _, e := range batch {
		out = append(out, httpFlowEntry{
			EventID:        hex.EncodeToString(e.EventID),
			FlowID:         hex.EncodeToString(e.FlowID),
			Type:           typeString(e.Type),
			Direction:      dirString(e.Direction),
			Protocol:       protocolName(e.Protocol),
			IsInitiator:    e.IsInitiator,
			AccountID:      e.AccountID,
			PeerID:         e.PeerID,
			OccurredAt:     e.OccurredAt.UTC().Format(time.RFC3339Nano),
			ReceivedAt:     e.ReceivedAt.UTC().Format(time.RFC3339Nano),
			SourceIP:       e.SourceIP,
			SourcePort:     e.SourcePort,
			DestIP:         e.DestIP,
			DestPort:       e.DestPort,
			ICMPType:       e.ICMPType,
			ICMPCode:       e.ICMPCode,
			RxPackets:      e.RxPackets,
			TxPackets:      e.TxPackets,
			RxBytes:        e.RxBytes,
			TxBytes:        e.TxBytes,
			RuleID:         hex.EncodeToString(e.RuleID),
			SourceResource: hex.EncodeToString(e.SourceResource),
			DestResource:   hex.EncodeToString(e.DestResource),
		})
	}
	return out
}

// parseHTTPHeaders parses the OPENZRO_FLOW_EXPORT_HTTP_HEADERS env
// syntax: comma-separated "Name: Value" pairs. Malformed pairs (no
// colon) are skipped rather than failing the whole sink — a bad
// header should not silently disable flow export entirely, but it
// must also not crash boot.
func parseHTTPHeaders(s string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		idx := strings.Index(pair, ":")
		if idx <= 0 {
			log.Warnf("flow sink HTTP: ignoring malformed header %q (want \"Name: Value\")", pair)
			continue
		}
		name := strings.TrimSpace(pair[:idx])
		val := strings.TrimSpace(pair[idx+1:])
		if name == "" {
			continue
		}
		out[name] = val
	}
	return out
}
