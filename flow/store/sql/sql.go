// Package sql implements flow/store.Store on top of GORM, supporting
// PostgreSQL, MySQL, and SQLite via the same code path. The driver is
// chosen by the DSN scheme; see flow/store/factory.
//
// Postgres uses native declarative partitioning by month on the
// received_at column — flow_events is the parent, monthly children
// (flow_events_2026_05, ...) cover [first-of-month, first-of-next).
// Schema management lives in partition_postgres.go; retention is
// DROP PARTITION instead of row-level DELETE.
//
// MySQL and SQLite keep the single-table model — partitioning in
// MySQL is non-declarative and adds operational burden, SQLite has
// no native partitioning at all. For those engines Purge still does
// DELETE WHERE received_at < cutoff, which is fast enough at the
// small/medium tier.
package sql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/openzro/openzro/flow/store"
)

// row is the GORM-managed shape. Field types are deliberately
// driver-agnostic: BLOB ↔ []byte and TEXT ↔ string, no INET / JSONB
// so the same migrations work on SQLite for dev, Postgres for
// production, and MySQL if anyone wants it.
type row struct {
	ID            uint64 `gorm:"primaryKey;autoIncrement"`
	EventID       []byte `gorm:"size:32"`
	FlowID        []byte `gorm:"size:32"`
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
// DB or use a separate one.
//
// Schema management dispatches on the dialect:
//   - postgres: native declarative partitioning by month on
//     received_at (see partition_postgres.go). Retention drops old
//     partitions in O(1) instead of DELETE-ing rows.
//   - mysql / sqlite / others: AutoMigrate the single flow_events
//     table; retention is row-level DELETE.
func New(db *gorm.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("flow/store/sql: db is required")
	}
	if isPostgres(db) {
		if err := setupPostgresSchema(db); err != nil {
			return nil, err
		}
	} else {
		if err := db.AutoMigrate(&row{}); err != nil {
			return nil, err
		}
	}
	return &Store{db: db}, nil
}

// isPostgres returns true when the GORM dialector is Postgres-compatible.
// Used to gate the partitioning code paths so MySQL/SQLite stay on the
// vanilla single-table model.
func isPostgres(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	return db.Dialector.Name() == "postgres"
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
	if f.SourcePort != nil {
		q = q.Where("source_port = ?", *f.SourcePort)
	}
	if f.DestPort != nil {
		q = q.Where("dest_port = ?", *f.DestPort)
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

// Purge removes events older than the cutoff and on Postgres also
// extends the partition coverage forward so writes never hit a
// missing partition.
//
//   - Postgres: drops every monthly partition whose upper bound is
//     at or before the cutoff (constant time per partition; frees
//     disk immediately) and creates the next 3 months of partitions
//     ahead of `now`. Returns the partition drop count, NOT a row
//     count — DROP PARTITION does not surface row counts.
//   - Other dialects: row-level DELETE WHERE received_at < cutoff,
//     returns the affected rows.
//
// Callers (the retention loop in flow/store/factory) treat the
// return as a "did something happen" signal, not as a precise
// volume metric — keeping this asymmetry contained in the engine.
func (s *Store) Purge(ctx context.Context, olderThan time.Time) (int64, error) {
	if isPostgres(s.db) {
		// Keep the partition lookahead fresh on every retention
		// pass — cheap if-not-exists DDL, ensures the cron-style
		// loop also handles month-boundary creation without a
		// dedicated scheduler.
		if err := ensureFuturePartitions(s.db, time.Now().UTC(), 3); err != nil {
			return 0, fmt.Errorf("ensure future partitions: %w", err)
		}
		dropped, err := dropOldPartitions(s.db, olderThan)
		return int64(dropped), err
	}
	res := s.db.WithContext(ctx).Where("received_at < ?", olderThan).Delete(&row{})
	return res.RowsAffected, res.Error
}

// Close releases the underlying *sql.DB. Safe to call multiple times.
//
// The GORM DB is conceptually owned by the caller, but we still pull
// its *sql.DB out and close it here so callers don't have to thread
// the gorm handle separately. Closing is essential on Windows: the
// SQLite file lock isn't released until the *sql.DB is closed, and
// any subsequent t.TempDir / os.RemoveAll on the parent directory
// will fail with "the process cannot access the file because it is
// being used by another process".
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

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
