// Package redis implements signal/dispatcher.Dispatcher on top of Redis,
// allowing multiple signal-server instances to cooperate so that a peer
// connected to instance A can receive messages from a peer connected to
// instance B.
//
// Design (clean-room, openzro original — not derived from any post-AGPL
// upstream code; references only the public Redis pub/sub documentation):
//
//   - Each signal-server instance generates a UUID at startup (the
//     "instance ID") and SUBSCRIBEs to a per-instance pub/sub channel
//     keyed by that UUID: "oz:signal:instance:<instanceID>".
//
//   - Peer registrations are stored as Redis keys
//     "oz:signal:peer:<peerID>" whose value is the instance ID currently
//     hosting that peer's stream. The keys carry a TTL; the instance
//     hosting a peer renews that TTL on a heartbeat. If an instance dies,
//     its peers' keys expire on their own — no lease coordination needed.
//
//   - SendMessage looks up the destination peer's instance via GET on the
//     peer key, then PUBLISHes to that instance's channel. The receiving
//     instance's consumer goroutine routes the payload to the peer's local
//     handler.
//
//   - Local fast-path: if the destination peer is registered on THIS
//     instance, the dispatcher invokes the handler directly without going
//     through Redis. This makes single-instance deployments and
//     single-account-per-instance affinities free of Redis round-trips.
//
//   - Failure mode: if Redis is unreachable, SendMessage to remote peers
//     returns an error to the caller (which the signal server logs as a
//     forward failure metric). Local-peer messaging continues to work.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/metric"
	gproto "google.golang.org/protobuf/proto"

	"github.com/openzro/openzro/signal/proto"
)

const (
	peerKeyPrefix         = "oz:signal:peer:"
	instanceChannelPrefix = "oz:signal:instance:"

	// DefaultPeerTTL is how long a peer registration lives in Redis without
	// renewal. Set to ~3x the heartbeat interval so a single missed
	// heartbeat does not cause the registry entry to expire.
	DefaultPeerTTL = 90 * time.Second

	// DefaultHeartbeatInterval is the cadence at which an instance refreshes
	// the TTL of every peer it currently hosts.
	DefaultHeartbeatInterval = 30 * time.Second
)

// envelope is the JSON wire format published on per-instance channels.
// Using JSON (rather than a second protobuf type) keeps the dispatcher
// independent of any further .proto changes.
type envelope struct {
	PeerID  string `json:"p"`
	Message []byte `json:"m"` // proto-marshaled EncryptedMessage
}

// Config tunes the dispatcher. All fields are optional; zero values fall
// back to the constants above.
type Config struct {
	Client            *redis.Client
	PeerTTL           time.Duration
	HeartbeatInterval time.Duration
	// InstanceID overrides the auto-generated UUID. Useful for tests.
	InstanceID string
}

// Redis is a Dispatcher backed by a Redis client.
type Redis struct {
	rdb               *redis.Client
	instanceID        string
	peerTTL           time.Duration
	heartbeatInterval time.Duration

	mu            sync.RWMutex
	localHandlers map[string]func(context.Context, *proto.EncryptedMessage)

	pubsub *redis.PubSub

	parentCtx    context.Context
	parentCancel context.CancelFunc
}

// New constructs a Redis-backed dispatcher and immediately subscribes to
// this instance's pub/sub channel. The returned dispatcher remains active
// until ctx is cancelled.
func New(ctx context.Context, cfg Config, _ metric.Meter) (*Redis, error) {
	if cfg.Client == nil {
		return nil, errors.New("redis dispatcher: Client is required")
	}
	peerTTL := cfg.PeerTTL
	if peerTTL <= 0 {
		peerTTL = DefaultPeerTTL
	}
	hb := cfg.HeartbeatInterval
	if hb <= 0 {
		hb = DefaultHeartbeatInterval
	}
	instanceID := cfg.InstanceID
	if instanceID == "" {
		instanceID = uuid.NewString()
	}

	parentCtx, parentCancel := context.WithCancel(ctx)

	if err := cfg.Client.Ping(parentCtx).Err(); err != nil {
		parentCancel()
		return nil, fmt.Errorf("redis dispatcher: ping: %w", err)
	}

	pubsub := cfg.Client.Subscribe(parentCtx, instanceChannelPrefix+instanceID)
	// Subscribe is asynchronous; wait for confirmation before returning so
	// callers know the dispatcher is ready to receive cross-instance traffic.
	if _, err := pubsub.Receive(parentCtx); err != nil {
		_ = pubsub.Close()
		parentCancel()
		return nil, fmt.Errorf("redis dispatcher: subscribe: %w", err)
	}

	r := &Redis{
		rdb:               cfg.Client,
		instanceID:        instanceID,
		peerTTL:           peerTTL,
		heartbeatInterval: hb,
		localHandlers:     make(map[string]func(context.Context, *proto.EncryptedMessage)),
		pubsub:            pubsub,
		parentCtx:         parentCtx,
		parentCancel:      parentCancel,
	}

	go r.consumePubSub()

	log.Infof("redis dispatcher started; instance=%s peerTTL=%s heartbeat=%s", instanceID, peerTTL, hb)
	return r, nil
}

// InstanceID returns the UUID that identifies this signal-server instance
// to peers in Redis. Mainly useful for tests and debug endpoints.
func (r *Redis) InstanceID() string { return r.instanceID }

// Close stops the dispatcher: cancels the consumer goroutine, drops the
// pub/sub subscription. It does NOT delete peer registry keys; those are
// expected to expire via TTL once heartbeats stop.
func (r *Redis) Close() error {
	r.parentCancel()
	return r.pubsub.Close()
}

// SendMessage delivers msg to the peer named by msg.RemoteKey. If the peer
// is local to this instance, the local handler is invoked directly. If the
// peer is registered on another instance, the message is published to that
// instance's pub/sub channel. If the peer is not registered anywhere, the
// message is dropped (mirroring the in-memory dispatcher's behavior).
func (r *Redis) SendMessage(ctx context.Context, msg *proto.EncryptedMessage) (*proto.EncryptedMessage, error) {
	if msg.RemoteKey == "dummy" {
		return &proto.EncryptedMessage{}, nil
	}

	r.mu.RLock()
	h, isLocal := r.localHandlers[msg.RemoteKey]
	r.mu.RUnlock()
	if isLocal {
		h(ctx, msg)
		return &proto.EncryptedMessage{}, nil
	}

	instanceID, err := r.rdb.Get(ctx, peerKeyPrefix+msg.RemoteKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			log.Tracef("peer [%s] not registered anywhere; dropping message from [%s]", msg.RemoteKey, msg.Key)
			return &proto.EncryptedMessage{}, nil
		}
		return nil, fmt.Errorf("redis dispatcher: registry lookup: %w", err)
	}

	if instanceID == r.instanceID {
		// Lost-the-race case: the local-handler check above said the peer
		// was not local, yet Redis says it is. The handler must have been
		// removed concurrently — drop, the peer is on its way out.
		return &proto.EncryptedMessage{}, nil
	}

	payload, err := gproto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("redis dispatcher: marshal message: %w", err)
	}
	env, err := json.Marshal(envelope{PeerID: msg.RemoteKey, Message: payload})
	if err != nil {
		return nil, fmt.Errorf("redis dispatcher: marshal envelope: %w", err)
	}
	if err := r.rdb.Publish(ctx, instanceChannelPrefix+instanceID, env).Err(); err != nil {
		return nil, fmt.Errorf("redis dispatcher: publish: %w", err)
	}
	return &proto.EncryptedMessage{}, nil
}

// ListenForMessages registers handler as the receiver for messages addressed
// to peer id, both from peers connected locally (fast path) and from peers
// connected to other instances (via pub/sub). Runs until ctx is cancelled.
func (r *Redis) ListenForMessages(ctx context.Context, id string, handler func(context.Context, *proto.EncryptedMessage)) error {
	r.mu.Lock()
	if _, exists := r.localHandlers[id]; exists {
		// A new ConnectStream for the same peer id replaces the previous
		// one. The previous goroutine's ctx will already be Done by the
		// time this fires, but we overwrite the handler defensively.
		log.Debugf("redis dispatcher: replacing existing handler for peer %s", id)
	}
	r.localHandlers[id] = handler
	r.mu.Unlock()

	if err := r.rdb.Set(ctx, peerKeyPrefix+id, r.instanceID, r.peerTTL).Err(); err != nil {
		// Roll back the local registration so we don't claim a peer we
		// can't actually be reached for from other instances.
		r.mu.Lock()
		delete(r.localHandlers, id)
		r.mu.Unlock()
		return fmt.Errorf("redis dispatcher: register peer: %w", err)
	}

	go r.heartbeat(ctx, id)

	go func() {
		<-ctx.Done()
		r.mu.Lock()
		delete(r.localHandlers, id)
		r.mu.Unlock()

		// Best-effort cleanup of the registry key. If Redis is unreachable
		// here, the TTL will handle it within peerTTL seconds.
		cleanupCtx, cancel := context.WithTimeout(r.parentCtx, 2*time.Second)
		defer cancel()
		if err := r.rdb.Del(cleanupCtx, peerKeyPrefix+id).Err(); err != nil {
			log.Debugf("redis dispatcher: deregister peer %s: %v (TTL will clean up)", id, err)
		}
		log.Debugf("redis dispatcher: stream closed for peer %s", id)
	}()

	return nil
}

func (r *Redis) heartbeat(ctx context.Context, peerID string) {
	interval := r.heartbeatInterval
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.parentCtx.Done():
			return
		case <-t.C:
			if err := r.rdb.Expire(r.parentCtx, peerKeyPrefix+peerID, r.peerTTL).Err(); err != nil {
				log.Warnf("redis dispatcher: heartbeat for peer %s failed: %v", peerID, err)
				continue
			}
		}
	}
}

func (r *Redis) consumePubSub() {
	ch := r.pubsub.Channel()
	for {
		select {
		case <-r.parentCtx.Done():
			return
		case raw, ok := <-ch:
			if !ok {
				return
			}
			var env envelope
			if err := json.Unmarshal([]byte(raw.Payload), &env); err != nil {
				log.Errorf("redis dispatcher: bad envelope: %v", err)
				continue
			}
			msg := &proto.EncryptedMessage{}
			if err := gproto.Unmarshal(env.Message, msg); err != nil {
				log.Errorf("redis dispatcher: bad message proto: %v", err)
				continue
			}
			r.mu.RLock()
			h, ok := r.localHandlers[env.PeerID]
			r.mu.RUnlock()
			if !ok {
				// Peer disconnected between when the sender looked up its
				// instance and when the message arrived here. Drop —
				// signal traffic is eventually-consistent and the peer's
				// new instance (or its absence) will be reflected in the
				// next round-trip.
				log.Tracef("redis dispatcher: no local handler for peer %s; dropping forwarded message", env.PeerID)
				continue
			}
			h(r.parentCtx, msg)
		}
	}
}
