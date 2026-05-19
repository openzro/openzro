// Package nats implements signal/dispatcher.Dispatcher on top of NATS.
// Multiple signal-server instances connected to the same NATS deployment
// (external broker, embedded cluster, or anything in between) cooperate so
// that a peer connected to instance A can receive messages from a peer
// connected to instance B.
//
// Design (clean-room, openzro original):
//
//   - Each peer ID maps to a NATS subject "oz.signal.peer.<peerID>". When a
//     peer registers via ListenForMessages, the dispatcher SUBSCRIBEs to
//     that subject. When a sender calls SendMessage, the dispatcher
//     PUBLISHes to that subject. Whichever instance has the live
//     subscription receives the message and dispatches it to the local
//     handler.
//
//   - There is no separate peer registry, no TTL, no heartbeat. NATS
//     subscriptions are tied to the connection: on disconnect or peer
//     unregister, the subscription is dropped automatically.
//
//   - Local fast path: if the destination peer is registered on this
//     instance, the dispatcher invokes the handler synchronously without
//     a NATS round-trip. NATS clients are constructed with NoEcho so the
//     publishing instance is also not delivered its own message via the
//     subscription, even when both peers happen to be on the same node.
//
// Reference for the subject-hierarchy + cleanup-via-subscription pattern:
// https://docs.nats.io/nats-concepts/subjects . No upstream openzro/netbird
// post-AGPL code was consulted in writing this implementation.
package nats

import (
	"context"
	"errors"
	"fmt"
	"sync"

	natsclient "github.com/nats-io/nats.go"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/metric"
	gproto "google.golang.org/protobuf/proto"

	"github.com/openzro/openzro/signal/proto"
)

// SubjectPrefix is the NATS subject namespace used for peer messaging.
// Concrete subjects are SubjectPrefix + peerID.
const SubjectPrefix = "oz.signal.peer."

// NATS is a Dispatcher backed by a NATS connection.
type NATS struct {
	nc *natsclient.Conn

	mu            sync.RWMutex
	localHandlers map[string]func(context.Context, *proto.EncryptedMessage)
	subs          map[string]*natsclient.Subscription

	parentCtx    context.Context
	parentCancel context.CancelFunc
}

// New constructs a NATS-backed dispatcher. The caller owns the *nats.Conn:
// New does not Close() it. The connection should be created with
// nats.NoEcho() so the publishing instance does not receive its own
// messages back through its subscriptions.
func New(ctx context.Context, nc *natsclient.Conn, _ metric.Meter) (*NATS, error) {
	if nc == nil {
		return nil, errors.New("nats dispatcher: nil *nats.Conn")
	}
	if !nc.IsConnected() {
		return nil, errors.New("nats dispatcher: connection is not in connected state")
	}

	parentCtx, parentCancel := context.WithCancel(ctx)
	return &NATS{
		nc:            nc,
		localHandlers: make(map[string]func(context.Context, *proto.EncryptedMessage)),
		subs:          make(map[string]*natsclient.Subscription),
		parentCtx:     parentCtx,
		parentCancel:  parentCancel,
	}, nil
}

// Close drops every active subscription and stops the dispatcher. The
// underlying *nats.Conn is left intact (the caller manages its lifecycle).
func (n *NATS) Close() error {
	n.parentCancel()
	n.mu.Lock()
	defer n.mu.Unlock()
	var firstErr error
	for id, sub := range n.subs {
		if err := sub.Unsubscribe(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(n.subs, id)
	}
	for id := range n.localHandlers {
		delete(n.localHandlers, id)
	}
	return firstErr
}

// SendMessage delivers msg to the peer named by msg.RemoteKey. Local
// fast-path: if the peer is registered on this instance, the local handler
// is invoked synchronously. Otherwise the message is published to the
// peer's NATS subject; the instance that holds the subscription will
// deliver it to its handler.
func (n *NATS) SendMessage(ctx context.Context, msg *proto.EncryptedMessage) (*proto.EncryptedMessage, error) {
	if msg.RemoteKey == "dummy" {
		return &proto.EncryptedMessage{}, nil
	}

	n.mu.RLock()
	h, isLocal := n.localHandlers[msg.RemoteKey]
	n.mu.RUnlock()
	if isLocal {
		h(ctx, msg)
		return &proto.EncryptedMessage{}, nil
	}

	payload, err := gproto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("nats dispatcher: marshal message: %w", err)
	}
	if err := n.nc.Publish(SubjectPrefix+msg.RemoteKey, payload); err != nil {
		return nil, fmt.Errorf("nats dispatcher: publish: %w", err)
	}
	return &proto.EncryptedMessage{}, nil
}

// ListenForMessages registers handler as the receiver for messages
// addressed to peer id. Subscribes to oz.signal.peer.<id> and runs until
// ctx is canceled.
func (n *NATS) ListenForMessages(ctx context.Context, id string, handler func(context.Context, *proto.EncryptedMessage)) error {
	subject := SubjectPrefix + id

	n.mu.Lock()
	if existing, ok := n.subs[id]; ok {
		// Replace any prior registration for this peer id (a fresh
		// ConnectStream supersedes an older one).
		_ = existing.Unsubscribe()
		delete(n.subs, id)
		log.Debugf("nats dispatcher: replacing existing subscription for peer %s", id)
	}
	n.localHandlers[id] = handler
	n.mu.Unlock()

	sub, err := n.nc.Subscribe(subject, func(m *natsclient.Msg) {
		msg := &proto.EncryptedMessage{}
		if err := gproto.Unmarshal(m.Data, msg); err != nil {
			log.Errorf("nats dispatcher: bad message proto on %s: %v", subject, err)
			return
		}
		n.mu.RLock()
		h, ok := n.localHandlers[id]
		n.mu.RUnlock()
		if !ok {
			// Peer was unregistered between when the publisher sent and
			// when we received. Drop.
			return
		}
		h(n.parentCtx, msg)
	})
	if err != nil {
		n.mu.Lock()
		delete(n.localHandlers, id)
		n.mu.Unlock()
		return fmt.Errorf("nats dispatcher: subscribe %s: %w", subject, err)
	}

	n.mu.Lock()
	n.subs[id] = sub
	n.mu.Unlock()

	go func() {
		<-ctx.Done()
		n.mu.Lock()
		if s, ok := n.subs[id]; ok {
			_ = s.Unsubscribe()
			delete(n.subs, id)
		}
		delete(n.localHandlers, id)
		n.mu.Unlock()
		log.Debugf("nats dispatcher: stream closed for peer %s", id)
	}()

	return nil
}
