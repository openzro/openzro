package mdm

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingProvider counts Get calls and records the lookups it was
// asked about. Used to assert that the refresh worker (a) iterates
// the cached entries on tick, and (b) only goes to the underlying
// provider when the cache TTL has expired.
type countingProvider struct {
	mu      sync.Mutex
	calls   atomic.Uint64
	lookups []DeviceLookup
}

func (c *countingProvider) Type() ProviderType { return "fake" }
func (c *countingProvider) Close() error       { return nil }
func (c *countingProvider) GetDeviceStatus(_ context.Context, l DeviceLookup) (DeviceStatus, error) {
	c.calls.Add(1)
	c.mu.Lock()
	c.lookups = append(c.lookups, l)
	c.mu.Unlock()
	return DeviceStatus{Compliant: true}, nil
}

func TestRefreshWorker_TouchesEveryCachedEntryAfterTTLExpires(t *testing.T) {
	inner := &countingProvider{}
	// 80ms TTL → entries expire shortly after the first prime.
	// Worker fires at 50ms so the second pass sees expired entries.
	cached := NewCachedProvider(inner, 80*time.Millisecond)

	// Prime the cache with two distinct lookups (different keys).
	ctx := context.Background()
	_, err := cached.GetDeviceStatus(ctx, DeviceLookup{Hostname: "alice"})
	require.NoError(t, err)
	_, err = cached.GetDeviceStatus(ctx, DeviceLookup{Hostname: "bob", UserEmail: "b@x"})
	require.NoError(t, err)
	require.Equal(t, uint64(2), inner.calls.Load(), "prime should hit inner twice")

	w := startRefreshWorker(cached, 50*time.Millisecond, 1, "fake")
	defer w.Stop()

	// Wait long enough for the jittered first fire AND a subsequent
	// tick at 50ms past TTL expiry. Worker calls into the cached
	// provider, expired entries refetch → counter climbs to 4.
	deadline := time.After(1 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("refresh worker never re-touched entries, calls=%d", inner.calls.Load())
		default:
			if inner.calls.Load() >= 4 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestRefreshWorker_SkipsEntriesStillInsideTTL(t *testing.T) {
	inner := &countingProvider{}
	// Long TTL so entries stay valid for the duration of the test.
	cached := NewCachedProvider(inner, 1*time.Hour)

	_, err := cached.GetDeviceStatus(context.Background(),
		DeviceLookup{Hostname: "alice"})
	require.NoError(t, err)
	require.Equal(t, uint64(1), inner.calls.Load())

	// Worker tick 30ms — fires multiple times within the test window.
	w := startRefreshWorker(cached, 30*time.Millisecond, 1, "fake")
	defer w.Stop()

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, uint64(1), inner.calls.Load(),
		"entries inside TTL must be served from cache — no extra inner calls")
}

func TestRefreshWorker_StopIsIdempotent(t *testing.T) {
	cached := NewCachedProvider(&countingProvider{}, 1*time.Second)
	w := startRefreshWorker(cached, 50*time.Millisecond, 1, "fake")

	// Double-stop must not panic on close-of-closed-channel — the
	// Manager.Refresh + Manager.Close sequence racing on shutdown
	// is the production scenario.
	w.Stop()
	w.Stop()
}

// slowProvider blocks GetDeviceStatus until ctx is cancelled.
// Models a vendor API that has gone slow / hung in production.
type slowProvider struct{}

func (slowProvider) Type() ProviderType { return "slow" }
func (slowProvider) Close() error       { return nil }
func (slowProvider) GetDeviceStatus(ctx context.Context, _ DeviceLookup) (DeviceStatus, error) {
	<-ctx.Done()
	return DeviceStatus{}, ctx.Err()
}

func TestRefreshWorker_StopInterruptsInFlightVendorCall(t *testing.T) {
	// Regression: previously the worker's per-tick ctx was a fresh
	// context.Background() WithTimeout, so a Stop() during a slow
	// vendor call had to wait for the full 30s timeout. With the
	// per-worker ctx, Stop must propagate into the in-flight call
	// and return promptly.
	cached := NewCachedProvider(slowProvider{}, 1*time.Millisecond)
	// Seed the cache directly via the broker path so the slow inner
	// provider doesn't get called on the prime — and give it a
	// 1ms TTL so the entry is already expired by the time the
	// worker fires, forcing a refetch.
	cached.putFromBroker(DeviceLookup{Hostname: "alice"},
		DeviceStatus{Compliant: true})
	time.Sleep(5 * time.Millisecond)

	w := startRefreshWorker(cached, 5*time.Millisecond, 1, "slow")

	// Give the worker time to: clear the initial jitter, tick once,
	// enter GetDeviceStatus, and block inside slowProvider.
	time.Sleep(60 * time.Millisecond)

	stopDone := make(chan struct{})
	go func() {
		w.Stop()
		close(stopDone)
	}()

	select {
	case <-stopDone:
		// Good — Stop returned before the 30s timeout could elapse.
	case <-time.After(2 * time.Second):
		t.Fatalf("Stop() blocked on slow vendor — ctx cancellation does not reach the in-flight call")
	}
}

func TestCacheKey_RoundTripsHostnameOnly(t *testing.T) {
	in := DeviceLookup{Hostname: "alice-laptop"}
	out := decodeCacheKey(encodeCacheKey(in))
	assert.Equal(t, in, out)
}

func TestCacheKey_RoundTripsHostnameAndUserEmail(t *testing.T) {
	in := DeviceLookup{Hostname: "alice-laptop", UserEmail: "alice@example.com"}
	out := decodeCacheKey(encodeCacheKey(in))
	assert.Equal(t, in, out)
}
