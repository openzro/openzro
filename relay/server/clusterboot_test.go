package server

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/relay/server/store"
)

func TestStartCluster_RejectsBadConfig(t *testing.T) {
	st := store.NewStore()

	cases := []struct {
		name string
		cfg  ClusterBootstrapConfig
	}{
		{"missing headless", ClusterBootstrapConfig{PodIP: "10.0.0.1"}},
		{"missing pod ip", ClusterBootstrapConfig{Headless: "x"}},
		{"negative port", ClusterBootstrapConfig{Headless: "x", PodIP: "10.0.0.1", Port: -1}},
		{"port too high", ClusterBootstrapConfig{Headless: "x", PodIP: "10.0.0.1", Port: 70000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := StartCluster(context.Background(), st, tc.cfg)
			require.Error(t, err)
		})
	}

	t.Run("nil store", func(t *testing.T) {
		_, err := StartCluster(context.Background(), nil, ClusterBootstrapConfig{
			Headless: "x", PodIP: "10.0.0.1",
		})
		require.Error(t, err)
	})
}

// TestStartCluster_SmokeListensAndStops binds the transport on a
// random local port and confirms StartCluster returns a usable
// bootstrap (transport listening, discovery running) before Stop
// tears it down without panicking. The discovery loop will fail to
// resolve the bogus headless name — that's fine, it logs at debug
// and keeps ticking.
func TestStartCluster_SmokeListensAndStops(t *testing.T) {
	st := store.NewStore()

	cb, err := StartCluster(context.Background(), st, ClusterBootstrapConfig{
		Headless: "nope.invalid",
		Port:     freePort(t),
		PodIP:    "127.0.0.1",
		Interval: 50 * time.Millisecond,
	})
	require.NoError(t, err)
	require.NotNil(t, cb)
	require.NotNil(t, cb.Transport)
	require.NotNil(t, cb.Locator)
	require.NotNil(t, cb.Forwarder)
	require.NotNil(t, cb.Discovery)

	// Confirm Stop is idempotent and nil-safe.
	cb.Stop()
	cb.Stop()
	(*ClusterBootstrap)(nil).Stop()
}

// freePort grabs a port by binding ":0" briefly and reading what
// the kernel handed us. There's an inherent TOCTOU window before
// StartCluster reuses it, but on test hosts collisions are rare
// and a single retry is enough — keep it simple.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}
