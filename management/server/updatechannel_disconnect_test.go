package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
