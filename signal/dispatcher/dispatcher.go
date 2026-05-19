// Package dispatcher routes signal messages between peers, optionally across
// multiple signal-server instances (HA). The single-instance implementation
// lives in dispatcher/inmem; a Redis-backed multi-instance implementation
// lives in dispatcher/redis.
//
// This package was forked from netbirdio/signal-dispatcher@a464fd5f30cb (BSD-3,
// see LICENSE and AUTHORS in this directory). The single-instance implementation
// preserves the original behavior; the interface and the Redis backend are new
// in openzro and are not derived from any post-fork upstream code.
package dispatcher

import (
	"context"

	"github.com/openzro/openzro/signal/proto"
)

// Dispatcher routes encrypted signal messages from a sender to the peer they
// are addressed to. Implementations may keep peer registrations entirely in
// process memory (single instance) or back them with shared infrastructure
// (e.g. Redis) so that multiple signal-server instances can cooperate.
type Dispatcher interface {
	// SendMessage delivers msg to the peer identified by msg.RemoteKey. If the
	// destination peer is not currently registered, implementations MAY return
	// (&proto.EncryptedMessage{}, nil) and silently drop — the network will
	// re-converge when the peer reconnects.
	SendMessage(ctx context.Context, msg *proto.EncryptedMessage) (*proto.EncryptedMessage, error)

	// ListenForMessages registers handler as the receiver for messages
	// addressed to peer id. The handler runs in a dedicated goroutine until
	// ctx is canceled. The implementation is responsible for cleaning up
	// any registry entries when ctx is done.
	ListenForMessages(ctx context.Context, id string, handler func(context.Context, *proto.EncryptedMessage)) error
}
