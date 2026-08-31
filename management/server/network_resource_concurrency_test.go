package server

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/xid"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/groups"
	"github.com/openzro/openzro/management/server/networks/resources"
	resourceTypes "github.com/openzro/openzro/management/server/networks/resources/types"
	networkTypes "github.com/openzro/openzro/management/server/networks/types"
	"github.com/openzro/openzro/management/server/permissions"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// CreateResource rejects a name already taken in the account. It proves that
// absence with a read (manager.go:116) and then writes on the strength of it,
// which holds only as long as nothing else can insert the same name in between.
//
// AcquireWriteLockByUID makes that true inside one process and false across
// two, which is the deployment this project ships. Postgres has no gap locks —
// predicate locking only exists at SERIALIZABLE — so the shared-lock read
// locks the rows that exist, and there are none. Two replicas can both prove
// absence and both insert.
//
// The window is made deterministic rather than raced for. The name check at
// manager.go:116 runs *before* the transaction takes the network row
// exclusively at manager.go:121, so a third transaction holding that row parks
// both replicas after they have each decided the name is free. Releasing it
// lets both insert.
//
// This is the demonstration half of #143 step 3: the duplicate has to be shown
// before it is fixed, so the fix lands red to green rather than by argument.
//
// The two engines do not fail the same way, and running this on the wrong one
// would look like proof it does not need fixing. Measured before the unique
// index existed:
//
//	Postgres   FAIL — two rows with the same name. No gap locks; predicate
//	           locking only exists at SERIALIZABLE, so the shared-lock read
//	           locks the rows that exist, and there are none.
//	MySQL      pass — InnoDB's gap locks under REPEATABLE READ hold the
//	           position the row would occupy and refuse the second insert.
//	           The loser sees a 1213 deadlock rather than a name conflict.
//
// So the red is Postgres-only, and the assertion below is what both engines
// must satisfy afterwards: exactly one row survives, whichever way the database
// got there.
func TestNetworkResource_ConcurrentCreateAcrossReplicas(t *testing.T) {
	const (
		accountID    = "resource-race-account"
		userID       = "resource-race-user"
		networkID    = "resource-race-network"
		resourceName = "contested-name"
	)

	r := newTwoReplicas(t)
	ctx := context.Background()

	account := newAccountWithId(ctx, accountID, userID, "", false)
	require.NoError(t, r.A.Store.SaveAccount(ctx, account))
	require.NoError(t, r.A.Store.SaveNetwork(ctx, store.LockingStrengthUpdate, &networkTypes.Network{
		ID:        networkID,
		AccountID: accountID,
		Name:      "race-network",
	}))

	// One resources manager per replica, over that replica's own store, so the
	// two sides share nothing but the database.
	managerFor := func(am *DefaultAccountManager) resources.Manager {
		perms := permissions.NewManager(am.Store)
		return resources.NewManager(am.Store, perms, groups.NewManager(am.Store, perms, am), am)
	}
	resA, resB := managerFor(r.A), managerFor(r.B)

	// Hold the network row so both replicas pile up behind it *after* each has
	// decided the name is free. Generous on purpose: the name check is a single
	// indexed lookup, so both are through it long before this releases, and
	// releasing early is the failure mode that would make this pass for the
	// wrong reason.
	blockerReady := make(chan struct{})
	blockerDone := make(chan error, 1)
	go func() {
		blockerDone <- r.A.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
			if _, err := tx.GetNetworkByID(ctx, store.LockingStrengthUpdate, accountID, networkID); err != nil {
				return err
			}
			close(blockerReady)
			time.Sleep(2 * time.Second)
			return nil
		})
	}()
	select {
	case <-blockerReady:
	case err := <-blockerDone:
		// The blocker failed before it could signal. Without this the test
		// would sit on blockerReady until the package timeout, naming nothing.
		t.Fatalf("blocking transaction failed before taking the network row: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("blocking transaction never took the network row")
	}

	b := newBarrier()
	create := func(mgr resources.Manager) <-chan error {
		errCh := make(chan error, 1)
		go func() {
			b.wait(t)
			_, err := mgr.CreateResource(ctx, userID, &resourceTypes.NetworkResource{
				ID:        xid.New().String(),
				AccountID: accountID,
				NetworkID: networkID,
				Name:      resourceName,
				Type:      resourceTypes.NetworkResourceType("domain"),
				Address:   "example.internal",
				Enabled:   true,
			})
			errCh <- err
		}()
		return errCh
	}
	errA, errB := create(resA), create(resB)

	join := func(name string, ch <-chan error) error {
		t.Helper()
		select {
		case err := <-ch:
			return err
		case <-time.After(30 * time.Second):
			t.Fatalf("replica %s never returned from CreateResource", name)
			return nil
		}
	}
	// Both sides are joined before anything is asserted, so a failure on one
	// cannot leave the other running into the next assertion.
	resultA, resultB := join("A", errA), join("B", errB)
	require.NoError(t, <-blockerDone)

	accepted := 0
	for _, err := range []error{resultA, resultB} {
		if err == nil {
			accepted++
		}
	}

	// Measured from committed state through the other replica's store, not from
	// what either side believes it did.
	stored, err := r.B.Store.GetNetworkResourcesByNetID(ctx, store.LockingStrengthNone, accountID, networkID)
	require.NoError(t, err)

	named := 0
	for _, res := range stored {
		if res.Name == resourceName {
			named++
		}
	}

	require.Equal(t, 1, named,
		"the account holds %d resources named %q; CreateResource accepted %d of 2 concurrent creates (A=%v, B=%v)",
		named, resourceName, accepted, resultA, resultB)
	require.Equal(t, 1, accepted, "exactly one create must be accepted")

	// The loser's error is engine-specific, so assert it where it is
	// deterministic. On Postgres the insert reaches the unique index and the
	// store maps the violation to the same answer the pre-check gives. On
	// MySQL the gap lock fires first and the loser sees a 1213 deadlock
	// instead — see the note at the top of this file.
	if types.Engine(strings.ToLower(os.Getenv("OPENZRO_STORE_ENGINE"))) == types.PostgresStoreEngine {
		loser := resultA
		if loser == nil {
			loser = resultB
		}
		require.ErrorContains(t, loser, "already exists",
			"the losing create must be reported as a taken name, not an internal error")
	}
}
