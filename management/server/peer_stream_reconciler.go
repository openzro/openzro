package server

import (
	"context"
	"os"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	// peerStreamReconcileIntervalEnv overrides how often each replica
	// reconciles its local Sync streams against persisted peer state.
	peerStreamReconcileIntervalEnv = "OPENZRO_PEER_STREAM_RECONCILE_INTERVAL_SECONDS"
	// defaultPeerStreamReconcileInterval is the worst-case delay before a
	// peer whose disconnect broadcast was lost loses its stream. The cost
	// per pass is one batched indexed query over the replica's connected
	// peers.
	defaultPeerStreamReconcileInterval = 30 * time.Second
	// minPeerStreamReconcileInterval floors pathological values so a typo
	// cannot turn the reconciler into a tight DB loop.
	minPeerStreamReconcileInterval = 5 * time.Second
)

// runPeerStreamReconciler periodically closes local Sync streams of peers
// that may no longer sync. The cluster disconnect broadcast
// (PeersUpdateManager.DisconnectPeers) is at-most-once: it can be dropped,
// or published while the owning replica has no subscriber. This loop is the
// convergent boundary that bounds how long such a peer stays connected.
func (am *DefaultAccountManager) runPeerStreamReconciler(ctx context.Context) {
	interval := peerStreamReconcileInterval()
	if interval == 0 {
		log.WithContext(ctx).Warnf("peer stream reconciler disabled (%s=0): a lost disconnect "+
			"broadcast leaves the peer connected until it reconnects", peerStreamReconcileIntervalEnv)
		return
	}
	log.WithContext(ctx).Infof("peer stream reconciler running every %s", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			am.reconcilePeerStreamsOnce(ctx)
		}
	}
}

// reconcilePeerStreamsOnce closes the local stream of every connected peer
// that no longer exists or whose login is expired. A store error skips the
// pass rather than being read as "no peer may sync".
func (am *DefaultAccountManager) reconcilePeerStreamsOnce(ctx context.Context) {
	if am.peersUpdateManager == nil {
		return
	}
	connected := am.peersUpdateManager.GetAllConnectedPeers()
	if len(connected) == 0 {
		return
	}
	ids := make([]string, 0, len(connected))
	for id := range connected {
		ids = append(ids, id)
	}

	syncable, err := am.Store.GetSyncablePeerIDs(ctx, ids)
	if err != nil {
		log.WithContext(ctx).Errorf("peer stream reconcile: %v", err)
		return
	}

	var stale []string
	for _, id := range ids {
		if _, ok := syncable[id]; !ok {
			stale = append(stale, id)
		}
	}
	if len(stale) == 0 {
		return
	}
	// Local only: every replica reconciles its own streams.
	am.peersUpdateManager.CloseChannels(ctx, stale)
	log.WithContext(ctx).Warnf("peer stream reconcile: closed %d stream(s) of expired or deleted peers", len(stale))
}

func peerStreamReconcileInterval() time.Duration {
	v := os.Getenv(peerStreamReconcileIntervalEnv)
	if v == "" {
		return defaultPeerStreamReconcileInterval
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		log.Warnf("ignoring invalid %s=%q; using default %s",
			peerStreamReconcileIntervalEnv, v, defaultPeerStreamReconcileInterval)
		return defaultPeerStreamReconcileInterval
	}
	if n == 0 {
		return 0
	}
	return max(time.Duration(n)*time.Second, minPeerStreamReconcileInterval)
}
