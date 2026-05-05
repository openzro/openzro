package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openzro/openzro/relay/messages"
	"github.com/openzro/openzro/relay/server/cluster"
	"github.com/openzro/openzro/relay/server/store"
)

// clusterForwardTimeout caps how long handleTransportMsg waits for
// the cluster fabric to deliver before logging and dropping. The
// locator's broadcast deadline is ~2 s and inter-pod RTT inside K8s
// is sub-millisecond; 5 s gives the locator slack while still
// failing fast on a real partition.
const clusterForwardTimeout = 5 * time.Second

// errClusterPeerNotFound mirrors cluster.ErrPeerNotFound so the
// peer.go drop-on-miss path can branch via errors.Is without
// importing the cluster package directly.
var errClusterPeerNotFound = cluster.ErrPeerNotFound

// CrossPodForwarder is the seam where the cluster transport plugs in
// (per ADR-0014). When a transport-msg arrives for a peer this pod
// doesn't own, the relay hands the bytes to the forwarder; the
// forwarder either delivers across the inter-pod fabric or returns
// an error indicating the peer is nowhere in the cluster.
//
// In single-pod deployments this stays nil and the relay falls back
// to its legacy "drop on local-store miss" behaviour, byte-for-byte.
type CrossPodForwarder interface {
	Forward(ctx context.Context, dst messages.PeerID, msg []byte) error
}

// ErrLocalPeerGone is returned by LocalPeerDispatcher when the peer
// disappeared between the cluster forwarder's HasPeer check and the
// dispatch — the asking pod will time out and re-broadcast on the
// next Lookup, same as today's race-recovery path.
var ErrLocalPeerGone = errors.New("relay: local peer disappeared between HasPeer and dispatch")

// LocalPeerDispatcher adapts the relay store to the cluster-side
// "local pod surface" contract. The cluster forwarder calls into
// this when:
//
//   - a Lookup resolved to *this* pod (e.g. the asker hadn't seen the
//     I_HAVE yet but local cache says we own it), or
//   - a sibling pod sent us an MsgFwd whose dst peer ID is connected
//     here.
//
// In both cases we just write the transport-msg bytes to the local
// peer's TCP connection. The src field of the transport-msg has
// already been set by the originating pod, so the wire format the
// local peer sees is identical to a same-pod relay.
type LocalPeerDispatcher struct {
	store *store.Store
}

// NewLocalPeerDispatcher wires the cluster's LocalDispatcher
// contract against the existing relay store.
func NewLocalPeerDispatcher(s *store.Store) *LocalPeerDispatcher {
	return &LocalPeerDispatcher{store: s}
}

// HasPeer reports whether the peer is connected to this pod.
func (d *LocalPeerDispatcher) HasPeer(p messages.PeerID) bool {
	_, ok := d.store.Peer(p)
	return ok
}

// DispatchToLocal writes the transport message bytes to the peer
// identified by dst. Returns ErrLocalPeerGone if the peer
// disappeared between HasPeer and dispatch — the cluster forwarder
// turns that into a re-broadcast on the next Lookup.
func (d *LocalPeerDispatcher) DispatchToLocal(dst messages.PeerID, msg []byte) error {
	item, ok := d.store.Peer(dst)
	if !ok {
		return ErrLocalPeerGone
	}
	p, ok := item.(*Peer)
	if !ok {
		return fmt.Errorf("relay: store entry for %s is not a *Peer", dst)
	}
	if _, err := p.Write(msg); err != nil {
		return fmt.Errorf("relay: write to local peer %s: %w", dst, err)
	}
	return nil
}
