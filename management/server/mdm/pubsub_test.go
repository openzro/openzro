package mdm

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/cluster"
)

// fakeBrokerCoord is a tiny in-process cluster.Coordinator stand-in
// for the pubsub tests: only Publish + Subscribe matter here.
// Lock returns immediately (we don't exercise it in these tests).
type fakeBrokerCoord struct {
	mu   sync.Mutex
	subs map[string][]chan cluster.Event
}

func newFakeBrokerCoord() *fakeBrokerCoord {
	return &fakeBrokerCoord{subs: map[string][]chan cluster.Event{}}
}

func (c *fakeBrokerCoord) Lock(_ context.Context, _ string) (func(), error) {
	return func() {}, nil
}

func (c *fakeBrokerCoord) Publish(_ context.Context, topic string, payload []byte) error {
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

func (c *fakeBrokerCoord) Subscribe(ctx context.Context, topic string) (<-chan cluster.Event, error) {
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

func (c *fakeBrokerCoord) Close() error { return nil }

func TestPublishStatus_RoundTripsThroughBroker(t *testing.T) {
	coord := newFakeBrokerCoord()
	subs, err := coord.Subscribe(context.Background(), statusTopic(42))
	require.NoError(t, err)

	in := DeviceLookup{Hostname: "alice", UserEmail: "alice@example.com", OS: "linux"}
	out := DeviceStatus{Found: true, Compliant: true, Reason: "ok"}
	publishStatus(context.Background(), coord, 42, in, out)

	select {
	case ev := <-subs:
		gotLookup, gotStatus, err := decodeStatusBroadcast(ev.Payload)
		require.NoError(t, err)
		assert.Equal(t, in, gotLookup)
		// LastChecked is set by put(), not by encode — strip it on
		// compare. Compliant + Reason + Found must round-trip.
		assert.Equal(t, out.Compliant, gotStatus.Compliant)
		assert.Equal(t, out.Reason, gotStatus.Reason)
		assert.Equal(t, out.Found, gotStatus.Found)
	case <-time.After(1 * time.Second):
		t.Fatal("broker subscriber never received the published status")
	}
}

func TestPublishStatus_NilCoordIsSafeNoOp(t *testing.T) {
	// Single-instance deployments pass nil — publishStatus must
	// short-circuit without touching anything.
	publishStatus(context.Background(), nil, 1,
		DeviceLookup{Hostname: "alice"}, DeviceStatus{Compliant: true})
}

func TestCachedProvider_FreshFetchFiresBroadcaster(t *testing.T) {
	inner := &countingProvider{}
	cached := NewCachedProvider(inner, 1*time.Hour)

	var fired atomic.Uint64
	var captured DeviceStatus
	cached.setBroadcaster(func(_ DeviceLookup, s DeviceStatus) {
		fired.Add(1)
		captured = s
	})

	// First call → cache miss → inner hit → broadcaster fires.
	_, err := cached.GetDeviceStatus(context.Background(),
		DeviceLookup{Hostname: "alice"})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), fired.Load(),
		"broadcaster must fire after a cache miss")
	assert.True(t, captured.Compliant,
		"broadcaster must receive the fetched status")

	// Second call → cache hit → broadcaster MUST NOT fire (would
	// cause runaway publish loops across replicas).
	_, err = cached.GetDeviceStatus(context.Background(),
		DeviceLookup{Hostname: "alice"})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), fired.Load(),
		"broadcaster must not fire on cache hit")
}

func TestCachedProvider_PutFromBrokerDoesNotEcho(t *testing.T) {
	// An inbound broker event must populate the cache WITHOUT firing
	// the broadcaster, otherwise N replicas would amplify a single
	// publish into N² messages on the broker.
	inner := &countingProvider{}
	cached := NewCachedProvider(inner, 1*time.Hour)

	var fired atomic.Uint64
	cached.setBroadcaster(func(_ DeviceLookup, _ DeviceStatus) {
		fired.Add(1)
	})

	cached.putFromBroker(
		DeviceLookup{Hostname: "alice"},
		DeviceStatus{Found: true, Compliant: true, Reason: "from-broker"},
	)
	assert.Equal(t, uint64(0), fired.Load(),
		"putFromBroker must not fire the broadcaster")

	// Subsequent lookup serves from the broker-populated cache —
	// inner is never called.
	st, err := cached.GetDeviceStatus(context.Background(),
		DeviceLookup{Hostname: "alice"})
	require.NoError(t, err)
	assert.Equal(t, "from-broker", st.Reason)
	assert.Equal(t, uint64(0), inner.calls.Load(),
		"inner must not be called when the broker pre-populated the cache")
}

func TestSubscribeStatus_FillsLocalCacheFromBroker(t *testing.T) {
	coord := newFakeBrokerCoord()
	inner := &countingProvider{}
	cached := NewCachedProvider(inner, 1*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, err := subscribeStatus(ctx, coord, 7, cached)
	require.NoError(t, err)
	defer stop()

	// Replica B (this side) is subscribed; replica A publishes.
	publishStatus(context.Background(), coord, 7,
		DeviceLookup{Hostname: "alice"},
		DeviceStatus{Found: true, Compliant: false, Reason: "denied-elsewhere"},
	)

	// The subscriber goroutine must see the event and fill the cache.
	// Peek directly at the cache (NOT via GetDeviceStatus, which would
	// itself hit inner on a miss and pollute the assertion below).
	deadline := time.After(1 * time.Second)
	for {
		if _, ok := cached.cache.get(encodeCacheKey(DeviceLookup{Hostname: "alice"})); ok {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("subscriber never filled the cache from broker event")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Now the cache is primed — GetDeviceStatus must serve from it.
	st, err := cached.GetDeviceStatus(context.Background(),
		DeviceLookup{Hostname: "alice"})
	require.NoError(t, err)
	assert.Equal(t, "denied-elsewhere", st.Reason)

	// Crucially: inner was never called on this replica — the cache
	// fill came entirely from the broker broadcast.
	assert.Equal(t, uint64(0), inner.calls.Load(),
		"inner must not be hit on the subscriber side")
}

func TestSubscribeStatus_NilCoordReturnsNoopCancel(t *testing.T) {
	stop, err := subscribeStatus(context.Background(), nil, 1, nil)
	require.NoError(t, err)
	// Must be safe to call even though there's nothing running.
	stop()
}

func TestSubscribeStatus_DropsMalformedPayload(t *testing.T) {
	coord := newFakeBrokerCoord()
	cached := NewCachedProvider(&countingProvider{}, 1*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, err := subscribeStatus(ctx, coord, 99, cached)
	require.NoError(t, err)
	defer stop()

	// Junk payload: must not panic, must not poison the subscription.
	_ = coord.Publish(context.Background(), statusTopic(99),
		[]byte("not-valid-json{{"))

	// Follow-up valid payload still lands in the cache → subscription
	// is alive.
	publishStatus(context.Background(), coord, 99,
		DeviceLookup{Hostname: "bob"},
		DeviceStatus{Found: true, Compliant: true, Reason: "fine"},
	)

	deadline := time.After(1 * time.Second)
	for {
		st, _ := cached.GetDeviceStatus(context.Background(),
			DeviceLookup{Hostname: "bob"})
		if st.Reason == "fine" {
			return
		}
		select {
		case <-deadline:
			t.Fatal("subscription died after malformed payload")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}
