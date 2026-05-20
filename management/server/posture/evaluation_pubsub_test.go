package posture

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/cluster"
)

// fakeDedupCoord is a tiny in-process cluster.Coordinator for the
// dedup-pubsub tests. Implements Publish + Subscribe with fan-out
// to every subscriber. Lock is a no-op (we don't exercise it here).
type fakeDedupCoord struct {
	mu        sync.Mutex
	subs      map[string][]chan cluster.Event
	published atomic.Uint64
}

func newFakeDedupCoord() *fakeDedupCoord {
	return &fakeDedupCoord{subs: map[string][]chan cluster.Event{}}
}

func (c *fakeDedupCoord) Lock(_ context.Context, _ string) (func(), error) {
	return func() {}, nil
}

func (c *fakeDedupCoord) Publish(_ context.Context, topic string, payload []byte) error {
	c.published.Add(1)
	c.mu.Lock()
	subs := append([]chan cluster.Event(nil), c.subs[topic]...)
	c.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- cluster.Event{Topic: topic, Payload: payload}:
		default:
		}
	}
	return nil
}

func (c *fakeDedupCoord) Subscribe(ctx context.Context, topic string) (<-chan cluster.Event, error) {
	ch := make(chan cluster.Event, 16)
	c.mu.Lock()
	c.subs[topic] = append(c.subs[topic], ch)
	c.mu.Unlock()
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func (c *fakeDedupCoord) Close() error { return nil }

func TestDedupBroadcast_RoundTripsThroughBroker(t *testing.T) {
	coord := newFakeDedupCoord()
	subs, err := coord.Subscribe(context.Background(), dedupTopic)
	require.NoError(t, err)

	now := time.Now().UTC()
	inKey := dedupKey{AccountID: "a", PeerID: "p", PostureCheckID: "c", CheckType: "EndpointSecurityCheck"}
	inVal := dedupValue{Compliant: false, Reason: "denied", LastRecordedAt: now}
	publishDedup(coord, inKey, inVal)

	select {
	case ev := <-subs:
		gotKey, gotVal, err := decodeDedupBroadcast(ev.Payload)
		require.NoError(t, err)
		assert.Equal(t, inKey, gotKey)
		assert.Equal(t, inVal.Compliant, gotVal.Compliant)
		assert.Equal(t, inVal.Reason, gotVal.Reason)
		assert.True(t, gotVal.LastRecordedAt.Equal(inVal.LastRecordedAt),
			"LastRecordedAt must round-trip exactly")
	case <-time.After(1 * time.Second):
		t.Fatal("subscriber never received the published dedup broadcast")
	}
}

func TestBufferedRecorder_PublishesOnCacheCommit(t *testing.T) {
	coord := newFakeDedupCoord()
	store := &fakeEvalStore{}
	r := NewBufferedRecorder(store, BufferedRecorderOpts{
		QueueSize:     128,
		BatchSize:     1,
		FlushInterval: 1 * time.Hour,
	}, coord)
	defer r.Close()

	now := time.Now().UTC()
	e := Evaluation{
		AccountID:      "a",
		PeerID:         "p",
		PostureCheckID: "c",
		CheckType:      "EndpointSecurityCheck",
		Compliant:      false,
		Reason:         "denied",
		EvaluatedAt:    now,
	}

	r.Record(context.Background(), e)

	// One publish for the first commit. Second Record() with the same
	// state must dedup → no extra publish.
	deadline := time.After(1 * time.Second)
	for {
		if coord.published.Load() >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Record() did not publish dedup broadcast; published=%d", coord.published.Load())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	r.Record(context.Background(), e)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, uint64(1), coord.published.Load(),
		"deduped Record() must NOT publish a broadcast")
}

func TestBufferedRecorder_AppliesInboundBroadcastToCache(t *testing.T) {
	// Replica B receives a broadcast from Replica A, populates its
	// local cache, and the next Record() on Replica B for the same
	// state is deduped — i.e. it does NOT reach the channel.
	coord := newFakeDedupCoord()
	store := &fakeEvalStore{}
	r := NewBufferedRecorder(store, BufferedRecorderOpts{
		QueueSize:     128,
		BatchSize:     1,
		FlushInterval: 1 * time.Hour,
	}, coord)
	defer r.Close()

	now := time.Now().UTC()
	key := dedupKey{AccountID: "a", PeerID: "p", PostureCheckID: "c", CheckType: "EndpointSecurityCheck"}
	value := dedupValue{Compliant: false, Reason: "denied", LastRecordedAt: now}

	// Simulate a peer replica's broadcast arriving on the wire.
	r.applyDedupBroadcast(key, value)

	// Now a Record() on this replica with the same state should be
	// suppressed by the freshly-populated cache.
	r.Record(context.Background(), Evaluation{
		AccountID:      "a",
		PeerID:         "p",
		PostureCheckID: "c",
		CheckType:      "EndpointSecurityCheck",
		Compliant:      false,
		Reason:         "denied",
		EvaluatedAt:    now,
	})

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 0, len(store.snapshot()),
		"Record() after inbound broadcast must dedup — no row should reach the store")

	r.mu.Lock()
	dd := r.deduped
	fp := r.fromPeers
	r.mu.Unlock()
	assert.Equal(t, uint64(1), dd, "Record after inbound broadcast must increment deduped counter")
	assert.Equal(t, uint64(1), fp, "fromPeers counter must increment on applyDedupBroadcast")
}

func TestBufferedRecorder_InboundDoesNotEcho(t *testing.T) {
	// An inbound broadcast must NOT republish — otherwise N replicas
	// would amplify a single broadcast into N² (or runaway) messages.
	coord := newFakeDedupCoord()
	r := NewBufferedRecorder(&fakeEvalStore{}, BufferedRecorderOpts{
		QueueSize:     128,
		BatchSize:     1,
		FlushInterval: 1 * time.Hour,
	}, coord)
	defer r.Close()

	r.applyDedupBroadcast(
		dedupKey{AccountID: "a", PeerID: "p", PostureCheckID: "c", CheckType: "EndpointSecurityCheck"},
		dedupValue{Compliant: true, Reason: "", LastRecordedAt: time.Now().UTC()},
	)

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, uint64(0), coord.published.Load(),
		"applyDedupBroadcast must not re-publish — otherwise the cluster echoes forever")
}

func TestBufferedRecorder_OlderInboundDoesNotClobberFresherLocal(t *testing.T) {
	// Last-writer-wins by EvaluatedAt: if the local cache holds a
	// fresher entry, a stale broadcast must not roll it back.
	// Without this guard a slow broadcast could re-enable a duplicate
	// write on the next Record().
	coord := newFakeDedupCoord()
	r := NewBufferedRecorder(&fakeEvalStore{}, BufferedRecorderOpts{
		QueueSize:     128,
		BatchSize:     1,
		FlushInterval: 1 * time.Hour,
	}, coord)
	defer r.Close()

	now := time.Now().UTC()
	key := dedupKey{AccountID: "a", PeerID: "p", PostureCheckID: "c", CheckType: "EndpointSecurityCheck"}
	fresh := dedupValue{Compliant: false, Reason: "denied", LastRecordedAt: now}
	stale := dedupValue{Compliant: false, Reason: "denied", LastRecordedAt: now.Add(-5 * time.Minute)}

	// Seed cache with the fresh entry (simulate a Record() just
	// committed locally).
	r.mu.Lock()
	r.cache[key] = fresh
	r.mu.Unlock()

	// Then a delayed inbound broadcast with an older timestamp arrives.
	r.applyDedupBroadcast(key, stale)

	r.mu.Lock()
	got := r.cache[key]
	fp := r.fromPeers
	r.mu.Unlock()
	assert.True(t, got.LastRecordedAt.Equal(now),
		"stale inbound broadcast must not clobber fresher local entry")
	assert.Equal(t, uint64(0), fp,
		"stale inbound must not increment fromPeers — it was a no-op")
}

func TestBufferedRecorder_SelfEchoIsNoOp(t *testing.T) {
	// The broker fans out every message to ALL subscribers, including
	// the publisher itself. When Record() commits + publishes, the
	// same recorder receives its own broadcast back via the
	// subscriber goroutine. That self-echo must be a no-op:
	//
	//   * It must not republish (N² amplification trap).
	//   * It must not bump fromPeers (which is reserved for genuine
	//     peer broadcasts — a useful signal that the cluster pub/sub
	//     is doing work).
	//   * It must not clobber the entry we just wrote with the same
	//     value (last-writer-wins guard: equal LastRecordedAt is NOT
	//     "after", so the inbound is dropped).
	coord := newFakeDedupCoord()
	store := &fakeEvalStore{}
	r := NewBufferedRecorder(store, BufferedRecorderOpts{
		QueueSize:     128,
		BatchSize:     1,
		FlushInterval: 1 * time.Hour,
	}, coord)
	defer r.Close()

	now := time.Now().UTC()
	r.Record(context.Background(), Evaluation{
		AccountID:      "a",
		PeerID:         "p",
		PostureCheckID: "c",
		CheckType:      "EndpointSecurityCheck",
		Compliant:      false,
		Reason:         "denied",
		EvaluatedAt:    now,
	})

	// Let the subscriber goroutine process the self-echo.
	deadline := time.After(1 * time.Second)
	for {
		if coord.published.Load() >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("publish never fired")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	time.Sleep(100 * time.Millisecond)

	r.mu.Lock()
	fp := r.fromPeers
	r.mu.Unlock()
	assert.Equal(t, uint64(0), fp,
		"self-echo must NOT increment fromPeers — it's reserved for genuine peer broadcasts")
	assert.Equal(t, uint64(1), coord.published.Load(),
		"self-echo must NOT re-publish; only the original Record() should publish")
}

func TestBufferedRecorder_SubscribeFailureDegradesGracefully(t *testing.T) {
	// If the cluster coordinator's Subscribe() call fails at boot,
	// the recorder must still operate — just without the
	// cross-replica dedup feature. This protects against transient
	// broker hiccups at startup and against misconfigurations.
	coord := &subscribeErrCoord{}
	store := &fakeEvalStore{}
	r := NewBufferedRecorder(store, BufferedRecorderOpts{
		QueueSize:     128,
		BatchSize:     1,
		FlushInterval: 1 * time.Hour,
	}, coord)
	defer r.Close()

	// Record() must still work end-to-end even though subscription
	// failed. The write goes to the store via the channel/drainer.
	r.Record(context.Background(), Evaluation{
		AccountID:      "a",
		PeerID:         "p",
		PostureCheckID: "c",
		CheckType:      "EndpointSecurityCheck",
		Compliant:      false,
		Reason:         "denied",
		EvaluatedAt:    time.Now().UTC(),
	})

	deadline := time.After(2 * time.Second)
	for {
		if len(store.snapshot()) >= 1 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("Record() did not reach the store after subscribe failure — recorder degraded too far")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// subscribeErrCoord returns an error from Subscribe but otherwise
// behaves like a working coordinator. Models a partial broker
// failure during recorder boot.
type subscribeErrCoord struct{}

func (subscribeErrCoord) Lock(_ context.Context, _ string) (func(), error) {
	return func() {}, nil
}
func (subscribeErrCoord) Publish(_ context.Context, _ string, _ []byte) error { return nil }
func (subscribeErrCoord) Subscribe(_ context.Context, _ string) (<-chan cluster.Event, error) {
	return nil, fmt.Errorf("subscribe boom")
}
func (subscribeErrCoord) Close() error { return nil }

func TestBufferedRecorder_NilCoordIsSafe(t *testing.T) {
	// Single-instance mode passes nil. Every dedup-pubsub call path
	// must short-circuit safely without touching the broker.
	r := NewBufferedRecorder(&fakeEvalStore{}, BufferedRecorderOpts{
		QueueSize:     128,
		BatchSize:     1,
		FlushInterval: 1 * time.Hour,
	}, nil)
	defer r.Close()

	r.Record(context.Background(), Evaluation{
		AccountID:      "a",
		PeerID:         "p",
		PostureCheckID: "c",
		CheckType:      "EndpointSecurityCheck",
		Compliant:      true,
		EvaluatedAt:    time.Now().UTC(),
	})
	// If this returns without panic, the nil-coord paths are safe.
}

func TestBufferedRecorder_CrossReplicaDedupReducesDuplicateWrites(t *testing.T) {
	// Integration scenario: two recorders share a coord. Both see
	// the same eval. The second one's Record() must dedup against
	// the first one's broadcast, so only ONE row lands in each
	// store (best case post-broadcast).
	coord := newFakeDedupCoord()

	storeA := &fakeEvalStore{}
	rA := NewBufferedRecorder(storeA, BufferedRecorderOpts{
		QueueSize:     128,
		BatchSize:     1,
		FlushInterval: 1 * time.Hour,
	}, coord)
	defer rA.Close()

	storeB := &fakeEvalStore{}
	rB := NewBufferedRecorder(storeB, BufferedRecorderOpts{
		QueueSize:     128,
		BatchSize:     1,
		FlushInterval: 1 * time.Hour,
	}, coord)
	defer rB.Close()

	now := time.Now().UTC()
	e := Evaluation{
		AccountID:      "a",
		PeerID:         "p",
		PostureCheckID: "c",
		CheckType:      "EndpointSecurityCheck",
		Compliant:      false,
		Reason:         "denied",
		EvaluatedAt:    now,
	}

	// Replica A commits + broadcasts.
	rA.Record(context.Background(), e)

	// Wait for the broadcast to propagate into Replica B's cache.
	deadline := time.After(1 * time.Second)
	for {
		rB.mu.Lock()
		fp := rB.fromPeers
		rB.mu.Unlock()
		if fp >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Replica B never received the broadcast (fromPeers=0)")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Now Replica B sees the same eval. With the broadcast applied,
	// it MUST dedup and not persist a duplicate row.
	rB.Record(context.Background(), e)
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, 1, len(storeA.snapshot()), "Replica A persists one row")
	assert.Equal(t, 0, len(storeB.snapshot()),
		"Replica B must NOT persist a duplicate after applying A's broadcast")
}
