package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// SaveDNSZone writes the account row (IncrementNetworkSerial) and also reads it
// under a shared lock, to resolve the peer DNS domain the overlap check needs.
// Taking the shared lock first and upgrading later deadlocks as soon as a
// second account-scoped write overlaps: both transactions hold the shared lock,
// both then ask for the exclusive one, and the database kills one of them
// (Postgres SQLSTATE 40P01), which reaches the API caller as a 500.
//
// Same defect and same shape as the one fixed for DeletePeer in #146; see
// peer_delete_concurrency_test.go. The competing goroutine stands in for any
// other account-scoped write reaching the database at the same time, and holds
// its shared lock across the window in which the mutation needs the exclusive
// one, so the ordering violation is deterministic rather than a race the test
// would only sometimes lose.
//
// CreateDNSZone carries the same defect and is deliberately not covered here —
// see the pull request for why it is held back.
func TestDNSZones_SaveConcurrentAccountWrite_NoDeadlock(t *testing.T) {
	t.Setenv("OPENZRO_STORE_ENGINE", string(types.PostgresStoreEngine))

	// holdShared has to outlast the mutation's own walk to the exclusive lock.
	const holdShared = 2 * time.Second

	am, zoneID := initDNSZoneTestAccountWithZone(t)
	ctx := context.Background()

	zone, err := am.GetDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, zoneID)
	require.NoError(t, err)

	var wg sync.WaitGroup
	var competingErr, saveErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		competingErr = am.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
			if _, err := tx.GetAccountSettings(ctx, store.LockingStrengthShare, dnsZoneTestAccountID); err != nil {
				return err
			}
			time.Sleep(holdShared)
			return tx.IncrementNetworkSerial(ctx, store.LockingStrengthUpdate, dnsZoneTestAccountID)
		})
	}()

	// Let the competing transaction take its shared lock first.
	time.Sleep(200 * time.Millisecond)

	wg.Add(1)
	go func() {
		defer wg.Done()
		zone.Name = "renamed-zone"
		_, saveErr = am.SaveDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, zone)
	}()

	wg.Wait()

	require.NoError(t, saveErr, "SaveDNSZone must queue behind a concurrent account write, not deadlock")
	require.NoError(t, competingErr, "the concurrent account write must not be the deadlock victim either")
}
