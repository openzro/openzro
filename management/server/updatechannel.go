package server

import (
	"context"
	"os"
	"strconv"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

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

type PeersUpdateManager struct {
	// peerChannels is an update channel indexed by Peer.ID
	peerChannels map[string]chan *UpdateMessage
	// channelsMux keeps the mutex to access peerChannels
	channelsMux *sync.RWMutex
	// metrics provides method to collect application metrics
	metrics telemetry.AppMetrics
}

// NewPeersUpdateManager returns a new instance of PeersUpdateManager
func NewPeersUpdateManager(metrics telemetry.AppMetrics) *PeersUpdateManager {
	return &PeersUpdateManager{
		peerChannels: make(map[string]chan *UpdateMessage),
		channelsMux:  &sync.RWMutex{},
		metrics:      metrics,
	}
}

// SendUpdate sends update message to the peer's channel
func (p *PeersUpdateManager) SendUpdate(ctx context.Context, peerID string, update *UpdateMessage) {
	start := time.Now()
	var found, dropped bool

	p.channelsMux.RLock()

	defer func() {
		p.channelsMux.RUnlock()
		if p.metrics != nil {
			p.metrics.UpdateChannelMetrics().CountSendUpdateDuration(time.Since(start), found, dropped)
		}
	}()

	if channel, ok := p.peerChannels[peerID]; ok {
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
	} else {
		log.WithContext(ctx).Debugf("peer %s has no channel", peerID)
	}
}

// CreateChannel creates a go channel for a given peer used to deliver updates relevant to the peer.
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

	if channel, ok := p.peerChannels[peerID]; ok {
		closed = true
		delete(p.peerChannels, peerID)
		close(channel)
	}
	channel := make(chan *UpdateMessage, channelBufferSize)
	p.peerChannels[peerID] = channel

	log.WithContext(ctx).Debugf("opened updates channel for a peer %s", peerID)

	return channel
}

func (p *PeersUpdateManager) closeChannel(ctx context.Context, peerID string) {
	if channel, ok := p.peerChannels[peerID]; ok {
		delete(p.peerChannels, peerID)
		close(channel)

		log.WithContext(ctx).Debugf("closed updates channel of a peer %s", peerID)
		return
	}

	log.WithContext(ctx).Debugf("closing updates channel: peer %s has no channel", peerID)
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

// GetAllConnectedPeers returns a copy of the connected peers map
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

// HasChannel returns true if peers has channel in update manager, otherwise false
func (p *PeersUpdateManager) HasChannel(peerID string) bool {
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
