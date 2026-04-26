// Package sql implements flow/store.Store on top of GORM, supporting
// PostgreSQL, MySQL, and SQLite via the same code path. The driver is
// chosen by the DSN scheme; see flow/store/factory.
//
// Schema is intentionally simple: one wide table indexed for the
// query patterns the dashboard exposes. There is no native
// partitioning at this layer — Purge runs a DELETE WHERE
// received_at < ?. For deployments where that DELETE becomes a
// problem, ADR-0002 plans a Postgres-specific partitioning
// migration; until then the simple model is fast enough for the
// small/medium tier and identical across drivers.
package sql

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/openzro/openzro/flow/store"
)

// row is the GORM-managed shape. Field types are deliberately
// driver-agnostic: BLOB ↔ []byte and TEXT ↔ string, no INET / JSONB
// so the same migrations work on SQLite for dev, Postgres for
// production, and MySQL if anyone wants it.
type row struct {
	ID            uint64    `gorm:"primaryKey;autoIncrement"`
	EventID       []byte    `gorm:"size:32"`
	FlowID        []byte    `gorm:"size:32"`
	PeerPublicKey []byte
	IsInitiator   bool

	AccountID  string    `gorm:"size:64;not null;index:idx_flow_account_received,priority:1"`
	PeerID     string    `gorm:"size:64;not null;index:idx_flow_account_peer_received,priority:2"`
	OccurredAt time.Time `gorm:"not null"`
	ReceivedAt time.Time `gorm:"not null;index:idx_flow_account_received,priority:2,sort:desc;index:idx_flow_account_peer_received,priority:3,sort:desc;index:idx_flow_received"`

	Type      uint8
	Direction uint8
	Protocol  uint16

	SourceIP   string `gorm:"size:45"`
	DestIP     string `gorm:"size:45"`
	SourcePort uint32
	DestPort   uint32

	ICMPType uint16
	ICMPCode uint16

	RxPackets uint64
	TxPackets uint64
	RxBytes   uint64
	TxBytes   uint64

	RuleID         []byte `gorm:"index:idx_flow_rule"`
	SourceResource []byte
	DestResource   []byte
}

// TableName pins the table to a stable name regardless of GORM's
// pluralization defaults (which would be "rows").
func (row) TableName() string { return "flow_events" }

// Store is a GORM-backed flow.store.Store.
type Store struct {
	db *gorm.DB
}

// New wires a Store on top of an existing GORM DB. The caller owns
// the connection — this lets operators choose to share the management
// DB or use a separate one. AutoMigrate runs on construction.
func New(db *gorm.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("flow/store/sql: db is required")
	}
	if err := db.AutoMigrate(&row{}); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Save inserts a batch using GORM's CreateInBatches, which uses a
// single multi-VALUES INSERT per chunk. 500 is a balance between
// fewer roundtrips and the parameter limit of common drivers.
func (s *Store) Save(ctx context.Context, events []*store.Event) error {
	if len(events) == 0 {
		return nil
	}
	rows := make([]row, len(events))
	for i, ev := range events {
		rows[i] = toRow(ev)
	}
	return s.db.WithContext(ctx).CreateInBatches(rows, 500).Error
}

// Query returns events matching the filter, ordered by received_at desc.
func (s *Store) Query(ctx context.Context, f store.Filter) ([]*store.Event, error) {
	if f.AccountID == "" {
		return nil, errors.New("flow/store/sql: AccountID is required for Query")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}

	q := s.db.WithContext(ctx).Model(&row{}).Where("account_id = ?", f.AccountID)
	if f.PeerID != "" {
		q = q.Where("peer_id = ?", f.PeerID)
	}
	if f.SourceIP != "" {
		q = q.Where("source_ip = ?", f.SourceIP)
	}
	if f.DestIP != "" {
		q = q.Where("dest_ip = ?", f.DestIP)
	}
	if f.Protocol != nil {
		q = q.Where("protocol = ?", *f.Protocol)
	}
	if f.Type != nil {
		q = q.Where("type = ?", uint8(*f.Type))
	}
	if f.Direction != nil {
		q = q.Where("direction = ?", uint8(*f.Direction))
	}
	if len(f.RuleID) > 0 {
		q = q.Where("rule_id = ?", f.RuleID)
	}
	if !f.Since.IsZero() {
		q = q.Where("received_at >= ?", f.Since)
	}
	if !f.Until.IsZero() {
		q = q.Where("received_at <= ?", f.Until)
	}

	var rows []row
	if err := q.Order("received_at DESC").Offset(f.Offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*store.Event, len(rows))
	for i := range rows {
		ev := fromRow(&rows[i])
		out[i] = &ev
	}
	return out, nil
}

// Purge removes events older than the cutoff. Returns the row count
// deleted. Implementations with declarative partitioning may
// override this with a DROP PARTITION; for now it is a single DELETE.
func (s *Store) Purge(ctx context.Context, olderThan time.Time) (int64, error) {
	res := s.db.WithContext(ctx).Where("received_at < ?", olderThan).Delete(&row{})
	return res.RowsAffected, res.Error
}

// Close is a no-op — the GORM DB is owned by the caller. The factory
// closes it when shutting down.
func (s *Store) Close() error { return nil }

func toRow(e *store.Event) row {
	return row{
		EventID:        e.EventID,
		FlowID:         e.FlowID,
		PeerPublicKey:  e.PeerPublicKey,
		IsInitiator:    e.IsInitiator,
		AccountID:      e.AccountID,
		PeerID:         e.PeerID,
		OccurredAt:     e.OccurredAt,
		ReceivedAt:     e.ReceivedAt,
		Type:           uint8(e.Type),
		Direction:      uint8(e.Direction),
		Protocol:       e.Protocol,
		SourceIP:       e.SourceIP,
		DestIP:         e.DestIP,
		SourcePort:     e.SourcePort,
		DestPort:       e.DestPort,
		ICMPType:       e.ICMPType,
		ICMPCode:       e.ICMPCode,
		RxPackets:      e.RxPackets,
		TxPackets:      e.TxPackets,
		RxBytes:        e.RxBytes,
		TxBytes:        e.TxBytes,
		RuleID:         e.RuleID,
		SourceResource: e.SourceResource,
		DestResource:   e.DestResource,
	}
}

func fromRow(r *row) store.Event {
	return store.Event{
		EventID:        r.EventID,
		FlowID:         r.FlowID,
		PeerPublicKey:  r.PeerPublicKey,
		IsInitiator:    r.IsInitiator,
		AccountID:      r.AccountID,
		PeerID:         r.PeerID,
		OccurredAt:     r.OccurredAt,
		ReceivedAt:     r.ReceivedAt,
		Type:           store.EventType(r.Type),
		Direction:      store.Direction(r.Direction),
		Protocol:       r.Protocol,
		SourceIP:       r.SourceIP,
		DestIP:         r.DestIP,
		SourcePort:     r.SourcePort,
		DestPort:       r.DestPort,
		ICMPType:       r.ICMPType,
		ICMPCode:       r.ICMPCode,
		RxPackets:      r.RxPackets,
		TxPackets:      r.TxPackets,
		RxBytes:        r.RxBytes,
		TxBytes:        r.TxBytes,
		RuleID:         r.RuleID,
		SourceResource: r.SourceResource,
		DestResource:   r.DestResource,
	}
}
