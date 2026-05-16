package sinks

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/flow/store"
	"github.com/openzro/openzro/safedial"
)

// Per-attempt backoff is capped so an operator-set InitialBackoff plus
// exponential growth can never wedge the drainer for minutes.
const httpMaxBackoffStep = 30 * time.Second

// httpMaxAttemptsCap bounds worst-case flush time. The single drainer
// goroutine runs flush() inline, so a slow/flapping endpoint with a
// high operator-set MaxAttempts stalls draining for roughly
// MaxAttempts*(Timeout+backoff) — at the cap that is ~10*(15s+30s) ≈
// 7m, during which the buffer fills and Save drops. The default
// MaxAttempts=1 makes retry opt-in precisely so this footgun is off
// unless an operator deliberately accepts it; the cap bounds the
// blast radius if they do. (4xx is non-retryable — see doRequest — so
// a misconfig does not pay this cost every batch forever.)
const httpMaxAttemptsCap = 10

// httpDropLogInterval throttles the queue-full drop log. Under a
// sustained outage Save is called continuously by the fan-out; an
// unthrottled per-call Errorf would be a thousands-per-second log
// flood. One line per interval carries the cumulative count instead.
const httpDropLogInterval = 10 * time.Second

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
//
// Delivery is at-least-once: a retry after a timeout where the server
// actually processed the batch double-delivers it. Every record
// carries event_id so receivers can deduplicate.
type HTTP struct {
	cfg    HTTPConfig
	client *http.Client

	// logURL is scheme://host only — never the path/query/userinfo of
	// cfg.URL. All log lines use it so an operator who put credentials
	// in the URL (https://user:pass@host) does not leak them to logs.
	logURL string

	queue  chan *store.Event
	wg     sync.WaitGroup
	stopCh chan struct{}
	closed sync.Once

	// drop accounting for the throttled queue-full log.
	dropped          atomic.Uint64
	lastDropLogNanos atomic.Int64

	// baseCtx is the parent of every request context; baseCancel fires
	// on Close so an in-flight request and any pending backoff abort
	// promptly instead of blocking shutdown for MaxAttempts*Timeout.
	baseCtx    context.Context
	baseCancel context.CancelFunc
}

// NewHTTP constructs and starts the generic HTTP flow sink.
func NewHTTP(cfg HTTPConfig) (*HTTP, error) {
	raw := strings.TrimSpace(cfg.URL)
	if raw == "" {
		return nil, fmt.Errorf("flow sink HTTP: URL is required")
	}
	// Fail fast on a malformed URL. Without this an env-configured
	// typo (no model-layer validation on the env path) constructs a
	// sink that fails every flush forever. The error never echoes the
	// raw URL — it may carry credentials.
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("flow sink HTTP: invalid URL — need an absolute http(s) URL")
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
		logURL:     parsed.Scheme + "://" + parsed.Host,
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
	for i, ev := range events {
		select {
		case h.queue <- ev:
		default:
			h.recordDrop(len(events) - i)
			return nil
		}
	}
	return nil
}

// recordDrop accounts dropped events and logs at most once per
// httpDropLogInterval with the cumulative count, so a sustained
// outage cannot turn the hot-path drop into a log flood. Lock-free:
// the timestamp CAS elects a single logger per window.
func (h *HTTP) recordDrop(n int) {
	total := h.dropped.Add(uint64(n))
	now := time.Now().UnixNano()
	last := h.lastDropLogNanos.Load()
	if now-last < int64(httpDropLogInterval) {
		return
	}
	if !h.lastDropLogNanos.CompareAndSwap(last, now) {
		return // another goroutine owns this window's log line
	}
	log.Errorf("flow sink HTTP: queue full (size=%d), %d events dropped cumulatively — %s slow/unreachable",
		h.cfg.BufferSize, total, h.logURL)
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
			// Shutdown drain: coalesces the whole backlog (up to
			// BufferSize) into one batch and one POST — it does NOT
			// chunk by BatchSize like steady state. A receiver may 413
			// a very large body; shutdown delivery is best-effort and
			// the hot store remains the durable record, so this is an
			// accepted trade for a single fast drain.
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
		ok, retryable := h.doRequest(body)
		if ok {
			return
		}
		if !retryable {
			// 4xx / unbuildable request: a misconfigured receiver must
			// not become a per-batch retry storm (same posture as the
			// activity HTTP webhook).
			log.Errorf("flow sink HTTP: permanent failure to %s, dropping %d events",
				h.logURL, len(batch))
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
		h.logURL, h.cfg.MaxAttempts, len(batch))
}

// doRequest performs a single attempt. ok=true means delivered (2xx).
// retryable reports whether a non-ok outcome is worth another try:
// 4xx (and an unbuildable request) are permanent — the caller must
// not loop on them. The per-attempt context derives from baseCtx so
// Close aborts in flight. Log lines use logURL, never cfg.URL.
func (h *HTTP) doRequest(body []byte) (ok bool, retryable bool) {
	ctx, cancel := context.WithTimeout(h.baseCtx, h.cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.URL, bytes.NewReader(body))
	if err != nil {
		log.Errorf("flow sink HTTP: build request for %s: %v", h.logURL, err)
		return false, false
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range h.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		log.Errorf("flow sink HTTP: transport to %s: %v", h.logURL, err)
		return false, true
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return true, false
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		log.Errorf("flow sink HTTP: %s returned HTTP %d (config error — not retrying)",
			h.logURL, resp.StatusCode)
		return false, false
	default:
		log.Errorf("flow sink HTTP: %s returned HTTP %d", h.logURL, resp.StatusCode)
		return false, true
	}
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
//
// Limitation: the separator is a literal comma, so a header VALUE
// containing a comma (Accept lists, an RFC1123 Date, multi-value
// headers) splits wrong here. Auth headers (Bearer/Basic/API-key)
// have no comma, so the common case is fine; callers needing
// comma-bearing values must use the DB-configured path, which carries
// a structured map and is unaffected.
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
