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

// seedEventParquet writes one row for the default test account.
func seedEventParquet(t *testing.T, db *sql.DB, path, ts string) {
	t.Helper()
	seedEventParquetAs(t, db, path, ts, testAccount)
}

// testAccount is the account the partition helpers file objects under.
const testAccount = "acct-1"

// seedEventParquetAs writes one row carrying every column the reader
// selects, so a failure under test is the query and not a missing
// column. accountID is a parameter because the account in the row and
// the account in the path are separate facts -- objects written before
// #186 disagree on them, and that disagreement is what the isolation
// test needs to reproduce.
//
// The column types mirror flow/sinks/parquet.go's parquetEvent, which
// matters for more than tidiness: type and direction are written as
// strings there ("start", "ingress"), and a fixture that wrote them as
// integers would let a broken string predicate pass. The fixture this
// grew out of had exactly that defect.
//
// ts is a DuckDB timestamp literal body, e.g. "2026-05-12 10:00:00".
func seedEventParquetAs(t *testing.T, db *sql.DB, path, ts, accountID string) {
	t.Helper()
	seedTypedAs(t, db, path, ts, accountID, "start", "ingress")
}

// seedTyped writes a row with a chosen type and direction, for the
// filters that compare them.
func seedTyped(t *testing.T, db *sql.DB, path, ts, typ, dir string) {
	t.Helper()
	seedTypedAs(t, db, path, ts, testAccount, typ, dir)
}

func seedTypedAs(t *testing.T, db *sql.DB, path, ts, accountID, typ, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	_, err := db.ExecContext(context.Background(), "COPY (SELECT "+
		"TIMESTAMP '"+ts+"' AS received_at, "+
		"TIMESTAMP '"+ts+"' AS occurred_at, "+
		"'"+accountID+"' AS account_id, 'peer-a' AS peer_id, "+
		"'ev-1' AS event_id, 'fl-1' AS flow_id, "+
		"'"+typ+"' AS type, '"+dir+"' AS direction, "+
		"6::UINTEGER AS protocol, "+
		"'10.0.0.1' AS source_ip, '10.0.0.2' AS dest_ip, "+
		"1024::UINTEGER AS source_port, 443::UINTEGER AS dest_port, "+
		"0::UINTEGER AS icmp_type, 0::UINTEGER AS icmp_code, "+
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
	return filepath.Join(root, "year="+year, "month="+month, "day="+day, "account="+testAccount, file)
}

// partitionGlob is what parquetURL produces for that layout.
func partitionGlob(root string) string {
	return filepath.Join(root, "year=*", "month=*", "day=*", "account="+testAccount, "*.parquet")
}
