package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

// Default port used between relay pods inside a cluster. The chart
// exposes this on a Headless Service that's NetworkPolicy-isolated
// from the rest of the namespace.
const DefaultInterpodPort = 7090

// FrameHandler dispatches an inbound frame for the caller to act
// on. The remote string is the peer pod's address (`host:port`),
// useful for logs and for deciding where a reply goes.
//
// Returning a non-nil error closes the stream — the remote pod will
// dial back as part of its discovery loop. Don't return errors for
// expected-bad-input; ignore them and keep serving.
type FrameHandler interface {
	HandleFrame(remote string, t MsgType, payload []byte) error
}

// Stream wraps one long-lived TCP connection between two pods. It
// owns the read loop and serialises writes; multiple goroutines
// can call Send concurrently and the frames stay framed correctly.
type Stream struct {
	remote string
	conn   net.Conn

	// writeMu protects against interleaved frame headers when two
	// goroutines write at once (e.g. one goroutine sending DATA on
	// channel X, another sending OPEN_ACK for channel Y).
	writeMu sync.Mutex

	closed atomic.Bool
}

func newStream(remote string, conn net.Conn) *Stream {
	return &Stream{
		remote: remote,
		conn:   conn,
	}
}

// Send writes a single framed message to the remote pod. Safe for
// concurrent calls — frames remain serialised on the wire.
func (s *Stream) Send(t MsgType, payload []byte) error {
	if s.closed.Load() {
		return net.ErrClosed
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := WriteFrame(s.conn, t, payload); err != nil {
		// One write failure normally means the peer disconnected;
		// flag closed so subsequent writers don't keep blocking.
		s.closed.Store(true)
		return err
	}
	return nil
}

// Remote returns the remote pod's address (host:port).
func (s *Stream) Remote() string { return s.remote }

// Close terminates the stream. Idempotent.
func (s *Stream) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	return s.conn.Close()
}

// readLoop reads frames off the connection and dispatches them
// through the handler. Returns when the connection closes or the
// handler asks us to stop. Caller is expected to invoke Close on
// return so the writer side also unblocks.
func (s *Stream) readLoop(handler FrameHandler) {
	defer s.Close()
	for {
		t, payload, err := ReadFrame(s.conn)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				log.Debugf("cluster: stream from %s closed cleanly", s.remote)
				return
			}
			if errors.Is(err, ErrShortHeader) || errors.Is(err, ErrShortPayload) {
				log.Warnf("cluster: stream from %s truncated mid-frame, closing: %v", s.remote, err)
				return
			}
			log.Warnf("cluster: stream from %s read error, closing: %v", s.remote, err)
			return
		}

		if handler == nil {
			// No handler yet (transient state during startup);
			// silently drop frames. Better than panicking.
			continue
		}
		if err := handler.HandleFrame(s.remote, t, payload); err != nil {
			log.Warnf("cluster: handler for stream %s requested close: %v", s.remote, err)
			return
		}
	}
}

// Transport owns the inter-pod TCP fabric of one relay pod. It
// listens for inbound connections from other pods and tracks
// outbound streams to remote pods. Streams are addressed by remote
// `host:port` — the discovery layer (next phase) will translate
// `pod-2.relay-internal.svc` into the right address.
//
// The Transport intentionally has no opinions about discovery,
// reconnection, or peer placement — those layer above it.
type Transport struct {
	listenAddr string
	dialer     net.Dialer
	handler    FrameHandler

	listener net.Listener

	streamsMu sync.RWMutex
	streams   map[string]*Stream // remote address → live stream

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// NewTransport constructs a Transport that listens on listenAddr
// for inbound pod connections and dispatches all received frames
// through handler. Use ListenAndServe to start.
func NewTransport(listenAddr string, handler FrameHandler) *Transport {
	return &Transport{
		listenAddr: listenAddr,
		handler:    handler,
		dialer:     net.Dialer{Timeout: 3 * time.Second},
		streams:    make(map[string]*Stream),
	}
}

// ListenAndServe binds the listener and accepts inbound pod
// connections in the background. Blocks only until the listen call
// itself returns; the accept loop runs in a goroutine.
//
// Cancel ctx (via Stop) to shut down. The listener and all live
// streams are closed; ListenAndServe returns nil on graceful stop.
func (t *Transport) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", t.listenAddr)
	if err != nil {
		return fmt.Errorf("cluster transport: listen %s: %w", t.listenAddr, err)
	}
	t.listener = ln

	ctx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		<-ctx.Done()
		_ = ln.Close()
	}()

	t.wg.Add(1)
	go t.acceptLoop(ctx)

	log.Infof("cluster: transport listening on %s", t.listenAddr)
	return nil
}

// Stop tears down the transport: closes the listener, cancels the
// accept loop, closes every live stream, and waits for goroutines
// to exit. Safe to call more than once.
func (t *Transport) Stop() {
	if t.cancel != nil {
		t.cancel()
	}

	t.streamsMu.Lock()
	for _, s := range t.streams {
		_ = s.Close()
	}
	t.streams = make(map[string]*Stream)
	t.streamsMu.Unlock()

	t.wg.Wait()
}

// Dial opens a new outbound stream to the given remote pod address
// and registers it in the streams map. If a live stream already
// exists for that address, the existing one is returned and no new
// connection is opened — Stream is one per pod-pair regardless of
// who initiated.
func (t *Transport) Dial(ctx context.Context, remote string) (*Stream, error) {
	t.streamsMu.RLock()
	if existing, ok := t.streams[remote]; ok && !existing.closed.Load() {
		t.streamsMu.RUnlock()
		return existing, nil
	}
	t.streamsMu.RUnlock()

	conn, err := t.dialer.DialContext(ctx, "tcp", remote)
	if err != nil {
		return nil, fmt.Errorf("cluster transport: dial %s: %w", remote, err)
	}

	s := newStream(remote, conn)

	t.streamsMu.Lock()
	if existing, ok := t.streams[remote]; ok && !existing.closed.Load() {
		// Lost the race against another Dial on the same remote;
		// drop ours and return the winner.
		t.streamsMu.Unlock()
		_ = s.Close()
		return existing, nil
	}
	t.streams[remote] = s
	t.streamsMu.Unlock()

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		s.readLoop(t.handler)
		t.dropStream(remote, s)
	}()

	return s, nil
}

// Stream returns the live stream to remote, or nil if none exists.
// Used by the locator / forwarder layers to send a frame without
// needing to dial.
func (t *Transport) Stream(remote string) *Stream {
	t.streamsMu.RLock()
	defer t.streamsMu.RUnlock()
	if s, ok := t.streams[remote]; ok && !s.closed.Load() {
		return s
	}
	return nil
}

// Streams returns a snapshot of every live stream. Useful for
// broadcasting (WHO_HAS) without holding the map lock during the
// per-stream send. The returned map is owned by the caller.
func (t *Transport) Streams() map[string]*Stream {
	t.streamsMu.RLock()
	defer t.streamsMu.RUnlock()
	out := make(map[string]*Stream, len(t.streams))
	for k, v := range t.streams {
		if !v.closed.Load() {
			out[k] = v
		}
	}
	return out
}

func (t *Transport) acceptLoop(ctx context.Context) {
	defer t.wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		conn, err := t.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return
			}
			log.Warnf("cluster: accept error, continuing: %v", err)
			// Backoff a touch — runaway accept failures are a
			// surprise and not worth burning CPU on.
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}

		remote := conn.RemoteAddr().String()
		s := newStream(remote, conn)

		t.streamsMu.Lock()
		// If we already have a stream to this remote, prefer the
		// older one and drop the newer. Both pods racing to
		// initiate would otherwise leave us with two streams to
		// the same place. Tie-break: keep what's there.
		if existing, ok := t.streams[remote]; ok && !existing.closed.Load() {
			t.streamsMu.Unlock()
			log.Debugf("cluster: incoming dup stream from %s — dropping new conn", remote)
			_ = s.Close()
			continue
		}
		t.streams[remote] = s
		t.streamsMu.Unlock()

		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			s.readLoop(t.handler)
			t.dropStream(remote, s)
		}()
	}
}

// dropStream removes a stream from the map only if the entry is
// still the same one we put there — avoids a race where a
// reconnect already replaced the entry by the time the old read
// loop returns.
func (t *Transport) dropStream(remote string, s *Stream) {
	t.streamsMu.Lock()
	defer t.streamsMu.Unlock()
	if cur, ok := t.streams[remote]; ok && cur == s {
		delete(t.streams, remote)
	}
}
