package exporter

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

	"github.com/openzro/openzro/management/server/activity"
)

// captureESBulk returns an httptest server that records every Bulk
// request body. Each request is split into NDJSON action/doc pairs.
type capturedBulk struct {
	mu         sync.Mutex
	auth       []string
	requests   int32
	docsByCall [][]map[string]any
}

func (c *capturedBulk) handler(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt32(&c.requests, 1)

	body, _ := io.ReadAll(r.Body)
	docs := []map[string]any{}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		// Skip action header lines, keep document lines.
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
}

func newESServer() (*capturedBulk, *httptest.Server) {
	c := &capturedBulk{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/_bulk") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		c.handler(w, r)
	}))
	return c, srv
}

func TestElastic_RequiresURLAndAuth(t *testing.T) {
	_, err := NewElastic(ElasticConfig{})
	assert.Error(t, err, "URL is required")

	_, err = NewElastic(ElasticConfig{URL: "http://x"})
	assert.Error(t, err, "auth is required")
}

func TestElastic_BatchesAndUsesAPIKeyAuth(t *testing.T) {
	c, srv := newESServer()
	defer srv.Close()

	exp, err := NewElastic(ElasticConfig{
		URL:           srv.URL,
		APIKey:        "test-api-key",
		BatchSize:     5,
		FlushInterval: 50 * time.Millisecond,
		BufferSize:    100,
	})
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		require.NoError(t, exp.Export(context.Background(), &activity.Event{
			ID:          uint64(i + 1),
			Timestamp:   time.Now(),
			Activity:    activity.PeerApproved,
			InitiatorID: "u-1",
			AccountID:   "acct-1",
		}))
	}

	require.NoError(t, exp.Close())

	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, 1, len(c.docsByCall),
		"5 events at BatchSize=5 must produce exactly one /_bulk call")
	assert.Equal(t, 5, len(c.docsByCall[0]),
		"the call must carry all 5 documents")
	assert.Equal(t, "ApiKey test-api-key", c.auth[0],
		"API key must be sent as 'ApiKey <key>' per Elastic docs")
}

func TestElastic_FlushesOnInterval(t *testing.T) {
	c, srv := newESServer()
	defer srv.Close()

	exp, err := NewElastic(ElasticConfig{
		URL:           srv.URL,
		APIKey:        "k",
		BatchSize:     1000, // way larger than what we send
		FlushInterval: 30 * time.Millisecond,
		BufferSize:    10,
	})
	require.NoError(t, err)
	defer exp.Close()

	for i := 0; i < 3; i++ {
		require.NoError(t, exp.Export(context.Background(), &activity.Event{
			ID: uint64(i + 1), Timestamp: time.Now(),
			Activity:  activity.PeerApproved,
			AccountID: "acct",
		}))
	}

	// Wait long enough for at least one timer tick to fire.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		done := len(c.docsByCall) > 0 && len(c.docsByCall[0]) == 3
		c.mu.Unlock()
		if done {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	require.GreaterOrEqual(t, len(c.docsByCall), 1,
		"interval flush did not fire")
	assert.Equal(t, 3, len(c.docsByCall[0]))
}

func TestElastic_DocumentIsECSShape(t *testing.T) {
	c, srv := newESServer()
	defer srv.Close()

	exp, err := NewElastic(ElasticConfig{
		URL:           srv.URL,
		APIKey:        "k",
		BatchSize:     1,
		FlushInterval: time.Second,
	})
	require.NoError(t, err)

	require.NoError(t, exp.Export(context.Background(), &activity.Event{
		ID:             42,
		Timestamp:      time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC),
		Activity:       activity.PeerApproved,
		InitiatorID:    "u-alice",
		InitiatorName:  "Alice",
		InitiatorEmail: "alice@example.com",
		TargetID:       "peer-7",
		AccountID:      "acct-1",
		Meta:           map[string]any{"ip": "10.0.0.7"},
	}))

	require.NoError(t, exp.Close())

	c.mu.Lock()
	defer c.mu.Unlock()
	require.NotEmpty(t, c.docsByCall)
	doc := c.docsByCall[0][0]

	assert.Equal(t, "2026-04-26T12:00:00Z", doc["@timestamp"])

	event := doc["event"].(map[string]any)
	assert.Equal(t, "event", event["kind"])
	assert.Equal(t, "peer.approve", event["action"])
	assert.Equal(t, "openzro.activity", event["dataset"])
	require.IsType(t, []any{}, event["category"])
	assert.Equal(t, "iam", event["category"].([]any)[0])

	user := doc["user"].(map[string]any)
	assert.Equal(t, "u-alice", user["id"])
	assert.Equal(t, "alice@example.com", user["email"])

	org := doc["organization"].(map[string]any)
	assert.Equal(t, "acct-1", org["id"])

	oz := doc["openzro"].(map[string]any)
	meta := oz["meta"].(map[string]any)
	assert.Equal(t, "10.0.0.7", meta["ip"])

	assert.NotEmpty(t, doc["message"], "human-readable message must be set for Kibana table view")
}

func TestElastic_DropsOnFullQueue(t *testing.T) {
	// Block the server so the loop never drains.
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer close(block)

	exp, err := NewElastic(ElasticConfig{
		URL:           srv.URL,
		APIKey:        "k",
		BatchSize:     1, // immediate flush each event
		FlushInterval: time.Hour,
		BufferSize:    1,
	})
	require.NoError(t, err)

	// First event is consumed by loop, blocked in flush — channel is empty.
	// Then up to BufferSize events fill the channel before drops.
	dropped := 0
	for i := 0; i < 50; i++ {
		err := exp.Export(context.Background(), &activity.Event{ID: uint64(i + 1)})
		if err != nil {
			dropped++
		}
	}
	assert.Greater(t, dropped, 0,
		"with the consumer blocked, some Export calls must report queue full")
}
