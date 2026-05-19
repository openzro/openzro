package server

import (
	"context"
	"os"
	"strconv"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	gproto "google.golang.org/protobuf/proto"

	"github.com/openzro/openzro/cluster"
	"github.com/openzro/openzro/management/proto"
	"github.com/openzro/openzro/management/server/telemetry"
	"github.com/openzro/openzro/management/server/types"
)

const (
	// defaultChannelBufferSize is the per-peer update queue size used when
	// peerUpdateChannelBufferSizeEnv is not set. Upstream's hardcoded value
	// of 100 silently drops updates in any account with high churn or more
	// than ~100 peers (see SendUpdate's `default:` branch below). 1000 is
	// roughly the smallest value that does not cause noticeable drops in
	// realistic large accounts; deployments that need more can override via
	// the environment variable.
	defaultChannelBufferSize       = 1000
	peerUpdateChannelBufferSizeEnv = "OPENZRO_PEER_UPDATE_CHANNEL_BUFFER_SIZE"

	// peerUpdateTopicPrefix is the cluster.Coordinator pub/sub topic
	// namespace used to fan updates out across management instances. The
	// concrete topic per peer is peerUpdateTopicPrefix + peer.ID.
	peerUpdateTopicPrefix = "mgmt.peer."
)

// channelBufferSize is resolved once at package init and used for every
// new per-peer channel created by CreateChannel.
var channelBufferSize = resolveChannelBufferSize()

func resolveChannelBufferSize() int {
	v := os.Getenv(peerUpdateChannelBufferSizeEnv)
	if v == "" {
		return defaultChannelBufferSize
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		log.Warnf("ignoring invalid %s=%q; using default %d", peerUpdateChannelBufferSizeEnv, v, defaultChannelBufferSize)
		return defaultChannelBufferSize
	}
	log.Infof("%s set to %d (overriding default %d)", peerUpdateChannelBufferSizeEnv, n, defaultChannelBufferSize)
	return n
}

type UpdateMessage struct {
	Update     *proto.SyncResponse
	NetworkMap *types.NetworkMap
}

// PeersUpdateManager fans network-state updates out to connected peers.
//
// In single-instance mode (coordinator == nil) it works exactly like
// upstream: each peer with an open Sync stream gets a buffered channel,
// SendUpdate writes to it, the gRPC handler reads from it.
//
// In HA mode (coordinator != nil) it additionally:
//
//   - SUBSCRIBEs to mgmt.peer.<peerID> on the cluster Coordinator when a
//     peer connects locally; messages received on that topic are
//     dispatched to the local channel as if SendUpdate had been called.
//   - PUBLISHes to mgmt.peer.<peerID> when SendUpdate is asked to deliver
//     to a peer that is NOT registered locally — whichever instance has
//     the peer's Sync stream picks it up via its subscription and
//     dispatches there.
//
// Local-fast-path always runs first: a SendUpdate to a peer registered on
// this instance never touches the broker. NetworkMap on UpdateMessage is
// metadata used only by the producer (peer.go) and is intentionally
// dropped from the cluster wire format — only the proto.SyncResponse
// is marshaled and forwarded.
type PeersUpdateManager struct {
	// peerChannels is an update channel indexed by Peer.ID
	peerChannels map[string]chan *UpdateMessage
	// channelsMux keeps the mutex to access peerChannels and subs
	channelsMux *sync.RWMutex
	// metrics provides method to collect application metrics
	metrics telemetry.AppMetrics

	// coordinator is the cluster pub/sub used to fan updates across
	// management instances. nil means single-instance mode.
	coordinator cluster.Coordinator
	// subs holds the cancel func of the cluster Subscribe goroutine
	// per peer; populated only when coordinator != nil.
	subs map[string]context.CancelFunc
	// parentCtx anchors all cluster subscriptions; canceling it (via
	// Stop) tears every per-peer subscription down at once.
	parentCtx    context.Context
	parentCancel context.CancelFunc
}

// NewPeersUpdateManager returns a new instance of PeersUpdateManager in
// single-instance mode (no cross-instance fan-out).
func NewPeersUpdateManager(metrics telemetry.AppMetrics) *PeersUpdateManager {
	return NewPeersUpdateManagerWithCluster(metrics, nil)
}

// NewPeersUpdateManagerWithCluster returns a PeersUpdateManager that uses
// coordinator to forward updates to peers registered on other instances.
// Pass coordinator=nil for single-instance behavior.
func NewPeersUpdateManagerWithCluster(metrics telemetry.AppMetrics, coordinator cluster.Coordinator) *PeersUpdateManager {
	parentCtx, cancel := context.WithCancel(context.Background())
	return &PeersUpdateManager{
		peerChannels: make(map[string]chan *UpdateMessage),
		channelsMux:  &sync.RWMutex{},
		metrics:      metrics,
		coordinator:  coordinator,
		subs:         make(map[string]context.CancelFunc),
		parentCtx:    parentCtx,
		parentCancel: cancel,
	}
}

// Stop tears down every cluster subscription. Safe to call once at
// shutdown; not normally invoked in single-instance mode.
func (p *PeersUpdateManager) Stop() {
	p.parentCancel()
}

// SendUpdate sends update message to the peer's channel, falling back to
// the cluster pub/sub when the peer is not registered on this instance.
func (p *PeersUpdateManager) SendUpdate(ctx context.Context, peerID string, update *UpdateMessage) {
	start := time.Now()
	var found, dropped bool

	p.channelsMux.RLock()
	channel, isLocal := p.peerChannels[peerID]
	p.channelsMux.RUnlock()

	defer func() {
		if p.metrics != nil {
			p.metrics.UpdateChannelMetrics().CountSendUpdateDuration(time.Since(start), found, dropped)
		}
	}()

	if isLocal {
		found = true
		select {
		case channel <- update:
			log.WithContext(ctx).Debugf("update was sent to channel for peer %s", peerID)
		default:
			// A drop here means the peer will miss this update entirely —
			// the next snapshot will reconcile state, but until then the
			// peer's view is stale. Log loudly so operators can see this
			// and either investigate the slow consumer or raise
			// OPENZRO_PEER_UPDATE_CHANNEL_BUFFER_SIZE.
			dropped = true
			log.WithContext(ctx).Errorf("dropped update for peer %s: channel full (%d/%d)", peerID, len(channel), channelBufferSize)
		}
		return
	}

	if p.coordinator == nil {
		log.WithContext(ctx).Debugf("peer %s has no channel", peerID)
		return
	}

	// Peer is not registered on this instance. Fan the update out across
	// the cluster — whichever instance owns the peer's Sync stream will
	// pick it up via its mgmt.peer.<peerID> subscription.
	if update == nil || update.Update == nil {
		log.WithContext(ctx).Debugf("skipping cluster publish for peer %s: empty update", peerID)
		return
	}
	payload, err := gproto.Marshal(update.Update)
	if err != nil {
		log.WithContext(ctx).Errorf("cluster publish for peer %s: marshal: %v", peerID, err)
		return
	}
	if err := p.coordinator.Publish(ctx, peerUpdateTopicPrefix+peerID, payload); err != nil {
		log.WithContext(ctx).Errorf("cluster publish for peer %s: %v", peerID, err)
		return
	}
	log.WithContext(ctx).Debugf("update for peer %s published to cluster", peerID)
}

// CreateChannel creates a go channel for a given peer used to deliver
// updates relevant to the peer. In HA mode it also subscribes to the
// peer's cluster topic so updates published from other instances are
// delivered into the same channel.
func (p *PeersUpdateManager) CreateChannel(ctx context.Context, peerID string) chan *UpdateMessage {
	start := time.Now()

	closed := false

	p.channelsMux.Lock()
	defer func() {
		p.channelsMux.Unlock()
		if p.metrics != nil {
			p.metrics.UpdateChannelMetrics().CountCreateChannelDuration(time.Since(start), closed)
		}
	}()

	if existing, ok := p.peerChannels[peerID]; ok {
		closed = true
		delete(p.peerChannels, peerID)
		close(existing)
	}
	if cancel, ok := p.subs[peerID]; ok {
		cancel()
		delete(p.subs, peerID)
	}

	channel := make(chan *UpdateMessage, channelBufferSize)
	p.peerChannels[peerID] = channel

	if p.coordinator != nil {
		subCtx, cancel := context.WithCancel(p.parentCtx)
		p.subs[peerID] = cancel
		topic := peerUpdateTopicPrefix + peerID
		events, err := p.coordinator.Subscribe(subCtx, topic)
		if err != nil {
			log.WithContext(ctx).Errorf("cluster subscribe for peer %s: %v", peerID, err)
			cancel()
			delete(p.subs, peerID)
		} else {
			go p.forwardClusterEvents(peerID, channel, events)
		}
	}

	log.WithContext(ctx).Debugf("opened updates channel for a peer %s", peerID)
	return channel
}

// forwardClusterEvents reads cluster Coordinator events for a peer and
// pushes them into the local channel as UpdateMessages. Drops messages
// whose proto fails to unmarshal; relies on the local channel's existing
// non-blocking `default` drop to avoid head-of-line blocking under load.
func (p *PeersUpdateManager) forwardClusterEvents(peerID string, channel chan *UpdateMessage, events <-chan cluster.Event) {
	for ev := range events {
		sync := &proto.SyncResponse{}
		if err := gproto.Unmarshal(ev.Payload, sync); err != nil {
			log.Errorf("cluster forward for peer %s: bad proto: %v", peerID, err)
			continue
		}
		// We re-check that the channel still exists and is the one we
		// were spawned for. If a CloseChannel raced with a delivery, we
		// drop instead of panicking on send-on-closed-channel.
		p.channelsMux.RLock()
		current, ok := p.peerChannels[peerID]
		p.channelsMux.RUnlock()
		if !ok || current != channel {
			return
		}
		select {
		case channel <- &UpdateMessage{Update: sync}:
		default:
			log.Errorf("dropped cluster-forwarded update for peer %s: channel full (%d/%d)", peerID, len(channel), channelBufferSize)
		}
	}
}

// closeChannel removes the local channel and cancels the cluster
// subscription, if any. Caller must hold channelsMux.
func (p *PeersUpdateManager) closeChannel(ctx context.Context, peerID string) {
	if channel, ok := p.peerChannels[peerID]; ok {
		delete(p.peerChannels, peerID)
		close(channel)
		log.WithContext(ctx).Debugf("closed updates channel of a peer %s", peerID)
	} else {
		log.WithContext(ctx).Debugf("closing updates channel: peer %s has no channel", peerID)
	}
	if cancel, ok := p.subs[peerID]; ok {
		cancel()
		delete(p.subs, peerID)
	}
}

// CloseChannels closes updates channel for each given peer
func (p *PeersUpdateManager) CloseChannels(ctx context.Context, peerIDs []string) {
	start := time.Now()

	p.channelsMux.Lock()
	defer func() {
		p.channelsMux.Unlock()
		if p.metrics != nil {
			p.metrics.UpdateChannelMetrics().CountCloseChannelsDuration(time.Since(start), len(peerIDs))
		}
	}()

	for _, id := range peerIDs {
		p.closeChannel(ctx, id)
	}
}

// CloseChannel closes updates channel of a given peer
func (p *PeersUpdateManager) CloseChannel(ctx context.Context, peerID string) {
	start := time.Now()

	p.channelsMux.Lock()
	defer func() {
		p.channelsMux.Unlock()
		if p.metrics != nil {
			p.metrics.UpdateChannelMetrics().CountCloseChannelDuration(time.Since(start))
		}
	}()

	p.closeChannel(ctx, peerID)
}

// GetAllConnectedPeers returns a copy of the LOCALLY connected peers map.
// In HA mode this is a per-instance view, not a global one — call sites
// that need a global view must aggregate across instances themselves.
func (p *PeersUpdateManager) GetAllConnectedPeers() map[string]struct{} {
	start := time.Now()

	p.channelsMux.RLock()

	m := make(map[string]struct{})

	defer func() {
		p.channelsMux.RUnlock()
		if p.metrics != nil {
			p.metrics.UpdateChannelMetrics().CountGetAllConnectedPeersDuration(time.Since(start), len(m))
		}
	}()

	for ID := range p.peerChannels {
		m[ID] = struct{}{}
	}

	return m
}

// HasChannel reports whether SendUpdate has any chance of delivering to
// peerID. In single-instance mode this means "is the peer registered on
// this manager?". In HA mode this returns true unconditionally because
// the peer might be registered on a different management instance, and
// SendUpdate's cluster fall-back will reach it transparently. Callers
// that explicitly want the local-only signal (e.g. metrics, debug
// endpoints) should use HasLocalChannel.
func (p *PeersUpdateManager) HasChannel(peerID string) bool {
	if p.coordinator != nil {
		return true
	}
	return p.HasLocalChannel(peerID)
}

// HasLocalChannel reports whether the peer is registered on THIS
// instance. Always strictly local, regardless of HA mode.
func (p *PeersUpdateManager) HasLocalChannel(peerID string) bool {
	start := time.Now()

	p.channelsMux.RLock()

	defer func() {
		p.channelsMux.RUnlock()
		if p.metrics != nil {
			p.metrics.UpdateChannelMetrics().CountHasChannelDuration(time.Since(start))
		}
	}()

	_, ok := p.peerChannels[peerID]
	return ok
}
