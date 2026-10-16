package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
)

// peerDisconnectTopic is the single cluster.Coordinator topic every
// management instance subscribes to so a disconnect decided on one replica
// reaches the replica that holds the peer's Sync stream. One topic per
// deployment rather than one per peer keeps the subscription count flat,
// and instances that predate it simply never subscribe.
const peerDisconnectTopic = "mgmt.peers.disconnect"

// disconnectResubscribeMax caps the backoff between attempts to
// (re)establish the disconnect subscription after a broker failure.
const disconnectResubscribeMax = 30 * time.Second

type peerDisconnectMessage struct {
	PeerIDs []string `json:"peer_ids"`
}

// DisconnectPeers closes the update channels of the given peers on every
// management instance, which ends their Sync streams. Locally it behaves
// like CloseChannels; in HA mode it also broadcasts the peer IDs so the
// replica that owns each stream closes it too.
//
// Use it only where the peer must re-authenticate before it may sync
// again (login expiry, blocked owner): a broadcast that lands after the
// peer reconnected elsewhere ends the new stream as well. It is
// idempotent, and a peer without a channel anywhere is a no-op.
//
// The broadcast is the fast path only. Coordinator pub/sub is
// at-most-once, and a replica can be between subscriptions, so a nil error
// does not prove every replica closed the stream. The per-replica
// reconciler (runPeerStreamReconciler) is the convergent boundary: it
// closes streams of expired or deleted peers from persisted state, so a
// missed broadcast is bounded by its interval.
//
// The error reports a failed cluster publish; the local close has already
// happened. Callers that confirm a deprovisioning should fail the request
// so the client retries.
func (p *PeersUpdateManager) DisconnectPeers(ctx context.Context, peerIDs []string) error {
	if len(peerIDs) == 0 {
		return nil
	}
	p.CloseChannels(ctx, peerIDs)

	if p.coordinator == nil {
		return nil
	}
	payload, err := json.Marshal(peerDisconnectMessage{PeerIDs: peerIDs})
	if err != nil {
		return fmt.Errorf("marshal peer disconnect: %w", err)
	}
	if err := p.coordinator.Publish(ctx, peerDisconnectTopic, payload); err != nil {
		return fmt.Errorf("publish peer disconnect: %w", err)
	}
	return nil
}

// watchDisconnects keeps a subscription to peerDisconnectTopic for the
// manager's lifetime and closes the named peers' local channels. It
// resubscribes with backoff when the broker drops the subscription, and
// exits when the manager is stopped.
func (p *PeersUpdateManager) watchDisconnects() {
	backoff := time.Second
	for {
		events, err := p.coordinator.Subscribe(p.parentCtx, peerDisconnectTopic)
		if err == nil {
			backoff = time.Second
			p.disconnectReady.Store(true)
			for ev := range events {
				p.handleDisconnect(ev.Payload)
			}
			p.disconnectReady.Store(false)
		} else {
			log.Errorf("cluster subscribe for peer disconnects: %v", err)
		}

		select {
		case <-p.parentCtx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, disconnectResubscribeMax)
	}
}

func (p *PeersUpdateManager) handleDisconnect(payload []byte) {
	var msg peerDisconnectMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		log.Errorf("cluster peer disconnect: bad payload: %v", err)
		return
	}
	p.CloseChannels(p.parentCtx, msg.PeerIDs)
}

// disconnectSubscribed reports whether the disconnect subscription is live.
func (p *PeersUpdateManager) disconnectSubscribed() bool {
	return p.disconnectReady.Load()
}
