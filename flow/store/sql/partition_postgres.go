// Postgres-specific schema management for flow_events.
//
// flow_events uses native Postgres declarative partitioning by month
// on the received_at column when running against Postgres. Other
// engines (SQLite, MySQL) keep the single-table model — partitioning
// in MySQL is non-declarative and bolts on extra operational
// burden, and SQLite has no native partitioning at all.
//
// The motivation: at scale (>= 1M events/day) the retention DELETE
// becomes the dominant cost — it touches every row older than the
// cutoff, fights autovacuum, and produces bloat that nothing
// reclaims short of a VACUUM FULL. Partitioning lets retention
// become DROP PARTITION (constant time, frees the disk
// immediately) and keeps each partition small enough that index
// scans stay hot in shared_buffers.
//
// This file is split off from sql.go so the cross-engine paths
// stay readable. The contract is:
//   - setupPostgresSchema is called by sql.New when the underlying
//     gorm dialector is "postgres".
//   - ensureFuturePartitions is called from the retention loop
//     (factory.runRetention) before every purge, to keep the
//     "current + next" coverage so writes never hit a missing
//     partition.
//   - dropOldPartitions replaces the row-level DELETE on Postgres,
//     also called from the retention loop.
package sql

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// monthlyKey returns the YYYY_MM suffix used to name a partition
// covering the calendar month of t (UTC).
func monthlyKey(t time.Time) string {
	return t.UTC().Format("2006_01")
}

// monthRange returns the [start, end) bounds of the calendar month
// containing t, normalised to UTC midnight.
func monthRange(t time.Time) (time.Time, time.Time) {
	t = t.UTC()
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	return start, end
}

// setupPostgresSchema converts an empty / non-existent flow_events
// into a partitioned table, creates the initial coverage of
// partitions (current month + look-ahead), and ensures all the
// supporting indexes exist on the parent.
//
// Migration policy for already-existing non-partitioned tables: at
// alpha-stage we DROP TABLE flow_events and recreate as partitioned.
// Once we have GA-blessed deployments holding production data,
// upgrade should grow into a copy-then-swap migration. Until then
// the loss is bounded to whatever flow events accumulated in the
// current retention window — replaceable from peer streams.
func setupPostgresSchema(db *gorm.DB) error {
	ctx := context.Background()

	exists, partitioned, err := inspectFlowEventsTable(db)
	if err != nil {
		return fmt.Errorf("inspect flow_events: %w", err)
	}

	if exists && !partitioned {
		// alpha-stage: drop and recreate. Safe because retention
		// windows are short (default 720h) and the source of truth
		// for flow events is the live peer stream, not the store.
		if err := db.WithContext(ctx).Exec(`DROP TABLE flow_events`).Error; err != nil {
			return fmt.Errorf("drop legacy non-partitioned flow_events: %w", err)
		}
		exists = false
	}

	if !exists {
		if err := createPartitionedFlowEvents(db); err != nil {
			return fmt.Errorf("create partitioned flow_events: %w", err)
		}
	}

	// Always make sure the next few months have partitions ready, so
	// inserts arriving across a month boundary at midnight don't fail.
	now := time.Now().UTC()
	if err := ensureFuturePartitions(db, now, 3); err != nil {
		return fmt.Errorf("ensure future partitions: %w", err)
	}
	return nil
}

// inspectFlowEventsTable answers two questions in one round-trip:
// does the relation exist, and if so is it the partitioned parent?
// (information_schema.tables returns "BASE TABLE" for both, so we
// have to reach into pg_partitioned_table to disambiguate.)
func inspectFlowEventsTable(db *gorm.DB) (exists bool, partitioned bool, err error) {
	var n int64
	if err := db.Raw(`
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_name = 'flow_events'
	`).Scan(&n).Error; err != nil {
		return false, false, err
	}
	if n == 0 {
		return false, false, nil
	}

	var partN int64
	if err := db.Raw(`
		SELECT COUNT(*) FROM pg_partitioned_table pt
		JOIN pg_class c ON c.oid = pt.partrelid
		WHERE c.relname = 'flow_events'
	`).Scan(&partN).Error; err != nil {
		return true, false, err
	}
	return true, partN > 0, nil
}

// createPartitionedFlowEvents builds the parent table + the indexes
// the dashboard depends on. The DDL mirrors the GORM struct tags in
// sql.go; we deliberately spell it out here instead of letting GORM
// AutoMigrate do it because GORM does not know how to express
// `PARTITION BY RANGE` and would fall back to a normal table.
func createPartitionedFlowEvents(db *gorm.DB) error {
	stmts := []string{
		`CREATE TABLE flow_events (
			id              BIGSERIAL,
			event_id        BYTEA,
			flow_id         BYTEA,
			peer_public_key BYTEA,
			is_initiator    BOOLEAN,
			account_id      VARCHAR(64) NOT NULL,
			peer_id         VARCHAR(64) NOT NULL,
			occurred_at     TIMESTAMPTZ NOT NULL,
			received_at     TIMESTAMPTZ NOT NULL,
			type            SMALLINT,
			direction       SMALLINT,
			protocol        INTEGER,
			source_ip       VARCHAR(45),
			dest_ip         VARCHAR(45),
			source_port     BIGINT,
			dest_port       BIGINT,
			icmp_type       INTEGER,
			icmp_code       INTEGER,
			rx_packets      BIGINT,
			tx_packets      BIGINT,
			rx_bytes        BIGINT,
			tx_bytes        BIGINT,
			rule_id         BYTEA,
			source_resource BYTEA,
			dest_resource   BYTEA,
			PRIMARY KEY (id, received_at)
		) PARTITION BY RANGE (received_at)`,

		// Indexes mirroring the gorm tags in row{}. They land on the
		// parent and Postgres propagates them to every partition.
		`CREATE INDEX idx_flow_account_received
		   ON flow_events (account_id, received_at DESC)`,
		`CREATE INDEX idx_flow_account_peer_received
		   ON flow_events (account_id, peer_id, received_at DESC)`,
		`CREATE INDEX idx_flow_received
		   ON flow_events (received_at)`,
		`CREATE INDEX idx_flow_rule
		   ON flow_events (rule_id) WHERE rule_id IS NOT NULL`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			return fmt.Errorf("DDL %q: %w",
				strings.SplitN(strings.TrimSpace(s), "\n", 2)[0], err)
		}
	}
	return nil
}

// ensureFuturePartitions creates the partition covering the current
// month plus `monthsAhead` more, using IF NOT EXISTS so it's safe to
// call repeatedly. Idempotent.
func ensureFuturePartitions(db *gorm.DB, anchor time.Time, monthsAhead int) error {
	for i := 0; i <= monthsAhead; i++ {
		t := time.Date(anchor.Year(), anchor.Month()+time.Month(i), 1, 0, 0, 0, 0, time.UTC)
		start, end := monthRange(t)
		name := "flow_events_" + monthlyKey(start)
		ddl := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s
			   PARTITION OF flow_events
			   FOR VALUES FROM ('%s') TO ('%s')`,
			name,
			start.Format("2006-01-02 15:04:05+00"),
			end.Format("2006-01-02 15:04:05+00"),
		)
		if err := db.Exec(ddl).Error; err != nil {
			return fmt.Errorf("create partition %s: %w", name, err)
		}
	}
	return nil
}

// dropOldPartitions drops every partition whose upper bound is at or
// before cutoff. Returns the count of dropped partitions (NOT row
// count — DROP PARTITION doesn't surface that and counting rows
// before the drop would defeat the purpose).
//
// Used by the retention loop on Postgres. Drastically faster than
// DELETE WHERE received_at < cutoff because it returns the disk
// immediately and never produces dead tuples for autovacuum to chase.
func dropOldPartitions(db *gorm.DB, cutoff time.Time) (int, error) {
	type partInfo struct {
		Name    string
		UpperUn string // pg_get_expr(partbound) text — we parse it cheaply
	}
	var rows []partInfo
	if err := db.Raw(`
		SELECT c.relname AS name, pg_get_expr(c.relpartbound, c.oid) AS upper_un
		FROM pg_class c
		JOIN pg_inherits i ON i.inhrelid = c.oid
		JOIN pg_class p ON p.oid = i.inhparent
		WHERE p.relname = 'flow_events'
	`).Scan(&rows).Error; err != nil {
		return 0, fmt.Errorf("list partitions: %w", err)
	}

	dropped := 0
	for _, r := range rows {
		// Bound text looks like:
		//   FOR VALUES FROM ('2026-04-01 00:00:00+00') TO ('2026-05-01 00:00:00+00')
		// We just need the TO bound — split on `TO ('` and take what
		// follows up to the next `'`.
		idx := strings.Index(r.UpperUn, "TO ('")
		if idx < 0 {
			continue
		}
		tail := r.UpperUn[idx+len("TO ('"):]
		end := strings.IndexByte(tail, '\'')
		if end < 0 {
			continue
		}
		boundary, err := time.Parse("2006-01-02 15:04:05-07", tail[:end])
		if err != nil {
			boundary, err = time.Parse("2006-01-02 15:04:05+00", tail[:end])
			if err != nil {
				continue
			}
		}
		if !boundary.After(cutoff) {
			if err := db.Exec("DROP TABLE IF EXISTS " + r.Name).Error; err != nil {
				return dropped, fmt.Errorf("drop partition %s: %w", r.Name, err)
			}
			dropped++
		}
	}
	return dropped, nil
}
