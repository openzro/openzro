package server

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
	"github.com/openzro/openzro/route"
)

// A peer may only receive one route for a prefix. The check reads existing
// routes and then writes on the strength of that absence. AcquireWriteLockByUID
// makes that safe inside one process, but not across replicas.
func TestRoute_ConcurrentCreateAcrossReplicas(t *testing.T) {
	const (
		accountID = "route-race-account"
		userID    = "route-race-user"
		peerID    = "route-race-peer"
		peerKey   = "route-race-peer-key"
		groupID   = "route-race-group"
	)

	r := newTwoReplicas(t)
	ctx := context.Background()

	account := newAccountWithId(ctx, accountID, userID, "route-race.example", false)
	account.Peers[peerID] = &nbpeer.Peer{
		ID:       peerID,
		AccountID: accountID,
		Key:      peerKey,
		IP:       net.ParseIP("100.64.0.10"),
		Name:     "route-race-peer",
		DNSLabel: "route-race-peer",
		UserID:   userID,
		Status:   &nbpeer.PeerStatus{LastSeen: time.Now().UTC(), Connected: true},
	}
	account.Groups[groupID] = &types.Group{
		ID:    groupID,
		Name:  "route-race-group",
		Peers: []string{peerID},
	}
	require.NoError(t, r.A.Store.SaveAccount(ctx, account))

	// Warm each replica. The barrier below aligns calls, not the first
	// statement inside each transaction.
	for _, am := range []*DefaultAccountManager{r.A, r.B} {
		_, err := am.ListRoutes(ctx, accountID, userID)
		require.NoError(t, err)
	}

	prefix := netip.MustParsePrefix("10.77.0.0/24")
	align := newBarrier()
	create := func(am *DefaultAccountManager, netID route.NetID) <-chan error {
		errCh := make(chan error, 1)
		go func() {
			align.wait(t)
			_, err := am.CreateRoute(ctx, accountID, prefix, route.IPv4Network, nil, "", []string{groupID},
				"route overlap race", netID, false, 1000, []string{groupID}, nil, true, userID, false)
			errCh <- err
		}()
		return errCh
	}

	errA, errB := create(r.A, "route-race-a"), create(r.B, "route-race-b")

	join := func(name string, ch <-chan error) error {
		t.Helper()
		select {
		case err := <-ch:
			return err
		case <-time.After(30 * time.Second):
			t.Fatalf("replica %s never returned from CreateRoute", name)
			return nil
		}
	}
	resultA, resultB := join("A", errA), join("B", errB)

	routes, err := r.B.Store.GetAccountRoutes(ctx, store.LockingStrengthShare, accountID)
	require.NoError(t, err)

	matching := 0
	for _, rt := range routes {
		if rt.Network == prefix && slices.Contains(rt.PeerGroups, groupID) {
			matching++
		}
	}

	require.Equal(t, 1, matching,
		"the account holds %d routes for group %q and prefix %q (A=%v, B=%v)",
		matching, groupID, prefix, resultA, resultB)

	losers := 0
	for name, err := range map[string]error{"A": resultA, "B": resultB} {
		if err == nil {
			continue
		}
		losers++
		if isRepeatableReadEngine() {
			require.ErrorContains(t, err, "failed to increment network serial count",
				"replica %s must lose at the account row, which is what holds this invariant on MySQL today; it got %v", name, err)
			s, ok := status.FromError(err)
			require.True(t, ok && s.Type() == status.Internal,
				"replica %s must lose with the store's internal error, got %v", name, err)
			continue
		}
		s, ok := status.FromError(err)
		require.True(t, ok && s.Type() == status.AlreadyExists,
			"replica %s must lose with an actionable route conflict, got %v", name, err)
	}
	require.Equal(t, 1, losers, "exactly one create must be rejected (A=%v, B=%v)", resultA, resultB)
}
