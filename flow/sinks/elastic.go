// Package sinks implements flow.store.Sink for streaming destinations:
// Elastic SIEM (this file) and any future native vendor drivers. The
// hot store also satisfies Sink and is built separately by
// flow/store/factory; FlowService aggregates both.
//
// Architecturally these mirror management/server/activity/exporter —
// same bulk batching, same retry posture, same env-var convention,
// different ECS category (network vs. iam).
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

// ElasticConfig configures an Elasticsearch sink for flow events.
type ElasticConfig struct {
	URL           string
	Index         string // default openzro-flow
	APIKey        string
	Username      string
	Password      string
	BatchSize     int           // default 200; flow rates can be high so the default is bigger than activity's
	FlushInterval time.Duration // default 5s
	BufferSize    int           // default 10000
	Timeout       time.Duration // default 30s
	HTTPClient    *http.Client
}

// Elastic is a flow.store.Sink that ships events to an Elasticsearch
// cluster as ECS-formatted documents (event.kind=event,
// event.category=[network]) via the Bulk API.
//
// The contract differs from store.Store on Save: this sink is
// best-effort, not durable. A full queue or a Bulk failure logs loud
// and drops the batch. Operators relying on durable retention pair
// this with the hot Postgres/ClickHouse store.
type Elastic struct {
	cfg     ElasticConfig
	client  *http.Client
	bulkURL string
	queue   chan *store.Event
	wg      sync.WaitGroup
	stopCh  chan struct{}
	closed  sync.Once
}

// NewElastic builds and starts an Elastic sink. Returns an error if
// URL is empty or both APIKey and Username are missing.
func NewElastic(cfg ElasticConfig) (*Elastic, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("flow sink: Elastic URL is required")
	}
	if cfg.APIKey == "" && cfg.Username == "" {
		return nil, fmt.Errorf("flow sink: Elastic auth missing (APIKey or Username/Password)")
	}
	if cfg.Index == "" {
		cfg.Index = "openzro-flow"
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 200
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
		queue:   make(chan *store.Event, cfg.BufferSize),
		stopCh:  make(chan struct{}),
	}
	e.wg.Add(1)
	go e.loop()
	return e, nil
}

// Save enqueues a batch of events. Non-blocking; drops on full queue
// with a loud log. Per-batch errors at the HTTP layer are logged at
// flush time, not here.
func (e *Elastic) Save(_ context.Context, events []*store.Event) error {
	for _, ev := range events {
		select {
		case e.queue <- ev:
		default:
			log.Errorf("flow sink elastic: queue full (size=%d), dropping events", e.cfg.BufferSize)
			return nil
		}
	}
	return nil
}

// Close stops the loop and flushes whatever is buffered. Idempotent.
func (e *Elastic) Close() error {
	e.closed.Do(func() {
		close(e.stopCh)
		e.wg.Wait()
	})
	return nil
}

func (e *Elastic) loop() {
	defer e.wg.Done()
	ticker := time.NewTicker(e.cfg.FlushInterval)
	defer ticker.Stop()

	batch := make([]*store.Event, 0, e.cfg.BatchSize)
	for {
		select {
		case <-e.stopCh:
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

func (e *Elastic) flush(ctx context.Context, batch []*store.Event) {
	body := buildBulkBody(batch, e.cfg.Index)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.bulkURL, bytes.NewReader(body))
	if err != nil {
		log.Errorf("flow sink elastic: build request: %v", err)
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
		log.Errorf("flow sink elastic: transport: %v", err)
		return
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 300 {
		log.Errorf("flow sink elastic: bulk returned HTTP %d (events=%d)",
			resp.StatusCode, len(batch))
	}
}

func buildBulkBody(batch []*store.Event, index string) []byte {
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

// toECSDoc projects a flow event onto Elastic Common Schema. Network
// flows map cleanly to ECS:
//
//	event.kind=event, event.category=[network], event.dataset=openzro.flow
//	event.action=start|end|drop, event.outcome=success|denied
//	network.transport=tcp|udp|icmp, network.iana_number=<protocol>
//	network.bytes, network.packets (sum of rx+tx)
//	source.ip,    source.port
//	destination.ip, destination.port
//	agent.id (peer ID), agent.type=openzro
//	organization.id (account)
//	openzro.{flow_id, rule_id, direction, ...} extension namespace
func toECSDoc(e *store.Event) map[string]any {
	doc := map[string]any{
		"@timestamp": ts(e.OccurredAt, e.ReceivedAt),
		"event": map[string]any{
			"kind":     "event",
			"category": []string{"network"},
			"action":   actionFromType(e.Type),
			"outcome":  outcomeFromType(e.Type),
			"dataset":  "openzro.flow",
			"start":    e.OccurredAt.UTC().Format(time.RFC3339Nano),
			"ingested": e.ReceivedAt.UTC().Format(time.RFC3339Nano),
		},
		"network": map[string]any{
			"transport":   transportFromProtocol(e.Protocol),
			"iana_number": fmt.Sprintf("%d", e.Protocol),
			"direction":   directionString(e.Direction),
			"bytes":       e.RxBytes + e.TxBytes,
			"packets":     e.RxPackets + e.TxPackets,
		},
		"source": map[string]any{
			"ip":      e.SourceIP,
			"port":    e.SourcePort,
			"bytes":   e.TxBytes,
			"packets": e.TxPackets,
		},
		"destination": map[string]any{
			"ip":      e.DestIP,
			"port":    e.DestPort,
			"bytes":   e.RxBytes,
			"packets": e.RxPackets,
		},
		"agent": map[string]any{
			"id":   e.PeerID,
			"type": "openzro",
		},
		"organization": map[string]any{
			"id": e.AccountID,
		},
	}

	openzroExt := map[string]any{
		"flow_id":      hex.EncodeToString(e.FlowID),
		"event_id":     hex.EncodeToString(e.EventID),
		"is_initiator": e.IsInitiator,
	}
	if len(e.RuleID) > 0 {
		openzroExt["rule_id"] = hex.EncodeToString(e.RuleID)
	}
	if len(e.SourceResource) > 0 {
		openzroExt["source_resource"] = hex.EncodeToString(e.SourceResource)
	}
	if len(e.DestResource) > 0 {
		openzroExt["dest_resource"] = hex.EncodeToString(e.DestResource)
	}
	doc["openzro"] = openzroExt

	return doc
}

// ts picks the best timestamp for ECS @timestamp. The peer's reported
// time can be skewed (clock drift); ReceivedAt is always trustworthy.
// We use OccurredAt when it looks reasonable (within an hour of
// received) and fall back to ReceivedAt otherwise.
func ts(occurred, received time.Time) string {
	if occurred.IsZero() {
		return received.UTC().Format(time.RFC3339Nano)
	}
	delta := occurred.Sub(received)
	if delta < -time.Hour || delta > time.Hour {
		return received.UTC().Format(time.RFC3339Nano)
	}
	return occurred.UTC().Format(time.RFC3339Nano)
}

func actionFromType(t store.EventType) string {
	switch t {
	case store.EventTypeStart:
		return "connection_started"
	case store.EventTypeEnd:
		return "connection_ended"
	case store.EventTypeDrop:
		return "connection_denied"
	}
	return "connection_unknown"
}

// outcomeFromType maps to ECS event.outcome enum.
func outcomeFromType(t store.EventType) string {
	if t == store.EventTypeDrop {
		return "denied"
	}
	return "success"
}

func transportFromProtocol(p uint16) string {
	switch p {
	case 1:
		return "icmp"
	case 6:
		return "tcp"
	case 17:
		return "udp"
	case 58:
		return "ipv6-icmp"
	case 132:
		return "sctp"
	}
	return ""
}

func directionString(d store.Direction) string {
	switch d {
	case store.DirectionIngress:
		return "ingress"
	case store.DirectionEgress:
		return "egress"
	}
	return "unknown"
}
