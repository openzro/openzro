package server

import (
	"context"
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

		require.ErrorContains(t, err, "overlaps with existing zone",
			"replica %s lost, but not by being told the domain overlaps; it got %v", name, err)
		s, ok := status.FromError(err)
		require.True(t, ok && s.Type() == status.InvalidArgument,
			"replica %s must lose with a rejection the caller can act on, got %v", name, err)
	}
	require.Equal(t, 1, losers, "exactly one create must be rejected (A=%v, B=%v)", resultA, resultB)
}
