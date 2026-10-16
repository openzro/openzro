package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/store"
)

// #212 review: the disconnect broadcast is at-most-once — it can be
// dropped, or published while the owning replica has no subscriber
// (startup, resubscribe backoff). The per-replica reconciler is the
// convergent boundary: whatever the broadcast missed, a local stream of a
// peer whose persisted state forbids syncing is closed on the next pass.
func TestReconcilePeerStreams(t *testing.T) {
	manager, accountID, user, peerID, peerCh := setupSCIMUserWithPeer(t)
	ctx := context.Background()

	// A stream for a peer that no longer exists (deleted on another replica).
	ghost := manager.peersUpdateManager.CreateChannel(ctx, "deleted-peer")

	// Persist the expiry without any disconnect reaching this replica.
	peers, err := manager.Store.GetUserPeers(ctx, store.LockingStrengthShare, accountID, user.Id)
	require.NoError(t, err)
	peers[0].MarkLoginExpired(true)
	require.NoError(t, manager.Store.SavePeerStatus(ctx, store.LockingStrengthUpdate, accountID, peerID, *peers[0].Status))
	requireOpen(t, peerCh, "precondition: nothing has closed the stream yet")

	manager.reconcilePeerStreamsOnce(ctx)

	requireClosed(t, peerCh, "stream of a login-expired peer was not reconciled")
	requireClosed(t, ghost, "stream of a deleted peer was not reconciled")
}

// A peer that exists and whose login is valid keeps its stream.
func TestReconcilePeerStreams_KeepsSyncablePeers(t *testing.T) {
	manager, _, _, peerID, peerCh := setupSCIMUserWithPeer(t)

	manager.reconcilePeerStreamsOnce(context.Background())

	requireOpen(t, peerCh, "a syncable peer must keep its stream")
	require.True(t, manager.peersUpdateManager.HasLocalChannel(peerID))
}

func TestPeerStreamReconcileInterval(t *testing.T) {
	tests := []struct {
		env  string
		want time.Duration
	}{
		{env: "", want: defaultPeerStreamReconcileInterval},
		{env: "0", want: 0},
		{env: "2", want: minPeerStreamReconcileInterval},
		{env: "45", want: 45 * time.Second},
		{env: "bogus", want: defaultPeerStreamReconcileInterval},
	}
	for _, tc := range tests {
		t.Run(tc.env, func(t *testing.T) {
			t.Setenv(peerStreamReconcileIntervalEnv, tc.env)
			require.Equal(t, tc.want, peerStreamReconcileInterval())
		})
	}
}
