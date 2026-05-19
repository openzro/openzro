package client

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServerPicker_UnavailableServers(t *testing.T) {
	// PickServer caps every unavailable-server run at its own
	// connectionTimeout (WS refuses instantly, but QUIC-over-UDP to a
	// dead port never refuses — it only ends when this timeout fires).
	// Keep it small so the test is fast; the value is arbitrary, it
	// just bounds PickServer.
	connectionTimeout = 2 * time.Second

	sp := ServerPicker{
		TokenStore: nil,
		PeerID:     "test",
	}
	// IP literals on closed loopback ports — NOT hostnames. With
	// "dummy1"/"dummy2" the dial path went through the OS resolver;
	// the macOS CI runner resolves dotless bogus names slowly, which
	// pushed PickServer past the (already too-tight, see below) test
	// deadline and flaked. IP literals need no resolver at all.
	sp.ServerURLs.Store([]string{"rel://127.0.0.1:1", "rel://127.0.0.1:2"})

	// + time.Second, NOT + 1. The original "+ 1" added one
	// NANOSECOND (connectionTimeout is a time.Duration), making the
	// test deadline essentially equal to PickServer's own
	// connectionTimeout — a dead heat the deadline wins, so an
	// all-unavailable run reported "took too long" even though
	// PickServer behaved correctly. A real 1s margin lets PickServer
	// return its error before the deadline, deterministically on
	// every OS regardless of resolver speed.
	ctx, cancel := context.WithTimeout(context.Background(), connectionTimeout+time.Second)
	defer cancel()

	go func() {
		_, err := sp.PickServer(ctx)
		if err == nil {
			t.Error(err)
		}
		cancel()
	}()

	<-ctx.Done()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Errorf("PickServer() took too long to complete")
	}
}
