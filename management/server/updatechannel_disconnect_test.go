package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/store"
)

// requireClosed waits for ch to be closed, draining any updates queued
// before the close.
func requireClosed(t *testing.T, ch chan *UpdateMessage, msg string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-ch:
			if !open {
				return
			}
		case <-deadline:
			t.Fatal(msg)
		}
	}
}

// requireOpen asserts ch stays open for a short grace period.
func requireOpen(t *testing.T, ch chan *UpdateMessage, msg string) {
	t.Helper()
	select {
	case _, open := <-ch:
		require.True(t, open, msg)
	case <-time.After(200 * time.Millisecond):
	}
}

// In HA a peer's Sync stream lives on whichever replica it connected to.
// CloseChannels only knows local channels, so a disconnect decided on
// replica A left the stream on replica B open. DisconnectPeers must reach
// the owning replica through the cluster coordinator.
func TestPeersUpdateManager_DisconnectPeersAcrossInstances(t *testing.T) {
	coord := newFakeCoordinator()
	a := NewPeersUpdateManagerWithCluster(nil, coord)
	b := NewPeersUpdateManagerWithCluster(nil, coord)
	t.Cleanup(a.Stop)
	t.Cleanup(b.Stop)

	ctx := context.Background()
	onA := a.CreateChannel(ctx, "peer-on-a")
	onB := b.CreateChannel(ctx, "peer-on-b")
	bystander := b.CreateChannel(ctx, "bystander")

	// The disconnect subscription is established asynchronously.
	require.Eventually(t, func() bool { return a.disconnectSubscribed() && b.disconnectSubscribed() },
		2*time.Second, 10*time.Millisecond)

	require.NoError(t, a.DisconnectPeers(ctx, []string{"peer-on-a", "peer-on-b"}))

	requireClosed(t, onA, "local channel on the deciding replica was not closed")
	requireClosed(t, onB, "channel on the other replica was not closed")
	requireOpen(t, bystander, "a peer that was not named must keep its channel")
}

// Single-instance deployments have no coordinator; DisconnectPeers must
// still close local channels.
func TestPeersUpdateManager_DisconnectPeersSingleInstance(t *testing.T) {
	m := NewPeersUpdateManager(nil)
	ch := m.CreateChannel(context.Background(), "p1")

	require.NoError(t, m.DisconnectPeers(context.Background(), []string{"p1", "unknown"}))
	requireClosed(t, ch, "local channel was not closed")
}

// failingCoordinator rejects every publish, as a broker outage would.
type failingCoordinator struct{ *fakeCoordinator }

func (f failingCoordinator) Publish(context.Context, string, []byte) error {
	return errors.New("broker unavailable")
}

// A failed cluster publish must surface: the caller (e.g. a SCIM
// deprovisioning) has to report failure so the client retries, instead of
// confirming a disconnect that may not have reached the owning replica.
func TestPeersUpdateManager_DisconnectPeersPublishFailure(t *testing.T) {
	m := NewPeersUpdateManagerWithCluster(nil, failingCoordinator{newFakeCoordinator()})
	t.Cleanup(m.Stop)
	ch := m.CreateChannel(context.Background(), "p1")

	err := m.DisconnectPeers(context.Background(), []string{"p1"})
	require.Error(t, err)
	requireClosed(t, ch, "the local close must still happen when the publish fails")
}

// countingCoordinator records the peer-disconnect publishes.
type countingCoordinator struct {
	*fakeCoordinator
	mu      sync.Mutex
	batches [][]string
}

func (c *countingCoordinator) Publish(ctx context.Context, topic string, payload []byte) error {
	if topic == peerDisconnectTopic {
		var msg peerDisconnectMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			return err
		}
		c.mu.Lock()
		c.batches = append(c.batches, msg.PeerIDs)
		c.mu.Unlock()
	}
	return c.fakeCoordinator.Publish(ctx, topic, payload)
}

func (c *countingCoordinator) published() [][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]string(nil), c.batches...)
}

// #212 review: expiry jobs re-select every peer whose login is past due,
// including long-expired ones. Broadcasting all of them on each run made
// the payload and the channel-lock work grow with the account's history.
// Only newly expired peers are broadcast; the reconciler covers the rest.
func TestExpireAndUpdatePeers_DoesNotRebroadcastExpiredHistory(t *testing.T) {
	manager, accountID, _, _, _ := setupSCIMUserWithPeer(t)
	coord := &countingCoordinator{fakeCoordinator: newFakeCoordinator()}
	manager.peersUpdateManager = NewPeersUpdateManagerWithCluster(nil, coord)
	t.Cleanup(manager.peersUpdateManager.Stop)

	history := make([]*nbpeer.Peer, 0, 1000)
	for i := 0; i < 1000; i++ {
		history = append(history, &nbpeer.Peer{
			ID:     fmt.Sprintf("expired-%d", i),
			Status: &nbpeer.PeerStatus{LoginExpired: true},
		})
	}

	require.NoError(t, manager.expireAndUpdatePeers(context.Background(), accountID, history))
	require.Empty(t, coord.published(), "already-expired peers must not be re-broadcast")
}

// A large disconnect is split so no single broker message grows without
// bound.
func TestPeersUpdateManager_DisconnectPeersBatches(t *testing.T) {
	coord := &countingCoordinator{fakeCoordinator: newFakeCoordinator()}
	m := NewPeersUpdateManagerWithCluster(nil, coord)
	t.Cleanup(m.Stop)

	ids := make([]string, 2*peerDisconnectBatchSize+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("p%d", i)
	}
	require.NoError(t, m.DisconnectPeers(context.Background(), ids))

	batches := coord.published()
	require.Len(t, batches, 3)
	total := 0
	for _, b := range batches {
		require.LessOrEqual(t, len(b), peerDisconnectBatchSize)
		total += len(b)
	}
	require.Equal(t, len(ids), total)
}

// A lost broadcast must still converge. The retry no longer re-broadcasts
// already-expired peers; the reconciler on the replica holding the stream
// closes it from persisted state.
func TestExpireAndUpdatePeers_LostBroadcastConvergesViaReconciler(t *testing.T) {
	manager, accountID, user, peerID, _ := setupSCIMUserWithPeer(t)
	ctx := context.Background()

	// The status is persisted, the broadcast fails.
	manager.peersUpdateManager = NewPeersUpdateManagerWithCluster(nil, failingCoordinator{newFakeCoordinator()})
	t.Cleanup(manager.peersUpdateManager.Stop)
	peers, err := manager.Store.GetUserPeers(ctx, store.LockingStrengthShare, accountID, user.Id)
	require.NoError(t, err)
	require.Error(t, manager.expireAndUpdatePeers(ctx, accountID, peers))

	// Replica B holds the stream and never got the broadcast; its next
	// reconcile pass (same database) closes it.
	replicaB := NewPeersUpdateManagerWithCluster(nil, newFakeCoordinator())
	t.Cleanup(replicaB.Stop)
	onB := replicaB.CreateChannel(ctx, peerID)
	manager.peersUpdateManager = replicaB
	manager.reconcilePeerStreamsOnce(ctx)
	requireClosed(t, onB, "reconciler did not close the stream the broadcast missed")
}
