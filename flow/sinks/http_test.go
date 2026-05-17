package sinks

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/flow/store"
)

// sampleEvent is the shared *store.Event fixture declared in
// elastic_test.go (package-level test helper). It uses DirectionEgress.

// TestHTTP_RequiresURL locks the boot-time refusal — a sink with no
// destination would silently swallow every flow event.
func TestHTTP_RequiresURL(t *testing.T) {
	_, err := NewHTTP(HTTPConfig{})
	require.Error(t, err)
}

// TestHTTP_Save_ShipsJSONArrayWithHeaders verifies the generic wire
// contract: one POST, Content-Type application/json, the operator's
// custom headers forwarded verbatim (this is where auth lives), and
// the body a JSON array of flat openzro-native event objects.
func TestHTTP_Save_ShipsJSONArrayWithHeaders(t *testing.T) {
	var (
		mu      sync.Mutex
		gotBody []byte
		gotCT   string
		gotAuth string
		recvCnt int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = append([]byte(nil), body...)
		gotCT = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()
		atomic.AddInt32(&recvCnt, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exp, err := NewHTTP(HTTPConfig{
		URL:           srv.URL,
		Headers:       map[string]string{"Authorization": "Bearer s3cr3t"},
		BatchSize:     1,
		FlushInterval: 100 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = exp.Close() })

	require.NoError(t, exp.Save(context.Background(), []*store.Event{sampleEvent()}))

	waitForCount(t, &recvCnt, 1)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "application/json", gotCT)
	assert.Equal(t, "Bearer s3cr3t", gotAuth)

	var entries []map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &entries))
	require.Len(t, entries, 1)
	e := entries[0]
	assert.Equal(t, "peer-1", e["peer_id"])
	assert.Equal(t, "acct-1", e["account_id"])
	assert.Equal(t, "deadbeef", e["event_id"])
	assert.Equal(t, "start", e["type"])
	assert.Equal(t, "egress", e["direction"])
	assert.Equal(t, "tcp", e["protocol"])
	assert.Equal(t, "10.0.0.1", e["source_ip"])
	assert.EqualValues(t, 443, e["dest_port"])
}

// TestHTTP_Save_BatchesMultiple proves several events coalesce into a
// single POST once BatchSize is reached — the volume contract that
// keeps a high-traffic fleet from issuing one request per flow.
func TestHTTP_Save_BatchesMultiple(t *testing.T) {
	var (
		mu      sync.Mutex
		lens    []int
		recvCnt int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var entries []map[string]any
		_ = json.Unmarshal(body, &entries)
		mu.Lock()
		lens = append(lens, len(entries))
		mu.Unlock()
		atomic.AddInt32(&recvCnt, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exp, err := NewHTTP(HTTPConfig{
		URL:           srv.URL,
		BatchSize:     3,
		FlushInterval: time.Hour, // force the size trigger, not the timer
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = exp.Close() })

	evs := []*store.Event{sampleEvent(), sampleEvent(), sampleEvent()}
	require.NoError(t, exp.Save(context.Background(), evs))

	waitForCount(t, &recvCnt, 1)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, lens, 1)
	assert.Equal(t, 3, lens[0])
}

// TestHTTP_RetriesThenSucceeds proves bounded retry with backoff: the
// receiver 500s twice then 200s, and with MaxAttempts=3 the batch is
// delivered rather than dropped. This is the behavior that justifies
// HTTPDestConfig carrying MaxAttempts/InitialBackoff at all (the
// Datadog sink is best-effort single-shot; the generic sink retries).
func TestHTTP_RetriesThenSucceeds(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exp, err := NewHTTP(HTTPConfig{
		URL:            srv.URL,
		BatchSize:      1,
		FlushInterval:  50 * time.Millisecond,
		MaxAttempts:    3,
		InitialBackoff: 5 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = exp.Close() })

	require.NoError(t, exp.Save(context.Background(), []*store.Event{sampleEvent()}))

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&attempts) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	assert.GreaterOrEqual(t, atomic.LoadInt32(&attempts), int32(3))
}

// TestHTTP_DropsAfterMaxAttempts proves the retry is bounded — a
// permanently failing endpoint gets exactly MaxAttempts tries then the
// batch is dropped, and crucially Close() returns promptly instead of
// being wedged behind an unbounded retry loop (the back-pressure
// contract: a dead destination must not stall the drainer forever).
func TestHTTP_DropsAfterMaxAttempts(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	exp, err := NewHTTP(HTTPConfig{
		URL:            srv.URL,
		BatchSize:      1,
		FlushInterval:  50 * time.Millisecond,
		MaxAttempts:    2,
		InitialBackoff: 5 * time.Millisecond,
	})
	require.NoError(t, err)

	require.NoError(t, exp.Save(context.Background(), []*store.Event{sampleEvent()}))

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&attempts) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	assert.Equal(t, int32(2), atomic.LoadInt32(&attempts), "must try exactly MaxAttempts then give up")

	done := make(chan struct{})
	go func() { _ = exp.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close() blocked — retry loop is not bounded/abortable")
	}
}

// TestHTTP_4xxIsNotRetried locks the poison-response posture: a
// permanent config error (401/404) must NOT be retried per batch even
// with MaxAttempts>1 — otherwise a misconfigured receiver becomes a
// retry storm that stalls the single drainer. Mirrors the activity
// HTTP webhook's deliberate 4xx handling.
func TestHTTP_4xxIsNotRetried(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusUnauthorized) // 401 — permanent
	}))
	defer srv.Close()

	exp, err := NewHTTP(HTTPConfig{
		URL:            srv.URL,
		BatchSize:      1,
		FlushInterval:  50 * time.Millisecond,
		MaxAttempts:    3,
		InitialBackoff: 5 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = exp.Close() })

	require.NoError(t, exp.Save(context.Background(), []*store.Event{sampleEvent()}))

	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts),
		"4xx must be delivered-once-then-dropped, never retried")
}

// TestHTTP_RejectsInvalidURL fails fast at construction so an
// env-configured typo does not silently fail every flush forever.
func TestHTTP_RejectsInvalidURL(t *testing.T) {
	for _, bad := range []string{
		"", "   ", "notaurl", "ftp://host/x", "http://", "://nohost",
	} {
		_, err := NewHTTP(HTTPConfig{URL: bad})
		assert.Error(t, err, "must reject %q", bad)
	}
}

// TestHTTP_LogURLStripsCredentials proves the log endpoint never
// carries userinfo / path / query — an operator who put a secret in
// the URL must not see it echoed to logs (CLAUDE.md: no secrets in
// logs).
func TestHTTP_LogURLStripsCredentials(t *testing.T) {
	exp, err := NewHTTP(HTTPConfig{
		URL: "https://user:s3cr3t@collector.example:9200/ingest?token=abc",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = exp.Close() })
	assert.Equal(t, "https://collector.example:9200", exp.logURL)
}

// TestHTTP_Save_NonBlockingDropsOnFullQueue locks the hot-path
// contract from store.go: Save MUST NOT block past a small bounded
// buffer. A wedged destination + a tiny buffer must still return
// immediately (drop, log), never back-pressure the gRPC fan-out.
func TestHTTP_Save_NonBlockingDropsOnFullQueue(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // wedge the destination
	}))
	defer srv.Close()
	defer close(block)

	exp, err := NewHTTP(HTTPConfig{
		URL:           srv.URL,
		BatchSize:     1000,
		FlushInterval: time.Hour,
		BufferSize:    2,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = exp.Close() })

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			_ = exp.Save(context.Background(), []*store.Event{sampleEvent()})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Save blocked on a full queue — violates the non-blocking Sink contract")
	}
}

// TestParseHTTPHeaders locks the env-var header syntax so an operator
// fat-fingering the format gets predictable behavior, not a silent
// missing-auth-header 401 loop.
func TestParseHTTPHeaders(t *testing.T) {
	cases := []struct {
		in   string
		want map[string]string
	}{
		{"", map[string]string{}},
		{"Authorization: Bearer abc", map[string]string{"Authorization": "Bearer abc"}},
		{
			"X-A: 1, X-B: two words",
			map[string]string{"X-A": "1", "X-B": "two words"},
		},
		{"Malformed-No-Colon", map[string]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, parseHTTPHeaders(tc.in))
		})
	}
}

func waitForCount(t *testing.T, c *int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(c) < want && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	require.GreaterOrEqual(t, atomic.LoadInt32(c), want)
}
