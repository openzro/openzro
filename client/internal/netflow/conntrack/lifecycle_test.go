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

// TestRestartDrainsStaleDoneSentinel is a regression test for the
// quick-Stop-then-Start case. Stop() pushes a sentinel onto c.done
// (capacity 1, buffered) to nudge the receiver out of its select
// loop. Without a drain at the top of Start(), that sentinel lives
// across the next Start: the new receiver goroutine reads it
// immediately on the first iteration, exits, and the listener
// errChan/events are never consumed. The kernel keeps emitting
// conntrack events into a dead channel until the daemon dies or
// the operator restarts the service.
//
// The fix drains the channel on Start. We assert the post-restart
// receiver is alive by triggering an error through the second
// listener — without drain the receiver exits before it can react
// and the mock's Close() is never called.
func TestRestartDrainsStaleDoneSentinel(t *testing.T) {
	first := newMockListener()
	second := newMockListener()

	callCount := atomic.Int32{}
	ct := New(nil, nil, nil, WithDialer(func() (listener, error) {
		n := callCount.Add(1)
		if n == 1 {
			return first, nil
		}
		return second, nil
	}))

	require.NoError(t, ct.Start(false))
	ct.Stop()

	// At this point Stop has fired the sentinel into c.done. With a
	// production-only c.done buffered at capacity 1, that sentinel
	// survives the Stop because no one is around to drain it once
	// the receiver goroutine already exited via its own done case.
	require.NoError(t, ct.Start(false))

	// If the new receiver consumed the stale sentinel, it exited
	// before it could listen on `second.errChan`. The error below
	// would be ignored and `second` would never be closed.
	second.errChan <- errors.New("boom")

	select {
	case <-second.closedCh:
		// Receiver handled the error and tore the listener down,
		// confirming it survived past Start.
	case <-time.After(2 * time.Second):
		t.Fatal("post-restart receiver did not consume errChan — stale done sentinel was not drained")
	}

	ct.Stop()
}

// TestStartIsIdempotent guards against double-Start clobbering the
// active connection. The second call must be a no-op — same
// listener, no extra dial.
func TestStartIsIdempotent(t *testing.T) {
	mock := newMockListener()
	callCount := atomic.Int32{}

	ct := New(nil, nil, nil, WithDialer(func() (listener, error) {
		callCount.Add(1)
		return mock, nil
	}))

	require.NoError(t, ct.Start(false))
	require.NoError(t, ct.Start(false))

	assert.Equal(t, int32(1), callCount.Load(), "second Start should not re-dial")
	ct.Stop()
}
