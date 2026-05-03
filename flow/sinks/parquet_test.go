package sinks

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/flow/store"
)

// TestEncodeBatchParquet_RoundTrip locks in the wire-format contract:
// the schema we emit must match the column names operators query against
// in DuckDB / Athena / BigQuery, and reading the bytes back must
// reproduce every field. Catches accidental tag drift on parquetEvent
// since renaming a column is a breaking change per ADR-0012.
func TestEncodeBatchParquet_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	ev := &store.Event{
		EventID:        []byte{0x01, 0x02, 0x03, 0x04},
		FlowID:         []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		PeerID:         "peer-alice",
		AccountID:      "acct-1",
		IsInitiator:    true,
		OccurredAt:     now,
		ReceivedAt:     now.Add(50 * time.Millisecond),
		Type:           store.EventTypeStart,
		Direction:      store.DirectionIngress,
		Protocol:       6,
		SourceIP:       "100.65.0.10",
		DestIP:         "100.65.0.40",
		SourcePort:     51322,
		DestPort:       22,
		ICMPType:       0,
		ICMPCode:       0,
		RxPackets:      12,
		TxPackets:      8,
		RxBytes:        4096,
		TxBytes:        2048,
		RuleID:         []byte{0x10, 0x20, 0x30},
		SourceResource: []byte{0x40},
		DestResource:   []byte{0x50, 0x60},
	}

	body, err := encodeBatchParquet([]*store.Event{ev})
	require.NoError(t, err)
	require.NotEmpty(t, body)

	r := parquet.NewGenericReader[parquetEvent](bytes.NewReader(body))
	defer r.Close()
	rows := make([]parquetEvent, r.NumRows())
	n, err := r.Read(rows)
	// parquet-go returns io.EOF together with the last N rows when the
	// buffer is sized to the file's row count — that's a successful
	// terminal read, not an error.
	if err != nil && !errors.Is(err, io.EOF) {
		require.NoError(t, err)
	}
	require.Equal(t, 1, n)

	got := rows[0]
	assert.Equal(t, "acct-1", got.AccountID)
	assert.Equal(t, "peer-alice", got.PeerID)
	assert.Equal(t, hex.EncodeToString(ev.EventID), got.EventID)
	assert.Equal(t, hex.EncodeToString(ev.FlowID), got.FlowID)
	assert.Equal(t, "start", got.Type)
	assert.Equal(t, "ingress", got.Direction)
	assert.Equal(t, uint32(6), got.Protocol)
	assert.Equal(t, "100.65.0.10", got.SourceIP)
	assert.Equal(t, "100.65.0.40", got.DestIP)
	assert.Equal(t, uint32(51322), got.SourcePort)
	assert.Equal(t, uint32(22), got.DestPort)
	assert.Equal(t, uint64(4096), got.RxBytes)
	assert.Equal(t, uint64(2048), got.TxBytes)
	assert.Equal(t, hex.EncodeToString(ev.RuleID), got.RuleID)
	assert.Equal(t, hex.EncodeToString(ev.SourceResource), got.SourceResourceID)
	assert.Equal(t, hex.EncodeToString(ev.DestResource), got.DestResourceID)
	assert.True(t, got.IsInitiator)

	// Timestamp round-trip: parquet-go stores as microsecond resolution
	// per the schema tag. Compare on truncated values to avoid
	// nanosecond noise from time.Now().
	assert.True(t, got.OccurredAt.Equal(ev.OccurredAt),
		"occurred_at: got %v, want %v", got.OccurredAt, ev.OccurredAt)
	assert.True(t, got.ReceivedAt.Equal(ev.ReceivedAt),
		"received_at: got %v, want %v", got.ReceivedAt, ev.ReceivedAt)
}

// TestEncodeBatchParquet_EmptyBatch documents that empty batches are
// rejected at the encoder layer — the upload loop is supposed to gate
// these out before reaching us, but a defensive error here saves the
// next refactor from producing zero-row Parquet files in the bucket.
func TestEncodeBatchParquet_EmptyBatch(t *testing.T) {
	_, err := encodeBatchParquet(nil)
	assert.Error(t, err)
}

// TestResolveFormat exercises the env-var parser. Empty / unknown
// values must fall back to NDJSON for back-compat per ADR-0012.
func TestResolveFormat(t *testing.T) {
	cases := []struct {
		in   string
		want archiveFormat
	}{
		{"", formatNDJSON},
		{"ndjson", formatNDJSON},
		{"parquet", formatParquet},
		{"PARQUET", formatNDJSON}, // case-sensitive; doc'd
		{"avro", formatNDJSON},    // future format we don't support
	}
	for _, tc := range cases {
		got := resolveFormat(tc.in)
		assert.Equal(t, tc.want, got, "input=%q", tc.in)
	}
}
