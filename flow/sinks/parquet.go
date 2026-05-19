// Package sinks — Parquet encoder shared by the S3 and GCS archive
// sinks (per ADR-0012). The schema mirrors the JSON wire shape from
// `toJSONEvent` in s3.go field-for-field so the same operator-side
// queries (DuckDB / Athena / BigQuery / Spark) read both formats with
// the same column names. Renames are forbidden without a major
// release + the re-emit migration tool ADR-0012 § "Format default
// and migration" reserves.
//
// Schema choices:
//
//   - Timestamps as TIMESTAMP(MICROS, UTC) — DuckDB / Athena / Spark
//     all read this natively without coercion.
//   - The four bytes-typed identifiers (event_id, flow_id, rule_id,
//     source_resource_id, dest_resource_id) are stored as their hex
//     STRING representation, identical to the NDJSON shape. Storing
//     raw BYTES would be marginally smaller but breaks the "same
//     query, both formats" promise.
//   - All numeric counters are uint64 — Parquet's Snappy compression
//     handles the high cardinality fine, and matches the wire proto.
//   - Compression is SNAPPY at the row-group level: it is the most
//     widely supported codec in DuckDB / Athena / BigQuery without
//     extra config.
package sinks

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/snappy"

	"github.com/openzro/openzro/flow/store"
)

// archiveFormat names a serialization layout the S3/GCS sinks know
// how to emit. Operators pick the format via OPENZRO_FLOW_ARCHIVE_FORMAT;
// the read-side federation in flow/store/archive recognizes Parquet
// only (NDJSON is kept for back-compat write-only).
type archiveFormat string

const (
	formatNDJSON  archiveFormat = "ndjson"
	formatParquet archiveFormat = "parquet"
)

// resolveFormat normalizes an operator-supplied format string. Empty
// or unknown values fall back to NDJSON for back-compat — existing
// operators who never set the env var keep emitting the format their
// downstream tooling already consumes.
func resolveFormat(s string) archiveFormat {
	switch archiveFormat(s) {
	case formatParquet:
		return formatParquet
	default:
		return formatNDJSON
	}
}

// parquetEvent is the on-disk row shape. Field order is intentional:
// the high-cardinality time + identity columns come first so they
// land at the front of each row group's statistics block, where
// DuckDB's predicate pushdown checks them.
//
// Tags map to the same snake_case names the JSON shape uses so a
// single operator-side query reads both formats. `parquet:` tags use
// the parquet-go schema language: `optional` lets MySQL-side rows
// with nullable counters round-trip cleanly, `timestamp` pins
// TIMESTAMP(MICROS), `string` is `BYTE_ARRAY (UTF8)`.
type parquetEvent struct {
	ReceivedAt time.Time `parquet:"received_at,timestamp(microsecond),optional"`
	OccurredAt time.Time `parquet:"occurred_at,timestamp(microsecond),optional"`
	AccountID  string    `parquet:"account_id"`
	PeerID     string    `parquet:"peer_id"`
	EventID    string    `parquet:"event_id"`
	FlowID     string    `parquet:"flow_id"`

	Type      string `parquet:"type"`
	Direction string `parquet:"direction"`
	Protocol  uint32 `parquet:"protocol"`

	SourceIP   string `parquet:"source_ip"`
	DestIP     string `parquet:"dest_ip"`
	SourcePort uint32 `parquet:"source_port"`
	DestPort   uint32 `parquet:"dest_port"`
	ICMPType   uint32 `parquet:"icmp_type"`
	ICMPCode   uint32 `parquet:"icmp_code"`

	IsInitiator bool `parquet:"is_initiator"`

	RxPackets uint64 `parquet:"rx_packets"`
	TxPackets uint64 `parquet:"tx_packets"`
	RxBytes   uint64 `parquet:"rx_bytes"`
	TxBytes   uint64 `parquet:"tx_bytes"`

	RuleID           string `parquet:"rule_id"`
	SourceResourceID string `parquet:"source_resource_id"`
	DestResourceID   string `parquet:"dest_resource_id"`
}

// toParquetEvent flattens the in-memory event onto the on-disk shape.
// Mirrors toJSONEvent so the column set is byte-identical between
// formats.
func toParquetEvent(e *store.Event) parquetEvent {
	return parquetEvent{
		ReceivedAt:       e.ReceivedAt.UTC(),
		OccurredAt:       e.OccurredAt.UTC(),
		AccountID:        e.AccountID,
		PeerID:           e.PeerID,
		EventID:          hex.EncodeToString(e.EventID),
		FlowID:           hex.EncodeToString(e.FlowID),
		Type:             typeString(e.Type),
		Direction:        dirString(e.Direction),
		Protocol:         uint32(e.Protocol),
		SourceIP:         e.SourceIP,
		DestIP:           e.DestIP,
		SourcePort:       e.SourcePort,
		DestPort:         e.DestPort,
		ICMPType:         uint32(e.ICMPType),
		ICMPCode:         uint32(e.ICMPCode),
		IsInitiator:      e.IsInitiator,
		RxPackets:        e.RxPackets,
		TxPackets:        e.TxPackets,
		RxBytes:          e.RxBytes,
		TxBytes:          e.TxBytes,
		RuleID:           hex.EncodeToString(e.RuleID),
		SourceResourceID: hex.EncodeToString(e.SourceResource),
		DestResourceID:   hex.EncodeToString(e.DestResource),
	}
}

// encodeBatchParquet writes the batch as a single Parquet file with
// SNAPPY compression. Returns the file bytes ready to PUT to the
// object store. The schema is derived from parquetEvent's struct
// tags so adding a column upstream is a one-line change here.
func encodeBatchParquet(batch []*store.Event) ([]byte, error) {
	if len(batch) == 0 {
		return nil, fmt.Errorf("flow sink parquet: empty batch")
	}

	rows := make([]parquetEvent, len(batch))
	for i, ev := range batch {
		rows[i] = toParquetEvent(ev)
	}

	var buf bytes.Buffer
	w := parquet.NewGenericWriter[parquetEvent](
		&buf,
		parquet.Compression(&snappy.Codec{}),
	)
	if _, err := w.Write(rows); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("flow sink parquet: write rows: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("flow sink parquet: close writer: %w", err)
	}
	return buf.Bytes(), nil
}
