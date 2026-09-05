//go:build archive_duckdb

package archive

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// The GCS read path shipped asking DuckDB to `INSTALL gcs`, an extension
// that does not exist. Every dashboard query for archived flow events
// failed with:
//
//	install gcs: HTTP Error: Failed to download extension "gcs" at
//	http://extensions.duckdb.org/v1.1.3/linux_amd64/gcs.duckdb_extension.gz (404)
//
// GCS is served by httpfs, and its secret takes an HMAC key pair —
// service account JSON is rejected by the binder. Both halves are pinned
// here against a real DuckDB, because both were assumptions in the code
// that no test contradicted.
func TestGCSSecretAcceptsOnlyHMAC(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(ctx, "INSTALL httpfs")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "LOAD httpfs")
	require.NoError(t, err, "httpfs is what serves gcs:// — if this fails the read path has no transport")

	t.Run("there is no gcs extension to install", func(t *testing.T) {
		_, err := db.ExecContext(ctx, "INSTALL gcs")
		require.Error(t, err, "if DuckDB ever ships a gcs extension, revisit applyAuthGCS")
		require.ErrorContains(t, err, "gcs")
	})

	t.Run("httpfs alone accepts an HMAC gcs secret", func(t *testing.T) {
		_, err := db.ExecContext(ctx,
			`CREATE OR REPLACE SECRET probe_hmac (TYPE GCS, KEY_ID 'k', SECRET 's')`)
		require.NoError(t, err)
	})

	t.Run("service account json is rejected", func(t *testing.T) {
		_, err := db.ExecContext(ctx,
			`CREATE OR REPLACE SECRET probe_json (TYPE GCS, CREDENTIAL_CHAIN '{"type":"service_account"}')`)
		require.Error(t, err, "the old code built this statement on every query")
		require.ErrorContains(t, err, "credential_chain")
	})
}

// An operator who configured the bucket but not the interoperability
// keys should be told which variables to set before a dashboard query
// gets as far as building a broken DuckDB secret.
func TestGCSAuthRefusesWithoutHMACCredentials(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := &duckdbStore{cfg: Config{
		Provider:        "gcs",
		Bucket:          "example-bucket",
		CredentialsJSON: []byte(`{"type":"service_account"}`),
	}}

	err = store.applyAuthGCS(ctx, db)
	require.Error(t, err, "service account JSON cannot authenticate a read; saying so is the whole point")
	require.True(t, errors.Is(err, ErrMissingCredentials), "management downgrades this typed error to hot-only")
	require.ErrorContains(t, err, "OPENZRO_FLOW_ARCHIVE_GCS_HMAC_KEY_ID")
	require.ErrorContains(t, err, "OPENZRO_FLOW_ARCHIVE_GCS_HMAC_SECRET")
}
