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

// ElasticConfig configures an Elasticsearch SIEM exporter.
type ElasticConfig struct {
	// URL is the base Elasticsearch endpoint (without /_bulk suffix —
	// the exporter appends it). Example: https://es.example.com:9200
	URL string
	// Index is the destination index. Defaults to "openzro-events".
	Index string
	// APIKey is the preferred auth mechanism. When set, Authorization
	// is "ApiKey <key>".
	APIKey string
	// Username + Password are the fallback when APIKey is empty.
	Username string
	Password string
	// BatchSize is the max events per /_bulk request. Default 100.
	BatchSize int
	// FlushInterval bounds the staleness of buffered events. Default 5s.
	FlushInterval time.Duration
	// BufferSize is the in-memory queue capacity. Default 10000.
	// When full, Export drops the event with a loud log line.
	BufferSize int
	// Timeout per HTTP attempt. Default 30s.
	Timeout time.Duration
	// HTTPClient overrides the default. Test seam.
	HTTPClient *http.Client
}

// Elastic ships activity events to an Elasticsearch cluster via the
// Bulk API in ECS (Elastic Common Schema) format. This is the right
// shape for Kibana's Security app: events show up under the "Audit"
// stream without any custom field mapping on the Elastic side.
//
// Performance shape: per-event Export() is non-blocking — it just
// pushes onto an internal channel. A background goroutine drains the
// channel, accumulates up to BatchSize events or until FlushInterval
// elapses, and POSTs them as one /_bulk request. Bulk batching is
// non-negotiable for Elastic at any real volume; per-event POSTs
// melt cluster CPU.
type Elastic struct {
	cfg     ElasticConfig
	client  *http.Client
	bulkURL string
	queue   chan *activity.Event
	wg      sync.WaitGroup
	stopCh  chan struct{}
	closed  sync.Once
}

// NewElastic builds and starts an Elastic exporter. Returns an error
// if URL is empty or both APIKey and Username are missing.
func NewElastic(cfg ElasticConfig) (*Elastic, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("exporter: Elastic URL is required")
	}
	if cfg.APIKey == "" && cfg.Username == "" {
		return nil, fmt.Errorf("exporter: Elastic auth missing (set APIKey or Username/Password)")
	}
	if cfg.Index == "" {
		cfg.Index = "openzro-events"
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 10000
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	client := cfg.HTTPClient
	if client == nil {
		client = safedial.Client(cfg.Timeout)
	}

	e := &Elastic{
		cfg:     cfg,
		client:  client,
		bulkURL: strings.TrimRight(cfg.URL, "/") + "/_bulk",
		queue:   make(chan *activity.Event, cfg.BufferSize),
		stopCh:  make(chan struct{}),
	}
	e.wg.Add(1)
	go e.loop()
	return e, nil
}

// Name returns the stable identifier used in log lines.
func (e *Elastic) Name() string { return "elastic" }

// Export non-blockingly enqueues the event. Returns an error only on
// queue overflow — that error is dropped by the caller (StoreEvent
// fan-out) but is visible in the loud log line we emit here so the
// drop is operationally visible.
func (e *Elastic) Export(_ context.Context, event *activity.Event) error {
	if event == nil {
		return nil
	}
	select {
	case e.queue <- event:
		return nil
	default:
		err := fmt.Errorf("elastic queue full (size=%d), dropping event", e.cfg.BufferSize)
		log.Error(err)
		return err
	}
}

// Close stops the background loop and flushes whatever is buffered.
// Safe to call multiple times. Idempotent.
func (e *Elastic) Close() error {
	e.closed.Do(func() {
		close(e.stopCh)
		e.wg.Wait()
	})
	return nil
}

// loop drains the queue and POSTs batches.
func (e *Elastic) loop() {
	defer e.wg.Done()
	ticker := time.NewTicker(e.cfg.FlushInterval)
	defer ticker.Stop()

	batch := make([]*activity.Event, 0, e.cfg.BatchSize)
	for {
		select {
		case <-e.stopCh:
			// Drain remaining queue entries on shutdown.
			for {
				select {
				case ev := <-e.queue:
					batch = append(batch, ev)
				default:
					if len(batch) > 0 {
						e.flush(context.Background(), batch)
					}
					return
				}
			}
		case ev := <-e.queue:
			batch = append(batch, ev)
			if len(batch) >= e.cfg.BatchSize {
				e.flush(context.Background(), batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				e.flush(context.Background(), batch)
				batch = batch[:0]
			}
		}
	}
}

// flush POSTs a batch as one Bulk request. Errors are logged loud and
// dropped — durability is the operator's monitoring story (alert on
// the dropped log line).
func (e *Elastic) flush(ctx context.Context, batch []*activity.Event) {
	body := buildBulkBody(batch, e.cfg.Index)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.bulkURL, bytes.NewReader(body))
	if err != nil {
		log.Errorf("elastic flush: build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	if e.cfg.APIKey != "" {
		req.Header.Set("Authorization", "ApiKey "+e.cfg.APIKey)
	} else {
		req.SetBasicAuth(e.cfg.Username, e.cfg.Password)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		log.Errorf("elastic flush: transport: %v", err)
		return
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 300 {
		log.Errorf("elastic flush: bulk returned HTTP %d (events=%d)",
			resp.StatusCode, len(batch))
		return
	}
}

// buildBulkBody constructs the NDJSON payload for the Bulk API: each
// event is two lines, the action header and the document.
func buildBulkBody(batch []*activity.Event, index string) []byte {
	var buf bytes.Buffer
	header, _ := json.Marshal(map[string]any{
		"index": map[string]any{"_index": index},
	})
	for _, ev := range batch {
		buf.Write(header)
		buf.WriteByte('\n')
		doc, _ := json.Marshal(toECSDoc(ev))
		buf.Write(doc)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

// toECSDoc projects an activity.Event onto Elastic Common Schema
// (https://www.elastic.co/guide/en/ecs/current/index.html) so events
// show up in Kibana's Security app under the standard "audit" view.
//
//   event.kind=event, event.category=iam
//   user.{id,name,email}
//   organization.id (the openZro account)
//   openzro.{...}  custom namespace for fields ECS does not cover
func toECSDoc(ev *activity.Event) map[string]any {
	doc := map[string]any{
		"@timestamp": ev.Timestamp.UTC().Format(time.RFC3339Nano),
		"event": map[string]any{
			"kind":     "event",
			"category": []string{"iam"},
			"action":   ev.Activity.StringCode(),
			"code":     uint32(ev.Activity),
			"id":       fmt.Sprint(ev.ID),
			"dataset":  "openzro.activity",
		},
		"organization": map[string]any{
			"id": ev.AccountID,
		},
	}

	// user.* — populated only if there is something to report; ECS
	// considers these all optional and Kibana is happier with absent
	// fields than with empty strings.
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
	if ev.TargetID != "" {
		user["target"] = map[string]any{"id": ev.TargetID}
	}
	if len(user) > 0 {
		doc["user"] = user
	}

	if len(ev.Meta) > 0 {
		doc["openzro"] = map[string]any{"meta": ev.Meta}
	}

	doc["message"] = ev.Activity.Message()
	return doc
}
