// Package factory builds a flow store from environment variables.
//
// Operators select the engine via OPENZRO_FLOW_STORE_ENGINE. The DSN
// is provided in OPENZRO_FLOW_STORE_DSN and is interpreted by the
// chosen engine. Default engine is "none" — meaning the management
// process accepts FlowEvents on the gRPC stream but does not persist
// them; reliance is on the streaming exporter / cold archive instead.
//
// See ADR-0002 §"HOT tier" for the rationale.
package factory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	gormsqlite "github.com/glebarez/sqlite"
	log "github.com/sirupsen/logrus"
	gormmysql "gorm.io/driver/mysql"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/openzro/openzro/flow/store"
	sqlstore "github.com/openzro/openzro/flow/store/sql"
)

const (
	envEngine    = "OPENZRO_FLOW_STORE_ENGINE"
	envDSN       = "OPENZRO_FLOW_STORE_DSN"
	envRetention = "OPENZRO_FLOW_RETENTION"
)

// Built holds the constructed store and any goroutines (retention
// purge) that the caller is responsible for stopping at shutdown.
type Built struct {
	Store     store.Store
	Retention time.Duration
	close     func() error
}

// Close stops the retention loop (if running) and releases the store.
func (b *Built) Close() error {
	if b == nil || b.close == nil {
		return nil
	}
	return b.close()
}

// NewFromEnv reads OPENZRO_FLOW_STORE_* and constructs the configured
// store. Returns (nil, nil) when engine is unset or "none" — the
// caller treats nil as "persistence disabled".
func NewFromEnv(ctx context.Context) (*Built, error) {
	engine := strings.ToLower(strings.TrimSpace(os.Getenv(envEngine)))
	if engine == "" || engine == "none" {
		return nil, nil
	}

	dsn := os.Getenv(envDSN)
	if dsn == "" {
		return nil, fmt.Errorf("%s=%q requires %s to be set", envEngine, engine, envDSN)
	}

	retention := parseRetention(os.Getenv(envRetention))

	db, err := openDB(engine, dsn)
	if err != nil {
		return nil, fmt.Errorf("flow store: open %s: %w", engine, err)
	}

	s, err := sqlstore.New(db)
	if err != nil {
		return nil, fmt.Errorf("flow store: %w", err)
	}

	stopCh := make(chan struct{})
	go runRetention(ctx, s, retention, stopCh)

	log.WithContext(ctx).Infof(
		"flow store: %s engine, retention=%s", engine, retention)

	// MySQL and SQLite do not get the native PARTITION BY RANGE schema
	// that the Postgres path uses (see ADR-0002). Retention falls back
	// to row-level DELETE, which scales with row count and does NOT
	// reclaim disk on its own — autovacuum / OPTIMIZE TABLE chase the
	// bloat after the fact. At >1M flow events/day this is operationally
	// painful; Postgres is strongly recommended for production.
	if engine == "mysql" || engine == "sqlite" {
		log.WithContext(ctx).Warnf(
			"flow store engine=%s lacks native partitioning — retention "+
				"will use row-level DELETE which scales poorly past ~1M "+
				"events/day. Postgres is recommended for production "+
				"(see ADR-0002).", engine)
	}

	return &Built{
		Store:     s,
		Retention: retention,
		close: func() error {
			close(stopCh)
			return s.Close()
		},
	}, nil
}

// openDB dispatches on engine name. The clickhouse case is reserved
// for a follow-up commit per ADR-0002 PR-G; today it returns an error
// pointing the operator at the supported set.
func openDB(engine, dsn string) (*gorm.DB, error) {
	gormCfg := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	}
	switch engine {
	case "postgres", "postgresql":
		return gorm.Open(gormpostgres.Open(dsn), gormCfg)
	case "mysql":
		return gorm.Open(gormmysql.Open(dsn), gormCfg)
	case "sqlite":
		return gorm.Open(gormsqlite.Open(dsn), gormCfg)
	case "clickhouse":
		return nil, errors.New("clickhouse engine is not yet implemented (see ADR-0002 PR-G)")
	default:
		return nil, fmt.Errorf("unknown engine %q (supported: postgres, mysql, sqlite)", engine)
	}
}

func parseRetention(raw string) time.Duration {
	const def = 7 * 24 * time.Hour
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		log.Warnf("flow store: invalid %s=%q, using default %s", envRetention, raw, def)
		return def
	}
	return d
}

// runRetention runs the daily purge loop. The first sweep happens
// after a short delay so process startup is not blocked by a long
// DELETE; subsequent sweeps are 24h apart. We deliberately do not
// expose the interval as an env var — making purge less frequent than
// daily would let retention drift, and more frequent than daily is
// pure CPU spend with no operator benefit.
func runRetention(ctx context.Context, s store.Store, retention time.Duration, stop <-chan struct{}) {
	const interval = 24 * time.Hour
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-timer.C:
			cutoff := time.Now().UTC().Add(-retention)
			deleted, err := s.Purge(ctx, cutoff)
			if err != nil {
				log.WithContext(ctx).Errorf(
					"flow store retention purge failed: %v", err)
			} else if deleted > 0 {
				log.WithContext(ctx).Infof(
					"flow store retention purge: removed %d events older than %s",
					deleted, cutoff.Format(time.RFC3339))
			}
			timer.Reset(interval)
		}
	}
}
