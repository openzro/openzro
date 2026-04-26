package sinks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/flow/store"
)

// capturedBulk records every Bulk request body for assertion. Each
// request body is split into NDJSON action/doc pairs.
type capturedBulk struct {
	mu         sync.Mutex
	auth       []string
	requests   atomic.Int32
	docsByCall [][]map[string]any
}

func newESServer(c *capturedBulk) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/_bulk") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		c.requests.Add(1)
		body, _ := io.ReadAll(r.Body)
		docs := []map[string]any{}
		scanner := bufio.NewScanner(bytes.NewReader(body))
		for scanner.Scan() {
			var head map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &head); err == nil {
				if _, isAction := head["index"]; isAction {
					continue
				}
				docs = append(docs, head)
			}
		}
		c.mu.Lock()
		c.docsByCall = append(c.docsByCall, docs)
		c.auth = append(c.auth, r.Header.Get("Authorization"))
		c.mu.Unlock()

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":false,"items":[]}`))
	}))
}

func sampleEvent() *store.Event {
	occurred := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	return &store.Event{
		EventID:     []byte{0xde, 0xad, 0xbe, 0xef},
		FlowID:      []byte{0xfe, 0xed, 0xfa, 0xce},
		PeerID:      "peer-1",
		AccountID:   "acct-1",
		IsInitiator: true,
		OccurredAt:  occurred,
		ReceivedAt:  occurred.Add(time.Millisecond),
		Type:        store.EventTypeStart,
		Direction:   store.DirectionEgress,
		Protocol:    6,
		SourceIP:    "10.0.0.1",
		DestIP:      "10.0.0.2",
		SourcePort:  49152,
		DestPort:    443,
		RxPackets:   10,
		TxPackets:   20,
		RxBytes:     100,
		TxBytes:     200,
		RuleID:      []byte("rule-allow"),
	}
}

func TestElastic_RequiresURLAndAuth(t *testing.T) {
	_, err := NewElastic(ElasticConfig{})
	assert.Error(t, err)

	_, err = NewElastic(ElasticConfig{URL: "http://x"})
	assert.Error(t, err, "auth required")
}

func TestElastic_BatchesAndUsesAPIKey(t *testing.T) {
	c := &capturedBulk{}
	srv := newESServer(c)
	defer srv.Close()

	exp, err := NewElastic(ElasticConfig{
		URL:           srv.URL,
		APIKey:        "api-key-test",
		BatchSize:     5,
		FlushInterval: 50 * time.Millisecond,
		BufferSize:    100,
	})
	require.NoError(t, err)

	events := make([]*store.Event, 5)
	for i := range events {
		events[i] = sampleEvent()
		events[i].EventID = []byte{byte(i)}
	}
	require.NoError(t, exp.Save(context.Background(), events))
	require.NoError(t, exp.Close())

	c.mu.Lock()
	defer c.mu.Unlock()
	require.Len(t, c.docsByCall, 1)
	assert.Equal(t, 5, len(c.docsByCall[0]))
	assert.Equal(t, "ApiKey api-key-test", c.auth[0])
}

func TestElastic_ECSShape(t *testing.T) {
	c := &capturedBulk{}
	srv := newESServer(c)
	defer srv.Close()

	exp, err := NewElastic(ElasticConfig{
		URL:           srv.URL,
		APIKey:        "k",
		BatchSize:     1,
		FlushInterval: time.Second,
	})
	require.NoError(t, err)

	require.NoError(t, exp.Save(context.Background(), []*store.Event{sampleEvent()}))
	require.NoError(t, exp.Close())

	c.mu.Lock()
	defer c.mu.Unlock()
	require.NotEmpty(t, c.docsByCall)
	doc := c.docsByCall[0][0]

	event := doc["event"].(map[string]any)
	assert.Equal(t, "event", event["kind"])
	assert.Equal(t, "openzro.flow", event["dataset"])
	assert.Equal(t, "connection_started", event["action"])
	assert.Equal(t, "success", event["outcome"])
	cats := event["category"].([]any)
	assert.Equal(t, "network", cats[0],
		"network category is the ECS contract for traffic events")

	network := doc["network"].(map[string]any)
	assert.Equal(t, "tcp", network["transport"])
	assert.Equal(t, "egress", network["direction"])
	assert.EqualValues(t, 300, network["bytes"], "bytes = rx+tx")

	src := doc["source"].(map[string]any)
	assert.Equal(t, "10.0.0.1", src["ip"])
	assert.EqualValues(t, 49152, src["port"])

	dst := doc["destination"].(map[string]any)
	assert.Equal(t, "10.0.0.2", dst["ip"])
	assert.EqualValues(t, 443, dst["port"])

	agent := doc["agent"].(map[string]any)
	assert.Equal(t, "peer-1", agent["id"])
	assert.Equal(t, "openzro", agent["type"])

	org := doc["organization"].(map[string]any)
	assert.Equal(t, "acct-1", org["id"])

	oz := doc["openzro"].(map[string]any)
	assert.Equal(t, "feedface", oz["flow_id"], "flow_id must be hex-encoded")
}

func TestElastic_DropEventOutcomeIsDenied(t *testing.T) {
	c := &capturedBulk{}
	srv := newESServer(c)
	defer srv.Close()

	exp, err := NewElastic(ElasticConfig{
		URL:           srv.URL,
		APIKey:        "k",
		BatchSize:     1,
		FlushInterval: time.Second,
	})
	require.NoError(t, err)

	ev := sampleEvent()
	ev.Type = store.EventTypeDrop
	require.NoError(t, exp.Save(context.Background(), []*store.Event{ev}))
	require.NoError(t, exp.Close())

	c.mu.Lock()
	defer c.mu.Unlock()
	doc := c.docsByCall[0][0]
	event := doc["event"].(map[string]any)
	assert.Equal(t, "denied", event["outcome"],
		"DROP events surface as outcome=denied so Kibana queries can split allowed vs blocked")
	assert.Equal(t, "connection_denied", event["action"])
}

func TestElastic_DefaultIndex(t *testing.T) {
	c := &capturedBulk{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// First line is the action header carrying the index
		var first map[string]any
		_ = json.Unmarshal(bytes.SplitN(body, []byte("\n"), 2)[0], &first)
		idx := first["index"].(map[string]any)["_index"]
		c.mu.Lock()
		c.docsByCall = append(c.docsByCall, []map[string]any{{"_index": idx}})
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exp, err := NewElastic(ElasticConfig{
		URL:           srv.URL,
		APIKey:        "k",
		BatchSize:     1,
		FlushInterval: time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, exp.Save(context.Background(), []*store.Event{sampleEvent()}))
	require.NoError(t, exp.Close())

	c.mu.Lock()
	defer c.mu.Unlock()
	require.NotEmpty(t, c.docsByCall)
	assert.Equal(t, "openzro-flow", c.docsByCall[0][0]["_index"],
		"flow events go to a different default index than activity (openzro-events)")
}
