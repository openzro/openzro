package store

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/openzro/openzro/management/server/types"
)

func TestSqlStore_MySQLExecuteInTransactionUsesReadCommitted(t *testing.T) {
	sqlStore := newMySQLIsolationTestStore(t)
	assertTransactionSeesConcurrentCommit(t, sqlStore, func(fn func(*gorm.DB) error) error {
		return sqlStore.ExecuteInTransaction(context.Background(), func(tx Store) error {
			return fn(tx.(*SqlStore).db)
		})
	})
}

func TestSqlStore_MySQLDirectTransactionUsesReadCommitted(t *testing.T) {
	sqlStore := newMySQLIsolationTestStore(t)
	assertTransactionSeesConcurrentCommit(t, sqlStore, func(fn func(*gorm.DB) error) error {
		return sqlStore.transaction(context.Background(), sqlStore.db, fn)
	})
}

func newMySQLIsolationTestStore(t *testing.T) *SqlStore {
	t.Helper()

	if types.Engine(strings.ToLower(os.Getenv("OPENZRO_STORE_ENGINE"))) != types.MysqlStoreEngine {
		t.Skip("MySQL isolation sentinel only runs in the mysql store job")
	}

	store, cleanup, err := NewTestStoreFromSQL(context.Background(), "", t.TempDir())
	require.NoError(t, err)
	t.Cleanup(cleanup)

	sqlStore, ok := store.(*SqlStore)
	require.True(t, ok, "mysql test store must be *SqlStore")
	return sqlStore
}

func assertTransactionSeesConcurrentCommit(t *testing.T, sqlStore *SqlStore, runTx func(func(*gorm.DB) error) error) {
	t.Helper()

	ctx := context.Background()
	accountID := xid.New().String()
	account := &types.Account{
		Id:      accountID,
		Network: types.NewNetwork(),
	}
	require.NoError(t, sqlStore.CreateAccount(ctx, account))

	err := runTx(func(tx *gorm.DB) error {
		first := readNetworkSerial(t, tx, accountID)

		require.NoError(t,
			sqlStore.db.Exec("UPDATE accounts SET network_serial = network_serial + 1 WHERE id = ?", accountID).Error,
			"the concurrent autocommit write should be visible to a READ COMMITTED transaction")

		second := readNetworkSerial(t, tx, accountID)
		require.Equal(t, first+1, second,
			"mysql store transactions must use READ COMMITTED; REPEATABLE READ would keep seeing %d", first)
		return nil
	})
	require.NoError(t, err)
}

func readNetworkSerial(t *testing.T, db *gorm.DB, accountID string) uint64 {
	t.Helper()

	var serial uint64
	require.NoError(t,
		db.Model(&types.Account{}).Select("network_serial").Where("id = ?", accountID).Scan(&serial).Error)
	return serial
}
