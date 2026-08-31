package server

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

// No DNS zone in an account may be a label-aligned suffix of another —
// validateDNSZone rejects the overlap because the agent's local resolver
// flattens records across zones without zone affinity, so two overlapping
// zones break which one is authoritative for a name.
//
// The check is a read followed by a write, and the only thing between them is
// AcquireWriteLockByUID, a mutex inside one process. With two replicas both
// reads can see an account with no overlapping zone, and both writes land.
//
// This is the fifth of the six invariants in #143 step 3, and the first of the
// two whose mechanism cannot be a unique index: the predicate is suffix
// overlap, not equality.
func TestDNSZones_ConcurrentOverlappingCreateAcrossReplicas(t *testing.T) {
	const (
		accountID = "dns-overlap-acc"
		userID    = "dns-overlap-user"
		groupID   = "dns-overlap-group"
		parent    = "overlap.example"
		child     = "sub.overlap.example"
	)

	r := newTwoReplicas(t)
	ctx := context.Background()

	account := newAccountWithId(ctx, accountID, userID, "tenant.example", false)
	account.Groups[groupID] = &types.Group{ID: groupID, Name: "overlap-group"}
	require.NoError(t, r.A.Store.SaveAccount(ctx, account))

	// Warm each replica: the barrier aligns the calls, not the moment each
	// reaches the database, and a cold pool offsets one past the other.
	for _, am := range []*DefaultAccountManager{r.A, r.B} {
		_, err := am.ListDNSZones(ctx, accountID, userID)
		require.NoError(t, err)
	}

	align := newBarrier()
	create := func(am *DefaultAccountManager, domain string) <-chan error {
		errCh := make(chan error, 1)
		go func() {
			align.wait(t)
			_, err := am.CreateDNSZone(ctx, accountID, userID, &types.DNSZone{
				Name:               domain,
				Domain:             domain,
				Enabled:            true,
				DistributionGroups: []types.DNSZoneGroup{{GroupID: groupID}},
			})
			errCh <- err
		}()
		return errCh
	}

	errA, errB := create(r.A, parent), create(r.B, child)

	join := func(name string, ch <-chan error) error {
		t.Helper()
		select {
		case err := <-ch:
			return err
		case <-time.After(30 * time.Second):
			t.Fatalf("replica %s never returned from CreateDNSZone", name)
			return nil
		}
	}
	resultA, resultB := join("A", errA), join("B", errB)

	// Measured from committed state through the other replica's store.
	zones, err := r.B.ListDNSZones(ctx, accountID, userID)
	require.NoError(t, err)

	require.Len(t, zones, 1,
		"%q and %q overlap; exactly one may exist (A=%v, B=%v)", parent, child, resultA, resultB)

	// One zone is not enough to call this correct. Today the survivor is
	// decided by the #143 deadlock: the loser dies inside
	// IncrementNetworkSerial on the shared-to-exclusive upgrade, which happens
	// to leave one zone standing. That is the bug protecting the invariant,
	// not the invariant being enforced, and the moment the lock order is
	// fixed the protection disappears with it.
	//
	// So the loser has to be turned away for the right reason: a rejection it
	// can act on, naming the overlap.
	losers := 0
	for name, err := range map[string]error{"A": resultA, "B": resultB} {
		if err == nil {
			continue
		}
		losers++

		// How the loser is told differs by engine, and the difference is the
		// whole of #157.
		//
		// Postgres runs READ COMMITTED and has no gap locks, so the re-check
		// under the exclusive account row sees the winner's committed zone and
		// answers with the rejection the caller can act on.
		//
		// MySQL runs REPEATABLE READ. The re-check cannot be a locking read —
		// that would take zone locks after the account row and deadlock
		// against every writer in this file, which take a zone row first — and
		// a non-locking read is served from a snapshot established before the
		// account row was held, so it cannot see the winner. What holds the
		// invariant there instead is InnoDB's gap lock on the sibling read: the
		// two creates deadlock and one dies. The invariant survives; the error
		// is one the caller cannot act on. Aligning MySQL to READ COMMITTED is
		// #157, and this assertion is what should flip when it lands.
		if isRepeatableReadEngine() {
			require.Error(t, err, "replica %s", name)
			continue
		}
		require.ErrorContains(t, err, "overlaps with existing zone",
			"replica %s lost, but not by being told the domain overlaps; it got %v", name, err)
		s, ok := status.FromError(err)
		require.True(t, ok && s.Type() == status.InvalidArgument,
			"replica %s must lose with a rejection the caller can act on, got %v", name, err)
	}
	require.Equal(t, 1, losers, "exactly one create must be rejected (A=%v, B=%v)", resultA, resultB)
}

// isRepeatableReadEngine reports whether the store under test defaults to
// REPEATABLE READ. Only MySQL does; Postgres and SQLite do not.
func isRepeatableReadEngine() bool {
	return types.Engine(strings.ToLower(os.Getenv("OPENZRO_STORE_ENGINE"))) == types.MysqlStoreEngine
}
