// Package inmem is the single-instance Dispatcher implementation. Peer
// registrations are kept in process memory; messages are delivered only when
// sender and receiver are connected to the same signal-server instance.
//
// Forked verbatim from netbirdio/signal-dispatcher@a464fd5f30cb (BSD-3, see
// ../LICENSE and ../AUTHORS), with import paths updated to openzro and the
// type renamed from Dispatcher to InMem to disambiguate from the package
// dispatcher.Dispatcher interface.
package inmem

import (
	"context"
	"errors"
	"sync"

	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/metric"

	"github.com/openzro/openzro/signal/proto"
)

// InMem is the in-process Dispatcher. It keeps a map of peer-id → channel for
// connected peers on the local instance.
type InMem struct {
	peerChannels map[string]chan *proto.EncryptedMessage
	mu           sync.RWMutex
	ctx          context.Context
}

// New constructs an in-memory Dispatcher. The meter argument is accepted for
// signature symmetry with other backends; this implementation does not record
// any metrics of its own (the signal server records them at the call site).
func New(ctx context.Context, _ metric.Meter) (*InMem, error) {
	return &InMem{
		peerChannels: make(map[string]chan *proto.EncryptedMessage),
		ctx:          ctx,
	}, nil
}

// SendMessage delivers msg to the peer identified by msg.RemoteKey if that
// peer is registered on this instance. If the peer is not connected here,
// the message is dropped (no cross-instance forwarding in the in-memory
// backend).
func (d *InMem) SendMessage(ctx context.Context, msg *proto.EncryptedMessage) (*proto.EncryptedMessage, error) {
	select {
	case <-ctx.Done():
		return nil, errors.New("context canceled")
	default:
	}

	if msg.RemoteKey == "dummy" {
		// Test message send during openzro status
		return &proto.EncryptedMessage{}, nil
	}

	d.mu.RLock()
	ch, ok := d.peerChannels[msg.RemoteKey]
	d.mu.RUnlock()

	if !ok {
		log.Tracef("message from peer [%s] can't be forwarded to peer [%s] because destination peer is not connected", msg.Key, msg.RemoteKey)
		return &proto.EncryptedMessage{}, nil
	}

	select {
	case <-ctx.Done():
		return nil, errors.New("context canceled")
	case ch <- msg:
		return &proto.EncryptedMessage{}, nil
	}
}

// ListenForMessages registers handler as the receiver for messages addressed
// to peer id. A goroutine is spawned that runs until ctx is canceled, at
// which point the registry entry is removed.
func (d *InMem) ListenForMessages(ctx context.Context, id string, messageHandler func(context.Context, *proto.EncryptedMessage)) error {
	ch := make(chan *proto.EncryptedMessage)

	d.mu.Lock()
	d.peerChannels[id] = ch
	d.mu.Unlock()

	go func() {
		defer func() {
			d.mu.Lock()
			close(ch)
			delete(d.peerChannels, id)
			d.mu.Unlock()
			log.Debugf("stream closed for peer %s", id)
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					// Channel was closed, exit the goroutine
					return
				}
				if msg != nil {
					messageHandler(ctx, msg)
				}
			}
		}
	}()

	return nil
}
