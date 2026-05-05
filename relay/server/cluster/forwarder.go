package cluster

import (
	"context"
	"errors"
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/relay/messages"
)

// ErrPeerNotFound is returned by Forward when no pod in the
// cluster (including this one) has the peer connected. The caller
// — relay/server/peer.go's transport-message handler — should
// silently drop the packet, mirroring the single-pod behaviour
// from before this ADR.
var ErrPeerNotFound = errors.New("cluster forwarder: peer not connected anywhere in the cluster")

// LocalDispatcher is the interface the forwarder uses to deliver a
// packet to a peer connected to THIS pod. In production it's wired
// to the existing relay/server/store.Store plus the per-Peer Write
// path; in tests it's a fake. The HasPeer half lives on
// LocalOwnership (defined in locator.go) so the locator and the
// forwarder share the same view of "what's local".
type LocalDispatcher interface {
	LocalOwnership

	// DispatchToLocal hands an entire relay transport message
	// (already with src/dst peer IDs in its header) to the local
	// peer identified by dst. Returns an error only on hard
	// failures (peer disappeared between HasPeer and dispatch,
	// underlying TCP write failed) — the caller treats either as
	// "drop and move on", same as today's single-pod path.
	DispatchToLocal(dst messages.PeerID, msg []byte) error
}

// Forwarder routes a relay transport message to its destination
// peer regardless of which pod the peer is connected to. Local
// peers are dispatched in-process; remote peers are sent across
// the inter-pod fabric as MsgFwd frames.
//
// Forwarder is the seam where single-pod and multi-pod paths
// meet. relay/server/peer.go's transport-message handler calls
// Forward instead of looking up the local store directly. With
// no Forwarder configured (single-pod build / single-pod
// deployment), the existing local-only path runs unchanged.
type Forwarder struct {
	transport *Transport
	locator   *PeerLocator
	local     LocalDispatcher
}

// NewForwarder wires a forwarder against a transport, a peer
// locator, and the local dispatch surface. All three are required.
func NewForwarder(transport *Transport, locator *PeerLocator, local LocalDispatcher) (*Forwarder, error) {
	if transport == nil {
		return nil, fmt.Errorf("cluster forwarder: transport is required")
	}
	if locator == nil {
		return nil, fmt.Errorf("cluster forwarder: locator is required")
	}
	if local == nil {
		return nil, fmt.Errorf("cluster forwarder: local dispatcher is required")
	}
	return &Forwarder{
		transport: transport,
		locator:   locator,
		local:     local,
	}, nil
}

// Forward delivers msg to the peer whose ID is embedded in the
// transport-msg header. Tries local first; on miss, asks the
// locator which pod owns the peer and pushes the bytes through the
// inter-pod fabric. Returns ErrPeerNotFound when nobody owns the
// peer (the caller should drop the packet, same as today).
func (f *Forwarder) Forward(ctx context.Context, dst messages.PeerID, msg []byte) error {
	if f.local.HasPeer(dst) {
		return f.local.DispatchToLocal(dst, msg)
	}

	pod, ok, err := f.locator.Lookup(ctx, dst)
	if !ok {
		// `!ok` covers both "lookup completed, no pod owns it"
		// (locator returns ErrLookupNoOwner) and "lookup
		// errored". From the caller's perspective both mean the
		// same thing — drop the packet. Translate to a single
		// forwarder-level sentinel so peer.go's transport
		// handler can branch cleanly.
		return ErrPeerNotFound
	}
	if err != nil {
		// !ok is false but err is set — shouldn't happen with
		// the current locator, but guard anyway.
		return err
	}

	stream := f.transport.Stream(pod)
	if stream == nil {
		// Locator says peer is on `pod` but we don't have a
		// stream there. Either the pod just dropped or we never
		// finished its HELLO handshake. Invalidate and let the
		// next packet's Lookup re-broadcast.
		f.locator.Invalidate(dst)
		return ErrPeerNotFound
	}

	if err := stream.Send(MsgFwd, msg); err != nil {
		// Send errored — the underlying conn is broken. The
		// transport's read loop will have noticed too and is
		// dropping the stream from its map; from our side, we
		// invalidate the locator entry so the next Forward
		// re-broadcasts. The packet itself is lost (we don't
		// retry — relay is best-effort by design).
		f.locator.Invalidate(dst)
		return fmt.Errorf("cluster forwarder: send to %s: %w", pod, err)
	}
	return nil
}

// HandleFwd dispatches an inbound MsgFwd that arrived from a peer
// pod. The payload is the complete transport msg; we unmarshal the
// dst peer ID, look it up locally, and hand off. If the peer isn't
// here (anymore), drop quietly — the asking pod will time out and
// re-broadcast on the next Lookup.
func (f *Forwarder) HandleFwd(remote string, payload []byte) error {
	dst, err := messages.UnmarshalTransportID(payload)
	if err != nil {
		return fmt.Errorf("cluster forwarder: malformed FWD from %s: %w", remote, err)
	}
	if !f.local.HasPeer(*dst) {
		// Stale forward — the asking pod's locator cache thinks
		// we own this peer but we don't (peer disconnected
		// between Lookup and Send, or migrated to a third pod).
		// Silently drop; the asker invalidates on its end after
		// the timeout / fail-to-deliver path.
		log.Debugf("cluster forwarder: FWD from %s for peer not connected here, dropping", remote)
		return nil
	}
	if err := f.local.DispatchToLocal(*dst, payload); err != nil {
		log.Debugf("cluster forwarder: dispatch FWD from %s to local peer failed: %v", remote, err)
	}
	return nil
}
