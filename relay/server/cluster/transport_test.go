package cluster

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// captureHandler records every frame it sees so tests can assert
// on what arrived without racing against the read loop directly.
type captureHandler struct {
	mu     sync.Mutex
	frames []capturedFrame
	gate   chan struct{}
}

type capturedFrame struct {
	Remote  string
	Type    MsgType
	Payload []byte
}

func newCaptureHandler() *captureHandler {
	return &captureHandler{gate: make(chan struct{}, 16)}
}

func (h *captureHandler) HandleFrame(remote string, t MsgType, payload []byte) error {
	h.mu.Lock()
	h.frames = append(h.frames, capturedFrame{
		Remote:  remote,
		Type:    t,
		Payload: append([]byte(nil), payload...),
	})
	h.mu.Unlock()
	select {
	case h.gate <- struct{}{}:
	default:
	}
	return nil
}

// waitFor blocks until at least n frames have arrived or the test
// times out. Avoids sleeping by polling a channel the handler
// pokes on every received frame.
func (h *captureHandler) waitFor(t *testing.T, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		h.mu.Lock()
		got := len(h.frames)
		h.mu.Unlock()
		if got >= n {
			return
		}
		select {
		case <-h.gate:
		case <-deadline:
			t.Fatalf("only saw %d frames after %s, wanted %d", got, timeout, n)
		}
	}
}

func (h *captureHandler) snapshot() []capturedFrame {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]capturedFrame, len(h.frames))
	copy(out, h.frames)
	return out
}

// startTransport spins a Transport on a free port and returns it
// + the address it bound to. t.Cleanup tears it down.
func startTransport(t *testing.T, h FrameHandler) (*Transport, string) {
	t.Helper()
	tr := NewTransport("127.0.0.1:0", "", h)
	require.NoError(t, tr.ListenAndServe(context.Background()))
	addr := tr.listener.Addr().String()
	t.Cleanup(tr.Stop)
	return tr, addr
}

func TestTransport_DialAndReceiveFrame(t *testing.T) {
	srvHandler := newCaptureHandler()
	_, srvAddr := startTransport(t, srvHandler)

	clientTr, _ := startTransport(t, newCaptureHandler())

	stream, err := clientTr.Dial(context.Background(), srvAddr)
	require.NoError(t, err)
	require.NotNil(t, stream)

	require.NoError(t, stream.Send(MsgPing, nil))
	require.NoError(t, stream.Send(MsgWhoHas, []byte("hello")))

	srvHandler.waitFor(t, 2, 2*time.Second)
	got := srvHandler.snapshot()
	require.Len(t, got, 2)
	require.Equal(t, MsgPing, got[0].Type)
	require.Equal(t, MsgWhoHas, got[1].Type)
	require.Equal(t, []byte("hello"), got[1].Payload)
}

func TestTransport_DialIsIdempotent(t *testing.T) {
	_, srvAddr := startTransport(t, newCaptureHandler())
	clientTr, _ := startTransport(t, newCaptureHandler())

	first, err := clientTr.Dial(context.Background(), srvAddr)
	require.NoError(t, err)

	second, err := clientTr.Dial(context.Background(), srvAddr)
	require.NoError(t, err)

	require.Same(t, first, second, "second dial to the same remote must reuse the live stream")
	require.Equal(t, 1, len(clientTr.Streams()), "exactly one stream per pod-pair")
}

func TestTransport_StopClosesEverything(t *testing.T) {
	_, srvAddr := startTransport(t, newCaptureHandler())
	clientTr := NewTransport("127.0.0.1:0", "", newCaptureHandler())
	require.NoError(t, clientTr.ListenAndServe(context.Background()))

	stream, err := clientTr.Dial(context.Background(), srvAddr)
	require.NoError(t, err)

	clientTr.Stop()

	require.True(t, stream.closed.Load(), "Stop must close every live stream")
	// A second Stop must be a no-op rather than panicking on the
	// already-closed listener / canceled context.
	clientTr.Stop()
}

func TestTransport_StreamReturnsNilWhenAbsent(t *testing.T) {
	tr, _ := startTransport(t, newCaptureHandler())
	require.Nil(t, tr.Stream("127.0.0.1:0"))
}

func TestTransport_HelloKeyingMakesBidirectionalDialCollapse(t *testing.T) {
	// Two pods symmetrically dial each other. With HELLO, both
	// streams are keyed by the OTHER pod's listen address — and
	// the connection-dedup inside handleAccepted / Dial collapses
	// any racing duplicate connection. End state: exactly one
	// stream entry on each side, keyed correctly.
	hA := newCaptureHandler()
	hB := newCaptureHandler()
	tA, addrA := startTransport(t, hA)
	tB, addrB := startTransport(t, hB)

	_, err := tA.Dial(context.Background(), addrB)
	require.NoError(t, err)
	_, err = tB.Dial(context.Background(), addrA)
	require.NoError(t, err)

	// Give the accept side a moment to read HELLO and register.
	require.Eventually(t, func() bool {
		return tA.Stream(addrB) != nil && tB.Stream(addrA) != nil
	}, 2*time.Second, 5*time.Millisecond)

	require.Len(t, tA.Streams(), 1, "A must have exactly one stream after dedup")
	require.Len(t, tB.Streams(), 1, "B must have exactly one stream after dedup")
	require.NotNil(t, tA.Stream(addrB), "A's stream must be keyed by B's announced listen address")
	require.NotNil(t, tB.Stream(addrA), "B's stream must be keyed by A's announced listen address")
}

func TestTransport_AcceptedConnWithoutHelloIsDropped(t *testing.T) {
	srvHandler := newCaptureHandler()
	srv, addr := startTransport(t, srvHandler)

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Send a non-HELLO frame as the first thing. Acceptor must
	// drop the conn instead of registering it.
	require.NoError(t, WriteFrame(conn, MsgWhoHas, make([]byte, peerIDSize)))

	require.Eventually(t, func() bool {
		return len(srv.Streams()) == 0
	}, 2*time.Second, 5*time.Millisecond,
		"transport must reject conns whose first frame is not HELLO")
}

func TestTransport_AcceptedConnWithoutHelloTimesOut(t *testing.T) {
	// A dialer that opens the conn and never sends anything must
	// be reaped by the helloTimeout, not held forever.
	srvHandler := newCaptureHandler()
	srv, addr := startTransport(t, srvHandler)

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Wait past the hello timeout and then some.
	time.Sleep(helloTimeout + 200*time.Millisecond)
	require.Empty(t, srv.Streams(),
		"silent dialers must time out — half-open conns can't accumulate")
}

func TestStream_SendAfterCloseRejects(t *testing.T) {
	srvHandler := newCaptureHandler()
	_, srvAddr := startTransport(t, srvHandler)
	clientTr, _ := startTransport(t, newCaptureHandler())

	stream, err := clientTr.Dial(context.Background(), srvAddr)
	require.NoError(t, err)

	require.NoError(t, stream.Close())
	require.Error(t, stream.Send(MsgPing, nil))
}
