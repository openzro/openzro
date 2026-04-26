package sinks

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/flow/store"
)

// captured records every PUT received by the fake S3.
type captured struct {
	mu      sync.Mutex
	keys    []string
	bodies  [][]byte
	headers []http.Header
}

// fakeS3 mimics enough of the S3 PUT interface for our tests. It
// accepts any path-style request and records the body. Real S3
// behaviors (auth, multipart, etag) are out of scope.
func fakeS3(c *captured) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.keys = append(c.keys, r.URL.Path)
		c.bodies = append(c.bodies, body)
		c.headers = append(c.headers, r.Header.Clone())
		c.mu.Unlock()
		w.Header().Set("ETag", `"deadbeef"`)
		w.WriteHeader(http.StatusOK)
	}))
}

func newS3SinkTo(t *testing.T, srvURL string, cfg S3Config) *S3 {
	t.Helper()
	cfg.Endpoint = srvURL
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.AccessKey == "" {
		cfg.AccessKey = "test"
		cfg.SecretKey = "test"
	}
	s, err := NewS3(context.Background(), cfg)
	require.NoError(t, err)
	return s
}

func sampleS3Event() *store.Event {
	t := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	return &store.Event{
		EventID:    []byte{0xde, 0xad, 0xbe, 0xef, 0xff},
		FlowID:     []byte{0xfe, 0xed},
		PeerID:     "peer-1",
		AccountID:  "acct-A",
		OccurredAt: t,
		ReceivedAt: t,
		Type:       store.EventTypeStart,
		Direction:  store.DirectionEgress,
		Protocol:   6,
		SourceIP:   "10.0.0.1",
		DestIP:     "10.0.0.2",
		SourcePort: 49152,
		DestPort:   443,
		RxBytes:    100,
		TxBytes:    200,
		RuleID:     []byte("rule-allow"),
	}
}

func TestS3_RequiresBucket(t *testing.T) {
	_, err := NewS3(context.Background(), S3Config{})
	assert.Error(t, err)
}

func TestS3_FlushesOnInterval(t *testing.T) {
	c := &captured{}
	srv := fakeS3(c)
	defer srv.Close()

	s := newS3SinkTo(t, srv.URL, S3Config{
		Bucket:           "test-bucket",
		FlushInterval:    50 * time.Millisecond,
		MaxEventsPerFile: 1000,
	})
	defer s.Close()

	require.NoError(t, s.Save(context.Background(), []*store.Event{sampleS3Event()}))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		done := len(c.keys) > 0
		c.mu.Unlock()
		if done {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	require.GreaterOrEqual(t, len(c.keys), 1, "interval flush did not fire")
}

func TestS3_FlushesOnMaxEvents(t *testing.T) {
	c := &captured{}
	srv := fakeS3(c)
	defer srv.Close()

	s := newS3SinkTo(t, srv.URL, S3Config{
		Bucket:           "test-bucket",
		FlushInterval:    time.Hour, // never on time
		MaxEventsPerFile: 3,
	})
	defer s.Close()

	for i := 0; i < 3; i++ {
		require.NoError(t, s.Save(context.Background(), []*store.Event{sampleS3Event()}))
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		done := len(c.keys) > 0
		c.mu.Unlock()
		if done {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	require.Len(t, c.keys, 1, "size threshold must trigger immediate flush")
}

func TestS3_ObjectKeyIsPartitioned(t *testing.T) {
	c := &captured{}
	srv := fakeS3(c)
	defer srv.Close()

	s := newS3SinkTo(t, srv.URL, S3Config{
		Bucket:           "test-bucket",
		Prefix:           "openzro/dev",
		FlushInterval:    30 * time.Millisecond,
		MaxEventsPerFile: 1000,
	})
	defer s.Close()

	require.NoError(t, s.Save(context.Background(), []*store.Event{sampleS3Event()}))
	time.Sleep(200 * time.Millisecond)

	c.mu.Lock()
	defer c.mu.Unlock()
	require.NotEmpty(t, c.keys)
	key := c.keys[0]

	assert.Contains(t, key, "openzro/dev/")
	assert.Contains(t, key, "year=2026/")
	assert.Contains(t, key, "month=04/")
	assert.Contains(t, key, "day=26/")
	assert.Contains(t, key, "account=acct-A/")
	assert.True(t, strings.HasSuffix(key, ".ndjson.gz"),
		"object name suffix is the format contract")
}

func TestS3_BodyIsGzippedNDJSON(t *testing.T) {
	c := &captured{}
	srv := fakeS3(c)
	defer srv.Close()

	s := newS3SinkTo(t, srv.URL, S3Config{
		Bucket:           "test-bucket",
		FlushInterval:    30 * time.Millisecond,
		MaxEventsPerFile: 1000,
	})
	defer s.Close()

	require.NoError(t, s.Save(context.Background(), []*store.Event{
		sampleS3Event(),
		sampleS3Event(),
	}))
	time.Sleep(200 * time.Millisecond)

	c.mu.Lock()
	defer c.mu.Unlock()
	require.NotEmpty(t, c.bodies)
	assert.Equal(t, "gzip", c.headers[0].Get("Content-Encoding"))
	assert.Equal(t, "application/x-ndjson", c.headers[0].Get("Content-Type"))

	gz, err := gzip.NewReader(bytes.NewReader(c.bodies[0]))
	require.NoError(t, err)
	plain, err := io.ReadAll(gz)
	require.NoError(t, err)

	lines := bytes.Split(bytes.TrimSpace(plain), []byte("\n"))
	require.Len(t, lines, 2, "two events → two NDJSON lines")
	var ev map[string]any
	require.NoError(t, json.Unmarshal(lines[0], &ev))

	assert.Equal(t, "peer-1", ev["peer_id"])
	assert.Equal(t, "acct-A", ev["account_id"])
	assert.Equal(t, "start", ev["type"])
	assert.Equal(t, "egress", ev["direction"])
	assert.Equal(t, "10.0.0.1", ev["source_ip"])
	assert.Equal(t, hex.EncodeToString([]byte("rule-allow")), ev["rule_id"])
}

func TestS3_SaveDoesNotBlockEvenWhenSlowSink(t *testing.T) {
	// Server takes 100ms per request — slow enough that Save floods
	// faster than the loop can drain. Save MUST NOT block; full
	// queue results in a (logged) drop, not back-pressure on the
	// caller.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newS3SinkTo(t, srv.URL, S3Config{
		Bucket:           "test-bucket",
		FlushInterval:    time.Hour,
		MaxEventsPerFile: 1,
		BufferSize:       4,
	})
	// Don't defer Close — slow server will keep upload busy for too
	// long. The loop and connections leak with the test process.

	start := time.Now()
	for i := 0; i < 1000; i++ {
		_ = s.Save(context.Background(), []*store.Event{sampleS3Event()})
	}
	elapsed := time.Since(start)
	// 1000 calls should take well under a second even if a few hit
	// channel pushes; the assertion is that we did not block waiting
	// on the slow upstream.
	assert.Less(t, elapsed, 500*time.Millisecond,
		"Save must be non-blocking — observed %v for 1000 calls", elapsed)
}
