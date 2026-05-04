package cluster

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/relay/messages"
)

// LookupTimeout caps how long a single Lookup waits for I_HAVE
// answers from peer pods. Inter-pod RTT in a healthy cluster is
// sub-ms; 200 ms is generous and still keeps the relay's hot path
// responsive when the target peer isn't connected anywhere (every
// pod stays silent and the lookup times out cleanly into "miss").
const LookupTimeout = 200 * time.Millisecond

// CacheTTL bounds how long a (peer → pod) mapping is trusted
// without re-checking. Five minutes balances "rarely re-broadcast
// during steady state" against "recover quickly when a peer
// migrates between pods after a reconnect". Stale entries are
// detected lazily: a forward that targets the cached pod fails the
// channel open, the locator entry is invalidated, and the next
// Lookup re-broadcasts.
const CacheTTL = 5 * time.Minute

// ErrLookupNoOwner is returned when no pod claims the peer within
// LookupTimeout. The caller should treat this exactly like a
// single-pod "peer not found" — fail the OpenConn back to the
// client with the existing error, no retry.
var ErrLookupNoOwner = errors.New("cluster locator: no pod owns this peer")

// LocalOwnership tells the locator which peers this pod owns
// directly. It's the existing relay/server/store/store.go,
// abstracted to a single method so unit tests don't have to wire
// in the whole store.
type LocalOwnership interface {
	HasPeer(messages.PeerID) bool
}

// LocalOwnershipFunc adapts a closure to LocalOwnership.
type LocalOwnershipFunc func(messages.PeerID) bool

// HasPeer implements LocalOwnership.
func (f LocalOwnershipFunc) HasPeer(p messages.PeerID) bool { return f(p) }

// PeerLocator answers "which pod owns peer X?" by checking a local
// cache first and broadcasting WHO_HAS to every connected peer pod
// on miss. Used by the channel forwarder when a relay client asks
// to open a session to a peer that isn't in this pod's local store.
//
// Locator is thread-safe.
type PeerLocator struct {
	transport *Transport
	local     LocalOwnership
	seqno     atomic.Uint32 // monotonic, used in our own I_HAVE answers

	cacheMu sync.RWMutex
	cache   map[messages.PeerID]locatorEntry

	waitersMu sync.Mutex
	waiters   map[messages.PeerID]*locatorWait

	clock func() time.Time
}

type locatorEntry struct {
	pod    string
	seqno  uint32
	expiry time.Time
}

type locatorWait struct {
	bestPod   string
	bestSeqno uint32
	found     bool
	done      chan struct{}
}

// NewPeerLocator builds a locator wired to the given transport.
// `local` answers whether this pod owns a peer (the relay's local
// peer store). `transport` provides the inter-pod fabric the
// locator broadcasts on.
func NewPeerLocator(transport *Transport, local LocalOwnership) *PeerLocator {
	return &PeerLocator{
		transport: transport,
		local:     local,
		cache:     make(map[messages.PeerID]locatorEntry),
		waiters:   make(map[messages.PeerID]*locatorWait),
		clock:     time.Now,
	}
}

// LocalSeqno bumps and returns the locator's own sequence number.
// This pod stamps every I_HAVE answer with this value, so when two
// pods briefly claim the same peer (peer migration race) the asker
// can pick the most recently bound owner.
func (pl *PeerLocator) LocalSeqno() uint32 {
	return pl.seqno.Add(1)
}

// Lookup resolves the peer to the pod address that currently owns
// it. Returns the pod's host:port and ok=true on success, or
// ("", false, ErrLookupNoOwner) if no pod responded within
// LookupTimeout.
//
// Cache hits return immediately; misses broadcast WHO_HAS and
// wait for I_HAVE answers.
func (pl *PeerLocator) Lookup(ctx context.Context, peer messages.PeerID) (string, bool, error) {
	// Cache fast-path. Stale entries are silently dropped here so
	// a Lookup after expiry behaves like a fresh miss.
	if pod, ok := pl.cacheGet(peer); ok {
		return pod, true, nil
	}

	streams := pl.transport.Streams()
	if len(streams) == 0 {
		// No peer pods discovered yet — fail fast with no-owner.
		// This also covers the "single-pod deployment that
		// somehow ended up running cluster code" sanity case.
		return "", false, ErrLookupNoOwner
	}

	wait := pl.beginWait(peer)
	defer pl.endWait(peer)

	payload := EncodeWhoHas(peer)
	for remote, stream := range streams {
		if err := stream.Send(MsgWhoHas, payload); err != nil {
			log.Debugf("cluster: WHO_HAS to %s failed: %v", remote, err)
			continue
		}
	}

	timeout := time.NewTimer(LookupTimeout)
	defer timeout.Stop()

	for {
		select {
		case <-wait.done:
			pl.waitersMu.Lock()
			pod := wait.bestPod
			found := wait.found
			pl.waitersMu.Unlock()
			if found {
				pl.cacheSet(peer, pod, wait.bestSeqno)
				return pod, true, nil
			}
			return "", false, ErrLookupNoOwner
		case <-timeout.C:
			pl.waitersMu.Lock()
			pod := wait.bestPod
			found := wait.found
			pl.waitersMu.Unlock()
			if found {
				pl.cacheSet(peer, pod, wait.bestSeqno)
				return pod, true, nil
			}
			return "", false, ErrLookupNoOwner
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
	}
}

// HandleWhoHas is the locator's reaction to an inbound WHO_HAS
// from another pod: if this pod owns the peer, reply with
// I_HAVE carrying our local seqno; otherwise stay silent.
//
// Silence-on-miss is deliberate: the asking pod gathers positive
// answers from every owner in the cluster; nothing useful comes
// from "I don't have it" replies and they would multiply N×.
func (pl *PeerLocator) HandleWhoHas(remote string, payload []byte) error {
	peer, err := DecodeWhoHas(payload)
	if err != nil {
		return err
	}
	if !pl.local.HasPeer(peer) {
		return nil
	}
	stream := pl.transport.Stream(remote)
	if stream == nil {
		// Sender went away between handing us the frame and our
		// reply — drop it; the asking pod will time out and treat
		// as miss, which is fine.
		return nil
	}
	return stream.Send(MsgIHave, EncodeIHave(peer, pl.LocalSeqno()))
}

// HandleIHave merges an I_HAVE answer into the in-flight waiter
// (if any) and updates the cache.
func (pl *PeerLocator) HandleIHave(remote string, payload []byte) error {
	peer, seqno, err := DecodeIHave(payload)
	if err != nil {
		return err
	}

	// Always update the cache — even if no Lookup is currently
	// pending, this gives us a head start on the next request.
	pl.cacheSet(peer, remote, seqno)

	pl.waitersMu.Lock()
	defer pl.waitersMu.Unlock()
	w, ok := pl.waiters[peer]
	if !ok {
		return nil
	}
	if !w.found || seqno > w.bestSeqno {
		w.bestPod = remote
		w.bestSeqno = seqno
		w.found = true
		// Don't close `done` here. Two pods may answer the same
		// WHO_HAS during a migration race; we want the late
		// answer to upgrade our pick if its seqno is higher.
		// The Lookup loop wakes on the timeout regardless.
	}
	return nil
}

// Invalidate drops the cache entry for peer. Called when a forward
// to the cached pod fails (open rejected / connection closed),
// since that's the strongest signal that the cache is stale.
func (pl *PeerLocator) Invalidate(peer messages.PeerID) {
	pl.cacheMu.Lock()
	delete(pl.cache, peer)
	pl.cacheMu.Unlock()
}

// CacheSize returns the number of cached entries. For diagnostics
// and tests.
func (pl *PeerLocator) CacheSize() int {
	pl.cacheMu.RLock()
	defer pl.cacheMu.RUnlock()
	return len(pl.cache)
}

func (pl *PeerLocator) beginWait(peer messages.PeerID) *locatorWait {
	pl.waitersMu.Lock()
	defer pl.waitersMu.Unlock()
	if existing, ok := pl.waiters[peer]; ok {
		// A second concurrent Lookup for the same peer joins the
		// in-flight waiter — both callers will see the same answer.
		return existing
	}
	w := &locatorWait{done: make(chan struct{})}
	pl.waiters[peer] = w
	return w
}

func (pl *PeerLocator) endWait(peer messages.PeerID) {
	pl.waitersMu.Lock()
	defer pl.waitersMu.Unlock()
	delete(pl.waiters, peer)
}

func (pl *PeerLocator) cacheGet(peer messages.PeerID) (string, bool) {
	pl.cacheMu.RLock()
	entry, ok := pl.cache[peer]
	pl.cacheMu.RUnlock()
	if !ok {
		return "", false
	}
	if pl.clock().After(entry.expiry) {
		pl.cacheMu.Lock()
		// Re-check under the write lock so we don't TOCTOU another
		// goroutine that already refreshed the entry.
		if cur, ok := pl.cache[peer]; ok && pl.clock().After(cur.expiry) {
			delete(pl.cache, peer)
		}
		pl.cacheMu.Unlock()
		return "", false
	}
	return entry.pod, true
}

func (pl *PeerLocator) cacheSet(peer messages.PeerID, pod string, seqno uint32) {
	pl.cacheMu.Lock()
	defer pl.cacheMu.Unlock()
	if cur, ok := pl.cache[peer]; ok && cur.seqno > seqno {
		// Older answer arrived after a newer one; ignore.
		return
	}
	pl.cache[peer] = locatorEntry{
		pod:    pod,
		seqno:  seqno,
		expiry: pl.clock().Add(CacheTTL),
	}
}
