//go:build archive_duckdb

package archive

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// seedEventParquet writes one row carrying every column the reader
// selects, so a failure under test is the query and not a missing
// column. ts is a DuckDB timestamp literal body, e.g. "2026-05-12
// 10:00:00".
func seedEventParquet(t *testing.T, db *sql.DB, path, ts string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	_, err := db.ExecContext(context.Background(), "COPY (SELECT "+
		"TIMESTAMP '"+ts+"' AS received_at, "+
		"TIMESTAMP '"+ts+"' AS occurred_at, "+
		"'acct-1' AS account_id, 'peer-a' AS peer_id, "+
		"'ev-1' AS event_id, 'fl-1' AS flow_id, "+
		"1::UTINYINT AS type, 1::UTINYINT AS direction, "+
		"6::USMALLINT AS protocol, "+
		"'10.0.0.1' AS source_ip, '10.0.0.2' AS dest_ip, "+
		"1024::UINTEGER AS source_port, 443::UINTEGER AS dest_port, "+
		"0::UTINYINT AS icmp_type, 0::UTINYINT AS icmp_code, "+
		"true AS is_initiator, "+
		"1::UBIGINT AS rx_packets, 2::UBIGINT AS tx_packets, "+
		"10::UBIGINT AS rx_bytes, 20::UBIGINT AS tx_bytes, "+
		"'' AS rule_id, '' AS source_resource_id, '' AS dest_resource_id"+
		") TO '"+path+"' (FORMAT PARQUET)")
	require.NoError(t, err)
}

// partitionPath builds the layout flow/sinks writes: zero-padded
// year/month/day above the account.
func partitionPath(root, year, month, day, file string) string {
	return filepath.Join(root, "year="+year, "month="+month, "day="+day, "account=acct-1", file)
}

// partitionGlob is what parquetURL produces for that layout.
func partitionGlob(root string) string {
	return filepath.Join(root, "year=*", "month=*", "day=*", "account=acct-1", "*.parquet")
}
