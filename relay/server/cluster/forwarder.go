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
	metrics   *Metrics
}

// SetMetrics installs the cluster metrics handle. Passing nil
// disables instrumentation; the *Metrics lifetime is owned by the
// caller.
func (f *Forwarder) SetMetrics(m *Metrics) {
	f.metrics = m
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

// Forward delivers msg to the peer whose ID is the dst arg. Tries
// local first; on miss, asks the locator which pod owns the peer
// and pushes the bytes through the inter-pod fabric. Returns
// ErrPeerNotFound when nobody owns the peer (the caller should
// drop the packet, same as today).
//
// Wire format of MsgFwd is `dst PeerID || msg`. The msg has
// already had its peer-ID slot rewritten to the SRC by peer.go's
// handleTransportMsg before Forward is called, so dispatching
// `msg` to the local destination on the receiving pod is a clean
// hand-off — the destination's TCP/WS connection sees the same
// bytes a same-pod relay would have produced. We carry dst as an
// explicit prefix because it would otherwise be unrecoverable on
// the receiving side (the sender already overwrote the slot to
// stamp src). Without this prefix, HandleFwd would read slot=src
// where it expects slot=dst and silently drop every cross-pod
// packet — which is the exact bug shipped in the alpha.41…42
// fabric line.
func (f *Forwarder) Forward(ctx context.Context, dst messages.PeerID, msg []byte) error {
	if f.local.HasPeer(dst) {
		err := f.local.DispatchToLocal(dst, msg)
		if err == nil {
			f.metrics.IncForward(ctx, ForwardResultOK)
		} else {
			f.metrics.IncForward(ctx, ForwardResultSendError)
		}
		return err
	}

	pod, ok, err := f.locator.Lookup(ctx, dst)
	if !ok {
		// `!ok` covers both "lookup completed, no pod owns it"
		// (locator returns ErrLookupNoOwner) and "lookup
		// errored". From the caller's perspective both mean the
		// same thing — drop the packet. Translate to a single
		// forwarder-level sentinel so peer.go's transport
		// handler can branch cleanly.
		f.metrics.IncForward(ctx, ForwardResultPeerNotFound)
		return ErrPeerNotFound
	}
	if err != nil {
		// !ok is false but err is set — shouldn't happen with
		// the current locator, but guard anyway.
		f.metrics.IncForward(ctx, ForwardResultSendError)
		return err
	}

	stream := f.transport.Stream(pod)
	if stream == nil {
		// Locator says peer is on `pod` but we don't have a
		// stream there. Either the pod just dropped or we never
		// finished its HELLO handshake. Invalidate and let the
		// next packet's Lookup re-broadcast.
		f.locator.Invalidate(dst)
		f.metrics.IncForward(ctx, ForwardResultStreamGone)
		return ErrPeerNotFound
	}

	framed := make([]byte, len(dst)+len(msg))
	copy(framed, dst[:])
	copy(framed[len(dst):], msg)

	if err := stream.Send(MsgFwd, framed); err != nil {
		// Send errored — the underlying conn is broken. The
		// transport's read loop will have noticed too and is
		// dropping the stream from its map; from our side, we
		// invalidate the locator entry so the next Forward
		// re-broadcasts. The packet itself is lost (we don't
		// retry — relay is best-effort by design).
		f.locator.Invalidate(dst)
		f.metrics.IncForward(ctx, ForwardResultSendError)
		return fmt.Errorf("cluster forwarder: send to %s: %w", pod, err)
	}
	f.metrics.IncForward(ctx, ForwardResultOK)
	return nil
}

// Locate answers "does any pod in the cluster currently own this
// peer?" without sending any data. Used by the relay's subscribe
// path so a client on pod-A asking about a peer on pod-B sees that
// the peer is online (rather than silently timing out as if the
// peer didn't exist anywhere). Local hits short-circuit without
// hitting the fabric.
func (f *Forwarder) Locate(ctx context.Context, peer messages.PeerID) (string, bool) {
	if f.local.HasPeer(peer) {
		return "", true
	}
	pod, ok, _ := f.locator.Lookup(ctx, peer)
	if !ok {
		return "", false
	}
	return pod, true
}

// HandleFwd dispatches an inbound MsgFwd that arrived from a peer
// pod. Wire format is `dst PeerID || msg`. We extract dst from the
// prefix and dispatch the embedded msg (which still has its slot
// stamped with src) to the local destination's connection. If the
// peer isn't here (anymore), drop quietly — the asking pod will
// time out and re-broadcast on the next Lookup.
func (f *Forwarder) HandleFwd(remote string, payload []byte) error {
	if len(payload) < peerIDSize {
		return fmt.Errorf("cluster forwarder: short FWD payload from %s (%d < %d)", remote, len(payload), peerIDSize)
	}
	var dst messages.PeerID
	copy(dst[:], payload[:peerIDSize])
	msg := payload[peerIDSize:]

	if !f.local.HasPeer(dst) {
		// Stale forward — the asking pod's locator cache thinks
		// we own this peer but we don't (peer disconnected
		// between Lookup and Send, or migrated to a third pod).
		// Silently drop; the asker invalidates on its end after
		// the timeout / fail-to-deliver path.
		log.Debugf("cluster forwarder: FWD from %s for peer not connected here, dropping", remote)
		return nil
	}
	if err := f.local.DispatchToLocal(dst, msg); err != nil {
		log.Debugf("cluster forwarder: dispatch FWD from %s to local peer failed: %v", remote, err)
	}
	return nil
}
