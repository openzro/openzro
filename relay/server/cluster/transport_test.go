package cluster

import (
	"context"
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
	tr := NewTransport("127.0.0.1:0", h)
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
	clientTr := NewTransport("127.0.0.1:0", newCaptureHandler())
	require.NoError(t, clientTr.ListenAndServe(context.Background()))

	stream, err := clientTr.Dial(context.Background(), srvAddr)
	require.NoError(t, err)

	clientTr.Stop()

	require.True(t, stream.closed.Load(), "Stop must close every live stream")
	// A second Stop must be a no-op rather than panicking on the
	// already-closed listener / cancelled context.
	clientTr.Stop()
}

func TestTransport_StreamReturnsNilWhenAbsent(t *testing.T) {
	tr, _ := startTransport(t, newCaptureHandler())
	require.Nil(t, tr.Stream("127.0.0.1:0"))
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
