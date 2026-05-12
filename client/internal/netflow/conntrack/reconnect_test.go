//go:build linux && !android

package conntrack

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shortenReconnect compresses the backoff window so tests run in
// milliseconds instead of the production 5-second minimum.
// Restored via the returned cleanup func so other tests are not
// affected.
func shortenReconnect(t *testing.T) {
	t.Helper()
	origInit, origMax := reconnectInitInterval, reconnectMaxInterval
	reconnectInitInterval = 10 * time.Millisecond
	reconnectMaxInterval = 50 * time.Millisecond
	t.Cleanup(func() {
		reconnectInitInterval = origInit
		reconnectMaxInterval = origMax
	})
}

// TestReconnect_AfterListenerError exercises the recovery path that
// closes the gap our pre-fix code had: any error on the conntrack
// netlink listener killed the receiver permanently. The kernel
// surfaces transient EPERMs (module reload, dispatcher restart,
// network namespace move) routinely, and operators rebooted the
// agent to get traffic capture back. Post-fix the receiver loops on
// backoff until a fresh dial succeeds.
func TestReconnect_AfterListenerError(t *testing.T) {
	shortenReconnect(t)

	first := newMockListener()
	second := newMockListener()
	third := newMockListener()
	listeners := []*mockListener{first, second, third}
	callCount := atomic.Int32{}

	ct := New(nil, nil, nil, WithDialer(func() (listener, error) {
		n := int(callCount.Add(1)) - 1
		return listeners[n], nil
	}))

	require.NoError(t, ct.Start(false))

	first.errChan <- errors.New("simulated netlink boom")

	require.Eventually(t, func() bool {
		return callCount.Load() >= 2
	}, 2*time.Second, 10*time.Millisecond, "reconnect should dial a second listener")

	select {
	case <-first.closedCh:
	case <-time.After(time.Second):
		t.Fatal("first listener was not closed after error")
	}

	// Receiver is still alive — feed it a second error and verify it
	// reconnects again instead of dying for good.
	second.errChan <- errors.New("second boom")

	require.Eventually(t, func() bool {
		return callCount.Load() >= 3
	}, 2*time.Second, 10*time.Millisecond, "second reconnect should also succeed")

	ct.Stop()
}

// TestReconnect_RetriesOnDialFailure asserts the receiver does not
// give up when the dial itself fails. Kernel module reload on a
// busy host can return ENODEV for a few seconds; we need to keep
// trying.
func TestReconnect_RetriesOnDialFailure(t *testing.T) {
	shortenReconnect(t)

	first := newMockListener()
	good := newMockListener()
	dialCount := atomic.Int32{}

	ct := New(nil, nil, nil, WithDialer(func() (listener, error) {
		n := dialCount.Add(1)
		switch n {
		case 1:
			return first, nil
		case 2:
			return nil, errors.New("ENODEV: kernel module not loaded yet")
		default:
			return good, nil
		}
	}))

	require.NoError(t, ct.Start(false))
	first.errChan <- errors.New("kernel reset")

	require.Eventually(t, func() bool {
		return dialCount.Load() >= 3
	}, 2*time.Second, 10*time.Millisecond, "reconnect should retry past the failed dial")

	ct.Stop()
}

// TestReconnect_StopDuringBackoff confirms the operator-initiated
// Stop wins the race against the backoff timer. Without the
// `<-c.done` branch in reconnect(), Stop would have to wait up to
// reconnectMaxInterval before the goroutine noticed it.
func TestReconnect_StopDuringBackoff(t *testing.T) {
	shortenReconnect(t)
	// Push max interval up so the test guarantees Stop hits the
	// sleeping reconnect loop before the timer fires.
	origMax := reconnectMaxInterval
	reconnectInitInterval = 500 * time.Millisecond
	reconnectMaxInterval = 5 * time.Second
	t.Cleanup(func() { reconnectMaxInterval = origMax })

	first := newMockListener()
	never := newMockListener()
	callCount := atomic.Int32{}

	ct := New(nil, nil, nil, WithDialer(func() (listener, error) {
		n := callCount.Add(1)
		if n == 1 {
			return first, nil
		}
		return never, nil
	}))

	require.NoError(t, ct.Start(false))
	first.errChan <- errors.New("die")

	// Wait for first listener to be closed (entered reconnect).
	select {
	case <-first.closedCh:
	case <-time.After(time.Second):
		t.Fatal("first listener was not closed before reconnect started")
	}

	// Stop while reconnect is sleeping on the backoff timer.
	done := make(chan struct{})
	go func() {
		ct.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not interrupt reconnect backoff sleep within 2s")
	}

	ct.mux.Lock()
	assert.False(t, ct.started, "started must be false after Stop interrupts reconnect")
	assert.Nil(t, ct.conn, "conn must be nil after Stop")
	ct.mux.Unlock()
}
