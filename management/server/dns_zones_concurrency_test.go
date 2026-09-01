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

// Moving the exclusive account lock ahead of the overlap read in CreateDNSZone
// changes the order it takes rows in: account row first, then a shared read of
// every zone in the account. SaveDNSZone and DeleteDNSRecord go the other way —
// the zone row first, the account row second — and #148 is the standing lesson
// that reordering one path against the others trades one deadlock for another.
//
// So this drives the inversion directly rather than racing for it. A
// transaction takes the zone row exclusively and holds it across the window in
// which CreateDNSZone needs to read the zones, then reaches for the account
// row that CreateDNSZone is holding. If the two orders conflict, one of them
// dies.
func TestDNSZones_CreateAgainstZoneFirstWriter_NoDeadlock(t *testing.T) {
	t.Setenv("OPENZRO_STORE_ENGINE", string(types.PostgresStoreEngine))

	// holdZone has to outlast CreateDNSZone's own walk to the zone reads.
	const holdZone = 2 * time.Second

	am, zoneID := initDNSZoneTestAccountWithZone(t)
	ctx := context.Background()

	// Deadlines rather than a WaitGroup, because the failure this test exists
	// to catch is one side never returning. A regression that blocks without
	// the database killing either transaction would hang here until the
	// package timeout and report as a timeout somewhere else entirely.
	competingCh := make(chan error, 1)
	go func() {
		competingCh <- am.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
			// The order SaveDNSZone and DeleteDNSRecord take: zone row first.
			if _, err := tx.GetDNSZoneByID(ctx, store.LockingStrengthUpdate, dnsZoneTestAccountID, zoneID); err != nil {
				return err
			}
			time.Sleep(holdZone)
			return tx.IncrementNetworkSerial(ctx, store.LockingStrengthUpdate, dnsZoneTestAccountID)
		})
	}()

	// Let the competing transaction take the zone row first.
	time.Sleep(200 * time.Millisecond)

	createCh := make(chan error, 1)
	go func() {
		_, err := am.CreateDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, &types.DNSZone{
			Name:               "second-zone",
			Domain:             "other.example",
			Enabled:            true,
			DistributionGroups: []types.DNSZoneGroup{{GroupID: dnsZoneTestGroupID}},
		})
		createCh <- err
	}()

	join := func(name string, ch <-chan error) error {
		t.Helper()
		select {
		case err := <-ch:
			return err
		case <-time.After(30 * time.Second):
			t.Fatalf("%s never returned; it is blocked, which is the failure this test watches for", name)
			return nil
		}
	}
	createErr := join("CreateDNSZone", createCh)
	competingErr := join("the zone-first writer", competingCh)

	require.NoError(t, createErr, "CreateDNSZone must queue behind a zone-first writer, not deadlock")
	require.NoError(t, competingErr, "the zone-first writer must not be the deadlock victim either")
}
