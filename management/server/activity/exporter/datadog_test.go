package exporter

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

	"github.com/openzro/openzro/management/server/activity"
)

// TestDatadog_SiteResolution verifies the site preset → URL mapping
// matches Datadog's documented sites. Boot-time errors here are
// silent (operator points at the wrong region and 401s for hours)
// so we lock the mapping with a test.
func TestDatadog_SiteResolution(t *testing.T) {
	cases := []struct {
		site string
		want string
	}{
		{"us1", "https://http-intake.logs.datadoghq.com/api/v2/logs"},
		{"US1", "https://http-intake.logs.datadoghq.com/api/v2/logs"},
		{"", "https://http-intake.logs.datadoghq.com/api/v2/logs"},
		{"eu1", "https://http-intake.logs.datadoghq.eu/api/v2/logs"},
		{"us3", "https://http-intake.logs.us3.datadoghq.com/api/v2/logs"},
		{"us5", "https://http-intake.logs.us5.datadoghq.com/api/v2/logs"},
		{"ap1", "https://http-intake.logs.ap1.datadoghq.com/api/v2/logs"},
	}
	for _, tc := range cases {
		t.Run(tc.site, func(t *testing.T) {
			got, err := resolveDatadogIntake(DatadogConfig{Site: tc.site})
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("unknown_site_errors", func(t *testing.T) {
		_, err := resolveDatadogIntake(DatadogConfig{Site: "moon"})
		require.Error(t, err)
	})

	t.Run("explicit_url_overrides_site", func(t *testing.T) {
		got, err := resolveDatadogIntake(DatadogConfig{
			Site: "us1",
			URL:  "https://datadog-proxy.internal",
		})
		require.NoError(t, err)
		assert.Equal(t, "https://datadog-proxy.internal/api/v2/logs", got)
	})
}

// TestDatadog_RequiresAPIKey locks in that the exporter refuses to
// boot without credentials. Otherwise it would silently drop events
// since the intake 401s every batch.
func TestDatadog_RequiresAPIKey(t *testing.T) {
	_, err := NewDatadog(DatadogConfig{})
	require.Error(t, err)
}

// TestDatadog_Export_BatchAndShape verifies events flow through the
// queue, hit the intake as a JSON array with DD-API-KEY auth, and
// carry the expected fields. Uses an httptest server so no real
// network or credentials are needed.
func TestDatadog_Export_BatchAndShape(t *testing.T) {
	var (
		gotMu    sync.Mutex
		gotBody  []byte
		gotKey   string
		received int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotMu.Lock()
		gotBody = append([]byte(nil), body...)
		gotKey = r.Header.Get("DD-API-KEY")
		gotMu.Unlock()
		atomic.AddInt32(&received, 1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	exp, err := NewDatadog(DatadogConfig{
		URL:           srv.URL,
		APIKey:        "secret",
		Service:       "openzro-test",
		Source:        "openzro",
		Tags:          "env:test,team:sec",
		BatchSize:     2,
		FlushInterval: 500 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = exp.Close() })

	now := time.Now().UTC()
	require.NoError(t, exp.Export(context.Background(), &activity.Event{
		ID:             1,
		Timestamp:      now,
		Activity:       activity.PeerAdmissionDenied,
		InitiatorID:    "u1",
		InitiatorName:  "Alice",
		InitiatorEmail: "alice@example.test",
		TargetID:       "peer-1",
		AccountID:      "acct-1",
		Meta:           map[string]any{"reason": "non-compliant", "check_type": "EndpointSecurityCheck"},
	}))
	require.NoError(t, exp.Export(context.Background(), &activity.Event{
		ID:        2,
		Timestamp: now,
		Activity:  activity.AdmissionEnforcementEnabled,
		AccountID: "acct-1",
	}))

	// Batch fills at 2; flush is sync within the loop. Wait briefly.
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&received) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	require.GreaterOrEqual(t, atomic.LoadInt32(&received), int32(1))

	gotMu.Lock()
	defer gotMu.Unlock()
	assert.Equal(t, "secret", gotKey)

	var entries []map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &entries))
	require.Len(t, entries, 2)

	first := entries[0]
	assert.Equal(t, "openzro-test", first["service"])
	assert.Equal(t, "openzro", first["ddsource"])
	assert.Equal(t, "env:test,team:sec", first["ddtags"])
	assert.Equal(t, "acct-1", first["hostname"])
	usr, ok := first["usr"].(map[string]any)
	require.True(t, ok, "usr should be present when initiator fields are set")
	assert.Equal(t, "u1", usr["id"])
	assert.Equal(t, "alice@example.test", usr["email"])
	oz, ok := first["openzro"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "peer.admission.deny", oz["activity"])
	meta, ok := oz["meta"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "non-compliant", meta["reason"])
}

// TestDatadog_BatchSizeFloor caps BatchSize at Datadog's 1000
// per-request limit. Operator setting 5000 should silently floor,
// not crash.
func TestDatadog_BatchSizeFloor(t *testing.T) {
	exp, err := NewDatadog(DatadogConfig{
		Site:      "us1",
		APIKey:    "secret",
		BatchSize: 5000,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = exp.Close() })
	assert.Equal(t, 1000, exp.cfg.BatchSize)
}
