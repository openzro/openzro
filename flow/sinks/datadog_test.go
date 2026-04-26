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

// TestDatadog_SiteResolution mirrors the activity exporter's matching
// test — locks the site → URL mapping so a typo never silently
// 401s for hours.
func TestDatadog_SiteResolution(t *testing.T) {
	cases := []struct {
		site string
		want string
	}{
		{"us1", "https://http-intake.logs.datadoghq.com/api/v2/logs"},
		{"", "https://http-intake.logs.datadoghq.com/api/v2/logs"},
		{"eu1", "https://http-intake.logs.datadoghq.eu/api/v2/logs"},
	}
	for _, tc := range cases {
		t.Run(tc.site, func(t *testing.T) {
			got, err := resolveDatadogFlowIntake(DatadogConfig{Site: tc.site})
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
	_, err := resolveDatadogFlowIntake(DatadogConfig{Site: "moon"})
	require.Error(t, err, "unknown site must error so a misconfigured exporter never silently drops")
}

// TestDatadog_RequiresAPIKey locks in the boot-time refusal — without
// auth the intake 401s every batch.
func TestDatadog_RequiresAPIKey(t *testing.T) {
	_, err := NewDatadog(DatadogConfig{})
	require.Error(t, err)
}

// TestDatadog_BatchSizeFloor caps BatchSize at Datadog's 1000-per-
// request hard limit. Operator setting 5000 silently floors instead
// of producing 413s in production.
func TestDatadog_BatchSizeFloor(t *testing.T) {
	exp, err := NewDatadog(DatadogConfig{Site: "us1", APIKey: "secret", BatchSize: 5000})
	require.NoError(t, err)
	t.Cleanup(func() { _ = exp.Close() })
	assert.Equal(t, 1000, exp.cfg.BatchSize)
}

// TestDatadog_Save_ShipsBatchAsNPMShape verifies the wire shape: a
// single flow event arrives at the receiver as one log entry under
// the network.* namespace with bytes_read / bytes_written / transport
// keys (Datadog NPM canonical), plus the openzro_flow extras and a
// readable `message` line for the Logs Explorer.
func TestDatadog_Save_ShipsBatchAsNPMShape(t *testing.T) {
	var (
		gotMu   sync.Mutex
		gotBody []byte
		gotKey  string
		recvCnt int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotMu.Lock()
		gotBody = append([]byte(nil), body...)
		gotKey = r.Header.Get("DD-API-KEY")
		gotMu.Unlock()
		atomic.AddInt32(&recvCnt, 1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	exp, err := NewDatadog(DatadogConfig{
		URL:           srv.URL,
		APIKey:        "secret",
		Service:       "openzro-flow-test",
		Tags:          "env:test",
		BatchSize:     1,
		FlushInterval: 100 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = exp.Close() })

	now := time.Now().UTC()
	require.NoError(t, exp.Save(context.Background(), []*store.Event{{
		EventID:    []byte{0xde, 0xad, 0xbe, 0xef},
		FlowID:     []byte{0xca, 0xfe},
		PeerID:     "peer-1",
		AccountID:  "acct-1",
		OccurredAt: now,
		ReceivedAt: now,
		Type:       store.EventTypeStart,
		Direction:  store.DirectionIngress,
		Protocol:   6, // tcp
		SourceIP:   "10.0.0.1",
		DestIP:     "10.0.0.2",
		SourcePort: 49152,
		DestPort:   443,
		RxBytes:    1500,
		TxBytes:    100,
	}}))

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&recvCnt) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	require.GreaterOrEqual(t, atomic.LoadInt32(&recvCnt), int32(1))

	gotMu.Lock()
	defer gotMu.Unlock()
	assert.Equal(t, "secret", gotKey)

	var entries []map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &entries))
	require.Len(t, entries, 1)

	e := entries[0]
	assert.Equal(t, "openzro-flow-test", e["service"])
	assert.Equal(t, "env:test", e["ddtags"])
	assert.Equal(t, "acct-1", e["host"])
	assert.Contains(t, e["message"], "10.0.0.1:49152")
	assert.Contains(t, e["message"], "10.0.0.2:443")

	net, ok := e["network"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "tcp", net["transport"])
	cli, ok := net["client"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "10.0.0.1", cli["ip"])

	oz, ok := e["openzro_flow"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "peer-1", oz["peer_id"])
	assert.Equal(t, "deadbeef", oz["event_id"])
	assert.Equal(t, "start", oz["type"])
}

// TestProtocolName proves the IANA protocol number mapping. Not
// exhaustive — locks the protocols Datadog NPM cares about plus the
// fallback shape so unknowns are still searchable.
func TestProtocolName(t *testing.T) {
	cases := map[uint16]string{
		0:    "",
		1:    "icmp",
		6:    "tcp",
		17:   "udp",
		47:   "gre",
		58:   "icmpv6",
		1234: "proto-1234",
	}
	for in, want := range cases {
		t.Run(want, func(t *testing.T) {
			assert.Equal(t, want, protocolName(in))
		})
	}
}
