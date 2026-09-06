//go:build archive_duckdb

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	flowArchive "github.com/openzro/openzro/flow/store/archive"
	flowExports "github.com/openzro/openzro/management/server/flow_exports"
	mgmtStore "github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// archiveConfigFromIntegrations reads the archive destination the operator
// configured in the dashboard, under Integrations, and returns it as a
// reader/writer config.
//
// This is the same row the management server reads at startup, decrypted
// with the same key, so the compaction command writes to the bucket the
// sink is writing to and with the credential the operator already gave
// it. The alternative was a second copy of the service-account key,
// living in a Kubernetes Secret and drifting from the console the moment
// anybody rotated one and not the other.
//
// It deliberately does not supply the HMAC pair. That belongs to reads:
// DuckDB reaches GCS through httpfs, whose secret accepts interoperability
// keys and rejects service-account JSON, so the pair has to come from the
// environment and configWithRuntimeEnv restores it there. The row has no
// field for it, and inventing one would mean a dashboard change to store a
// credential that only one consumer needs.
//
// Returns ok=false when nothing is configured, which is not an error: the
// caller falls back to whatever the flags and environment provide.
func archiveConfigFromIntegrations(ctx context.Context) (cfg flowArchive.Config, source string, ok bool, err error) {
	// No management config means this is not running beside a management
	// deployment -- a laptop, a debug container, a CI job. That is not a
	// failure, it is an absence: the caller falls through to its flags
	// and environment, and its own error names all three sources. Only a
	// config that exists and cannot be read is worth reporting, since
	// that is a real misconfiguration rather than a different context.
	if _, statErr := os.Stat(types.MgmtConfigPath); errors.Is(statErr, os.ErrNotExist) {
		return flowArchive.Config{}, "", false, nil
	}

	mgmtCfg, err := loadMgmtConfig(ctx, types.MgmtConfigPath)
	if err != nil {
		return flowArchive.Config{}, "", false, fmt.Errorf(
			"flow archive compact: read management config from %s: %w", types.MgmtConfigPath, err)
	}

	// skipMigration: this command reads one row and exits. Running
	// schema migrations from a CronJob that happens to start while the
	// server is mid-upgrade is a way to turn a maintenance task into an
	// outage.
	store, err := mgmtStore.NewStore(ctx, mgmtCfg.StoreConfig.Engine, mgmtCfg.Datadir, nil, true)
	if err != nil {
		return flowArchive.Config{}, "", false, fmt.Errorf("flow archive compact: open store: %w", err)
	}
	defer func() { _ = store.Close(ctx) }()

	sqlStore, isSQL := store.(*mgmtStore.SqlStore)
	if !isSQL {
		// The file store predates integrations and cannot hold a
		// flow_exports row, so there is nothing to read rather than
		// something broken.
		return flowArchive.Config{}, "", false, nil
	}

	exports, err := flowExports.NewStore(sqlStore.GetGormDB(), mgmtCfg.DataStoreEncryptionKey)
	if err != nil {
		return flowArchive.Config{}, "", false, fmt.Errorf("flow archive compact: flow_exports store: %w", err)
	}

	cfg, source, ok, err = flowExports.ArchiveConfigFromRows(ctx, exports)
	if err != nil {
		return flowArchive.Config{}, "", false, fmt.Errorf("flow archive compact: read archive config: %w", err)
	}
	return cfg, source, ok, nil
}
