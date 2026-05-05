package cluster

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeResolver returns whatever IPs the test set, optionally
// returning an error to simulate a DNS hiccup.
type fakeResolver struct {
	mu    sync.Mutex
	ips   []string
	err   error
	calls atomic.Uint32
}

func (f *fakeResolver) LookupHost(_ context.Context, _ string) ([]string, error) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := make([]string, len(f.ips))
	copy(out, f.ips)
	return out, nil
}

func (f *fakeResolver) set(ips []string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ips = append([]string(nil), ips...)
	f.err = err
}

// startTransportOn launches a Transport listening on
// "127.0.0.1:<port>" so the discovery dial can hit a real port.
// Returns the actual bound address (port is dynamic).
func startTransportOn(t *testing.T, h FrameHandler) (*Transport, string, int) {
	t.Helper()
	tr := NewTransport("127.0.0.1:0", "", h)
	require.NoError(t, tr.ListenAndServe(context.Background()))
	t.Cleanup(tr.Stop)
	addr := tr.listener.Addr().String()
	_, portStr, err := splitHostPort(addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)
	return tr, addr, port
}

// splitHostPort wraps net.SplitHostPort to keep imports tidy.
func splitHostPort(addr string) (string, string, error) {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i], addr[i+1:], nil
		}
	}
	return "", "", errors.New("no port")
}

func TestDiscovery_FirstReconcileDialsAllPeers(t *testing.T) {
	srv1, _, port1 := startTransportOn(t, newCaptureHandler())
	_ = srv1
	// Discovery dials 127.0.0.1:<port> — both srv1 and the
	// dialer's transport accept on 127.0.0.1.
	tr, _, _ := startTransportOn(t, newCaptureHandler())

	resolver := &fakeResolver{}
	resolver.set([]string{"127.0.0.1"}, nil) // resolves to srv1's host

	disc, err := NewDiscovery(tr, DiscoveryConfig{
		Headless: "relay-internal",
		Port:     port1,
		SelfIP:   "10.0.0.99", // not in the resolved set, so no self-filter triggers
		Interval: 50 * time.Millisecond,
		Resolver: resolver,
	})
	require.NoError(t, err)

	hookFired := make(chan struct{}, 4)
	disc.onTickedHook = func(_, _ []string) {
		select {
		case hookFired <- struct{}{}:
		default:
		}
	}

	disc.Start(context.Background())
	t.Cleanup(disc.Stop)

	select {
	case <-hookFired:
	case <-time.After(time.Second):
		t.Fatal("first reconcile hook did not fire")
	}

	// After the first reconcile, dial should have happened. Wait a
	// beat for the dial goroutine + HELLO handshake to settle.
	require.Eventually(t, func() bool {
		return len(tr.Streams()) >= 1
	}, time.Second, 5*time.Millisecond,
		"discovery's first tick must trigger a Dial that produces a stream")
}

func TestDiscovery_FiltersSelfIP(t *testing.T) {
	tr, _, _ := startTransportOn(t, newCaptureHandler())

	resolver := &fakeResolver{}
	resolver.set([]string{"127.0.0.1", "10.99.99.99"}, nil)

	disc, err := NewDiscovery(tr, DiscoveryConfig{
		Headless: "relay-internal",
		Port:     7090,
		SelfIP:   "127.0.0.1", // matches one of the resolved IPs
		Interval: 50 * time.Millisecond,
		Resolver: resolver,
	})
	require.NoError(t, err)

	tickFired := make(chan struct{}, 4)
	disc.onTickedHook = func(_, _ []string) {
		select {
		case tickFired <- struct{}{}:
		default:
		}
	}

	disc.Start(context.Background())
	t.Cleanup(disc.Stop)

	select {
	case <-tickFired:
	case <-time.After(time.Second):
		t.Fatal("tick hook did not fire")
	}

	known := disc.Known()
	require.Equal(t, []string{"10.99.99.99"}, known,
		"self IP must be filtered out of the known set")
}

func TestDiscovery_AddedAndRemovedDiff(t *testing.T) {
	tr, _, _ := startTransportOn(t, newCaptureHandler())
	resolver := &fakeResolver{}
	resolver.set([]string{"10.0.0.1"}, nil)

	disc, err := NewDiscovery(tr, DiscoveryConfig{
		Headless: "relay-internal",
		Port:     7090,
		Interval: 30 * time.Millisecond,
		Resolver: resolver,
	})
	require.NoError(t, err)

	type tick struct {
		added   []string
		removed []string
	}
	ticks := make(chan tick, 16)
	disc.onTickedHook = func(a, r []string) {
		ticks <- tick{
			added:   append([]string(nil), a...),
			removed: append([]string(nil), r...),
		}
	}

	disc.Start(context.Background())
	t.Cleanup(disc.Stop)

	// First tick: 10.0.0.1 added.
	first := <-ticks
	sort.Strings(first.added)
	require.Equal(t, []string{"10.0.0.1"}, first.added)
	require.Empty(t, first.removed)

	// Update DNS: 10.0.0.1 gone, 10.0.0.2 + 10.0.0.3 added.
	resolver.set([]string{"10.0.0.2", "10.0.0.3"}, nil)

	for {
		select {
		case ev := <-ticks:
			if len(ev.added) == 2 && len(ev.removed) == 1 {
				sort.Strings(ev.added)
				require.Equal(t, []string{"10.0.0.2", "10.0.0.3"}, ev.added)
				require.Equal(t, []string{"10.0.0.1"}, ev.removed)
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatal("discovery did not observe DNS change within 2s")
		}
	}
}

func TestDiscovery_ResolverErrorIsTransparent(t *testing.T) {
	tr, _, _ := startTransportOn(t, newCaptureHandler())
	resolver := &fakeResolver{}
	resolver.set(nil, errors.New("simulated DNS hiccup"))

	disc, err := NewDiscovery(tr, DiscoveryConfig{
		Headless: "relay-internal",
		Port:     7090,
		Interval: 30 * time.Millisecond,
		Resolver: resolver,
	})
	require.NoError(t, err)
	disc.Start(context.Background())
	t.Cleanup(disc.Stop)

	// Wait for at least one resolve attempt.
	require.Eventually(t, func() bool {
		return resolver.calls.Load() >= 1
	}, time.Second, 5*time.Millisecond)

	require.Empty(t, disc.Known(),
		"a failing resolver must leave the known set empty rather than corrupt it")
	// Recover: the next reconcile after the resolver returns
	// addresses must populate Known.
	resolver.set([]string{"10.0.0.7"}, nil)
	require.Eventually(t, func() bool {
		return len(disc.Known()) == 1 && disc.Known()[0] == "10.0.0.7"
	}, 2*time.Second, 10*time.Millisecond,
		"discovery must recover after a transient resolver error")
}

func TestNewDiscovery_RejectsBadConfig(t *testing.T) {
	tr, _, _ := startTransportOn(t, newCaptureHandler())

	cases := []struct {
		name string
		cfg  DiscoveryConfig
	}{
		{"missing headless", DiscoveryConfig{Port: 7090}},
		{"zero port", DiscoveryConfig{Headless: "x"}},
		{"negative port", DiscoveryConfig{Headless: "x", Port: -1}},
		{"port too high", DiscoveryConfig{Headless: "x", Port: 70000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewDiscovery(tr, tc.cfg)
			require.Error(t, err)
		})
	}
	t.Run("nil transport", func(t *testing.T) {
		_, err := NewDiscovery(nil, DiscoveryConfig{Headless: "x", Port: 7090})
		require.Error(t, err)
	})
}

func TestDiscovery_StopIsIdempotent(t *testing.T) {
	tr, _, _ := startTransportOn(t, newCaptureHandler())
	disc, err := NewDiscovery(tr, DiscoveryConfig{
		Headless: "relay-internal",
		Port:     7090,
		Resolver: &fakeResolver{},
	})
	require.NoError(t, err)
	disc.Start(context.Background())
	disc.Stop()
	disc.Stop() // second Stop must not panic on the already-closed channel
}
