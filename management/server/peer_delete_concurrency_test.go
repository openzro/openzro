package server

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// TestDeletePeer_ConcurrentAccountWrite_NoDeadlock guards the lock
// ordering inside DeletePeer's transaction.
//
// The account row is the hot spot: DeletePeer ends up updating it
// (IncrementNetworkSerial), and several reads on the way there take a
// shared lock on that very same row. Taking the shared lock first and
// upgrading to exclusive later is a deadlock waiting to happen — two
// transactions can both hold the shared lock and then both ask for the
// exclusive one. Postgres breaks the cycle by killing one of them with
// SQLSTATE 40P01, which surfaces to the API caller as a 500.
//
// This is not hypothetical: the dashboard's bulk delete fires one
// DELETE /api/peers/{id} per selected peer in parallel, and
// AcquireWriteLockByUID only serializes within a single process, so
// with more than one management replica those transactions overlap.
//
// The second goroutine here stands in for any other account-scoped
// write reaching the database at the same time. It deliberately holds
// the shared lock across the window in which DeletePeer needs the
// exclusive one, so the ordering violation is deterministic rather
// than a race the test would only lose sometimes.
func TestDeletePeer_ConcurrentAccountWrite_NoDeadlock(t *testing.T) {
	t.Setenv("OPENZRO_STORE_ENGINE", string(types.PostgresStoreEngine))

	manager, err := createManager(t)
	require.NoError(t, err)

	const (
		accountID = "test_account"
		adminUser = "account_creator"
		peerID    = "peer1"
	)

	ctx := context.Background()
	account := newAccountWithId(ctx, accountID, adminUser, "", false)
	account.Peers = map[string]*nbpeer.Peer{
		peerID: {
			ID:        peerID,
			AccountID: accountID,
			IP:        net.IP{1, 1, 1, 1},
			DNSLabel:  peerID + ".test",
		},
	}
	account.Groups["group1"] = &types.Group{
		ID:        "group1",
		AccountID: accountID,
		Name:      "Group1",
		Peers:     []string{peerID},
	}
	require.NoError(t, manager.Store.SaveAccount(ctx, account))

	// holdShared is how long the competing transaction keeps its shared
	// lock on the account row. It has to outlast DeletePeer's own walk
	// to the exclusive lock, which is a handful of indexed queries.
	const holdShared = 2 * time.Second

	var wg sync.WaitGroup
	var competingErr, deleteErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		competingErr = manager.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
			if _, err := tx.GetAccountSettings(ctx, store.LockingStrengthShare, accountID); err != nil {
				return err
			}
			time.Sleep(holdShared)
			return tx.IncrementNetworkSerial(ctx, store.LockingStrengthUpdate, accountID)
		})
	}()

	// Let the competing transaction take its shared lock first.
	time.Sleep(200 * time.Millisecond)

	wg.Add(1)
	go func() {
		defer wg.Done()
		deleteErr = manager.DeletePeer(ctx, accountID, peerID, adminUser)
	}()

	wg.Wait()

	require.NoError(t, deleteErr, "DeletePeer must queue behind a concurrent account write, not deadlock")
	require.NoError(t, competingErr, "the concurrent account write must not be the deadlock victim either")

	_, err = manager.GetPeer(ctx, accountID, peerID, adminUser)
	require.Error(t, err, "peer should be gone after a successful delete")
}
