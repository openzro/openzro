package cluster

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/relay/messages"
)

// fakeOwner answers HasPeer from a slice the test pre-populates.
type fakeOwner struct {
	mu    sync.Mutex
	owned map[messages.PeerID]bool
}

func newFakeOwner(peers ...messages.PeerID) *fakeOwner {
	o := &fakeOwner{owned: make(map[messages.PeerID]bool, len(peers))}
	for _, p := range peers {
		o.owned[p] = true
	}
	return o
}

func (o *fakeOwner) HasPeer(p messages.PeerID) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.owned[p]
}

// dispatchHandler routes inbound frames to the locator's handlers
// while also publishing them to a channel a test can wait on.
type dispatchHandler struct {
	loc *PeerLocator
}

func (d *dispatchHandler) HandleFrame(remote string, t MsgType, payload []byte) error {
	switch t {
	case MsgWhoHas:
		return d.loc.HandleWhoHas(remote, payload)
	case MsgIHave:
		return d.loc.HandleIHave(remote, payload)
	default:
		return nil
	}
}

func newPeerID(seed byte) messages.PeerID {
	var p messages.PeerID
	for i := range p {
		p[i] = seed
	}
	return p
}

// startPair spins up two transports + locators and dials each
// other so they form a 2-pod cluster for the test.
func startPair(t *testing.T, ownsA, ownsB []messages.PeerID) (*PeerLocator, *PeerLocator, string, string) {
	t.Helper()
	skipOnDarwinCI(t)

	ownerA := newFakeOwner(ownsA...)
	ownerB := newFakeOwner(ownsB...)

	transA := NewTransport("127.0.0.1:0", "", nil)
	transB := NewTransport("127.0.0.1:0", "", nil)

	locA := NewPeerLocator(transA, ownerA)
	locB := NewPeerLocator(transB, ownerB)

	transA.handler = &dispatchHandler{loc: locA}
	transB.handler = &dispatchHandler{loc: locB}

	require.NoError(t, transA.ListenAndServe(context.Background()))
	require.NoError(t, transB.ListenAndServe(context.Background()))
	t.Cleanup(transA.Stop)
	t.Cleanup(transB.Stop)

	addrA := transA.listener.Addr().String()
	addrB := transB.listener.Addr().String()

	// Symmetric dial: each pod dials the other. Without the HELLO
	// exchange this would produce two TCP conns between the pair,
	// each accepted side keyed by an ephemeral source port that
	// the locator can't dial back to. With HELLO (phase 3) the
	// accepted side keys its stream by the dialer's announced
	// listen address, so both sides end up with one logical entry
	// keyed by the OTHER pod's listen address — and the dedup
	// inside Dial / handleAccepted collapses any racing duplicate
	// connection.
	_, err := transA.Dial(context.Background(), addrB)
	require.NoError(t, err)
	_, err = transB.Dial(context.Background(), addrA)
	require.NoError(t, err)

	// Wait for HELLO-driven dedup to converge: each pod must hold
	// exactly one live stream keyed by the OTHER pod's announced
	// address. Without this synchronization a test that calls
	// Lookup() right after startPair returns can land mid-dedup —
	// while one of the two racing TCP conns is being closed — and
	// observe a transient "connection reset by peer" from the
	// closing peer side. That's the recurring "Client / Unit
	// (macOS) TestLocator_*" flake; the fix gates the test on the
	// converged state rather than racing it.
	waitClusterReady(t, transA, addrB)
	waitClusterReady(t, transB, addrA)

	return locA, locB, addrA, addrB
}

// waitClusterReady polls until the transport's streams map holds a
// single live entry keyed by `expected`, signaling that the dedup
// in Dial / handleAccepted has settled. 2s is generous — local-
// loopback dedup completes in single-digit milliseconds in practice;
// a stall past 2s is a real bug worth surfacing.
func waitClusterReady(t *testing.T, tr *Transport, expected string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		streams := tr.Streams()
		if len(streams) == 1 {
			if _, ok := streams[expected]; ok {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waitClusterReady: cluster did not converge to {%s} within 2s; got %v", expected, tr.Streams())
}

func TestLocator_LookupHitsRemote(t *testing.T) {
	wanted := newPeerID(0x42)
	locA, _, _, addrB := startPair(t, nil, []messages.PeerID{wanted})

	pod, ok, err := locA.Lookup(context.Background(), wanted)
	require.NoError(t, err)
	require.True(t, ok, "remote pod owns the peer; lookup must resolve")
	require.Equal(t, addrB, pod, "lookup must point at the pod that answered")

	// Cache populated for the next call.
	require.Equal(t, 1, locA.CacheSize())
}

func TestLocator_LookupCacheHitSkipsBroadcast(t *testing.T) {
	wanted := newPeerID(0x10)
	locA, _, _, addrB := startPair(t, nil, []messages.PeerID{wanted})

	_, _, err := locA.Lookup(context.Background(), wanted)
	require.NoError(t, err)

	// Manually replace the remote owner's record so a second
	// broadcast would NOT find anything; the cache must answer
	// without re-asking. Lookup time should be near-zero.
	start := time.Now()
	pod, ok, err := locA.Lookup(context.Background(), wanted)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, addrB, pod)
	require.Less(t, time.Since(start), LookupTimeout/4,
		"cache hit must short-circuit the broadcast wait")
}

func TestLocator_LookupNoOwner(t *testing.T) {
	missing := newPeerID(0x99)
	locA, _, _, _ := startPair(t, nil, nil)

	pod, ok, err := locA.Lookup(context.Background(), missing)
	require.ErrorIs(t, err, ErrLookupNoOwner)
	require.False(t, ok)
	require.Empty(t, pod)
	require.Zero(t, locA.CacheSize(),
		"a no-owner lookup must not pollute the cache")
}

func TestLocator_LookupNoPeerPodsFailsFast(t *testing.T) {
	loc := NewPeerLocator(
		NewTransport("127.0.0.1:0", "", nil),
		newFakeOwner(),
	)
	require.NoError(t, loc.transport.ListenAndServe(context.Background()))
	t.Cleanup(loc.transport.Stop)

	start := time.Now()
	_, _, err := loc.Lookup(context.Background(), newPeerID(0x77))
	require.ErrorIs(t, err, ErrLookupNoOwner)
	require.Less(t, time.Since(start), LookupTimeout/2,
		"with no peer pods to ask, lookup must fail fast — not wait the full timeout")
}

func TestLocator_InvalidateDropsCacheEntry(t *testing.T) {
	wanted := newPeerID(0x55)
	locA, _, _, _ := startPair(t, nil, []messages.PeerID{wanted})

	_, _, err := locA.Lookup(context.Background(), wanted)
	require.NoError(t, err)
	require.Equal(t, 1, locA.CacheSize())

	locA.Invalidate(wanted)
	require.Zero(t, locA.CacheSize())
}

func TestLocator_HigherSeqnoWinsRace(t *testing.T) {
	// Seed locA's cache with a low-seqno entry pointing at one
	// pod, then handle an I_HAVE with a higher seqno from another
	// pod. The cache must update.
	loc := NewPeerLocator(
		NewTransport("127.0.0.1:0", "", nil),
		newFakeOwner(),
	)
	peer := newPeerID(0x33)

	loc.cacheSet(peer, "old-pod:7090", 5)
	require.NoError(t, loc.HandleIHave("new-pod:7090", EncodeIHave(peer, 17)))

	pod, ok := loc.cacheGet(peer)
	require.True(t, ok)
	require.Equal(t, "new-pod:7090", pod,
		"higher seqno must win the migration race; older bind is now stale")
}

func TestLocator_LowerSeqnoIgnored(t *testing.T) {
	loc := NewPeerLocator(
		NewTransport("127.0.0.1:0", "", nil),
		newFakeOwner(),
	)
	peer := newPeerID(0x33)
	loc.cacheSet(peer, "current-pod:7090", 100)

	require.NoError(t, loc.HandleIHave("late-pod:7090", EncodeIHave(peer, 50)))

	pod, ok := loc.cacheGet(peer)
	require.True(t, ok)
	require.Equal(t, "current-pod:7090", pod,
		"older seqno must not displace a fresher cache entry")
}

func TestLocator_StaleCacheEntryIsLazilyEvicted(t *testing.T) {
	loc := NewPeerLocator(
		NewTransport("127.0.0.1:0", "", nil),
		newFakeOwner(),
	)
	now := time.Now()
	loc.clock = func() time.Time { return now }

	peer := newPeerID(0x88)
	loc.cacheSet(peer, "expired-pod:7090", 1)
	require.Equal(t, 1, loc.CacheSize())

	// Advance past TTL.
	loc.clock = func() time.Time { return now.Add(CacheTTL + time.Second) }

	_, ok := loc.cacheGet(peer)
	require.False(t, ok, "TTL-expired entry must miss")
	require.Zero(t, loc.CacheSize(), "expired entries are evicted on access")
}

func TestLocator_ConcurrentLookupsShareWaiter(t *testing.T) {
	wanted := newPeerID(0xAA)
	locA, _, _, addrB := startPair(t, nil, []messages.PeerID{wanted})

	const callers = 8
	var wg sync.WaitGroup
	results := make([]string, callers)
	errs := make([]error, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pod, _, err := locA.Lookup(context.Background(), wanted)
			results[i] = pod
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		require.NoErrorf(t, e, "caller %d errored", i)
		require.Equal(t, addrB, results[i], "caller %d got wrong pod", i)
	}
	require.Equal(t, 1, locA.CacheSize(),
		"N concurrent lookups for the same peer must produce exactly one cache entry")
}
