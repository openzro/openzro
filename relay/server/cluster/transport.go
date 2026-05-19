package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
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
// owns the read loop and serializes writes; multiple goroutines
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
// concurrent calls — frames remain serialized on the wire.
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

// helloTimeout caps how long an accepted connection has to send
// its HELLO frame before we drop it. Real intra-cluster handshakes
// finish in microseconds; 3 s leaves plenty of room for slow CNI
// startup without enabling a long-tail DoS via half-open conns.
const helloTimeout = 3 * time.Second

// classifyHelloReject maps a DecodeHello failure to a metric label.
// Pattern-matching on the error text keeps the cluster package
// decoupled from messages.go's error sentinels — adding a new
// reject reason is a one-line case here.
func classifyHelloReject(err error) HelloRejectReason {
	if err == nil {
		return HelloRejectMalformed
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "hmac mismatch"):
		return HelloRejectHMAC
	case strings.Contains(msg, "requires auth"):
		return HelloRejectUnsigned
	case strings.Contains(msg, "asymmetric"):
		return HelloRejectAsymmetric
	case strings.Contains(msg, "timestamp out of window"):
		return HelloRejectStale
	default:
		return HelloRejectMalformed
	}
}

// Transport owns the inter-pod TCP fabric of one relay pod. It
// listens for inbound connections from other pods and tracks
// outbound streams to remote pods. Streams are keyed by the
// remote pod's announced listen address — the dialer transmits it
// as a HELLO frame, so the accepting side doesn't have to fall
// back to the conn's ephemeral source port (which it can't dial
// back to). The discovery layer (next phase) only has to know
// each pod's listen address, never an ephemeral.
//
// The Transport intentionally has no opinions about discovery,
// reconnection, or peer placement — those layer above it.
type Transport struct {
	listenAddr string

	// announceAddr is what we transmit in our HELLO. In production
	// this is the pod's POD_IP:port reachable from sibling pods —
	// often different from listenAddr (which may be `:7090` or
	// `0.0.0.0:7090`). For tests we set it to the bound address.
	announceAddr string

	// authSecret authenticates the inter-pod fabric. When non-empty,
	// every HELLO carries an HMAC-SHA256 over (version, addr,
	// timestamp); receivers reject unsigned or wrong-hmac frames.
	// Empty (the default) keeps the legacy unsigned format and
	// requires the operator to isolate the backplane via
	// NetworkPolicy or equivalent.
	authSecret []byte

	dialer  net.Dialer
	handler FrameHandler
	metrics *Metrics

	listener net.Listener

	streamsMu sync.RWMutex
	streams   map[string]*Stream // remote announced address → live stream

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// NewTransport constructs a Transport that listens on listenAddr
// for inbound pod connections, dispatches all received frames
// through handler, and announces itself to other pods as
// `announceAddr`. announceAddr must be reachable from sibling pods
// — typically `<pod-ip>:<port>` resolved from the K8s downward API.
// If empty, the transport falls back to whatever address the
// listener bound to (`listener.Addr()`), which is fine for tests
// on 127.0.0.1 but not for cross-pod traffic.
//
// handler may be nil at construction; the locator and forwarder
// usually need a transport reference to be built first, so the
// real handler is wired via SetHandler before ListenAndServe.
//
// Use ListenAndServe to start.
func NewTransport(listenAddr, announceAddr string, handler FrameHandler) *Transport {
	return &Transport{
		listenAddr:   listenAddr,
		announceAddr: announceAddr,
		handler:      handler,
		dialer:       net.Dialer{Timeout: 3 * time.Second},
		streams:      make(map[string]*Stream),
	}
}

// SetHandler swaps the FrameHandler. Must be called before
// ListenAndServe — the read loops capture the field once at
// construction-time and aren't safe to swap mid-flight.
func (t *Transport) SetHandler(h FrameHandler) {
	t.handler = h
}

// SetAuthSecret installs the shared inter-pod HMAC key. Must be
// called before ListenAndServe; the field is read on every Dial
// and Accept and must not change once handshakes start. An empty
// secret keeps the unsigned HELLO format (legacy / NetworkPolicy
// trust). The byte slice is stored by reference — don't mutate it
// after the call.
func (t *Transport) SetAuthSecret(secret []byte) {
	t.authSecret = secret
}

// SetMetrics installs the cluster metrics handle and wires the
// streams gauge to this transport's live stream count. Must be
// called before ListenAndServe; passing nil disables
// instrumentation. The *Metrics lifetime is owned by the caller.
func (t *Transport) SetMetrics(m *Metrics) {
	t.metrics = m
	if m != nil {
		m.SetStreamSource(func() int {
			t.streamsMu.RLock()
			defer t.streamsMu.RUnlock()
			n := 0
			for _, s := range t.streams {
				if !s.closed.Load() {
					n++
				}
			}
			return n
		})
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

	// Fall back to the bound address when the caller didn't pass an
	// explicit announcement — tests use 127.0.0.1:0 and rely on
	// this; production wires in POD_IP:port and never hits this.
	if t.announceAddr == "" {
		t.announceAddr = ln.Addr().String()
	}

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

	log.Infof("cluster: transport listening on %s, announcing %s", t.listenAddr, t.announceAddr)
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
//
// Right after the TCP connection establishes, Dial transmits a
// HELLO frame announcing this pod's address. The accepting side
// uses that to key its streams map by the same logical address —
// avoiding the ephemeral-source-port problem that would otherwise
// make the inverse lookup impossible on the accepted side.
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

	// Announce who we are. We send HELLO before any other frame so
	// the accepting side can key its stream map on our listen
	// address (not the ephemeral source port of this conn). The
	// payload carries an HMAC when authSecret is set, so the peer
	// can authenticate us before adding the stream to its map.
	hello, err := EncodeHello(t.announceAddr, t.authSecret, time.Now())
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("cluster transport: encode HELLO: %w", err)
	}
	if err := s.Send(MsgHello, hello); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("cluster transport: send HELLO to %s: %w", remote, err)
	}

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

		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			t.handleAccepted(ctx, conn)
		}()
	}
}

// handleAccepted reads the HELLO frame off a freshly-accepted conn
// and registers the stream keyed by the announced listen address.
// Runs in its own goroutine so a single slow / misbehaving dialer
// doesn't stall the accept loop.
func (t *Transport) handleAccepted(ctx context.Context, conn net.Conn) {
	// Bound the HELLO read so half-open conns don't accumulate.
	_ = conn.SetReadDeadline(time.Now().Add(helloTimeout))
	msgType, payload, err := ReadFrame(conn)
	_ = conn.SetReadDeadline(time.Time{}) // back to no deadline for the steady stream
	if err != nil {
		log.Warnf("cluster: accept from %s: read HELLO: %v", conn.RemoteAddr(), err)
		t.metrics.IncHelloReject(ctx, HelloRejectTimeout)
		_ = conn.Close()
		return
	}
	if msgType != MsgHello {
		log.Warnf("cluster: accept from %s: first frame was %s, expected HELLO; dropping", conn.RemoteAddr(), msgType)
		t.metrics.IncHelloReject(ctx, HelloRejectWrongFirst)
		_ = conn.Close()
		return
	}
	announced, err := DecodeHello(payload, t.authSecret, time.Now())
	if err != nil {
		log.Warnf("cluster: accept from %s: rejecting HELLO: %v", conn.RemoteAddr(), err)
		t.metrics.IncHelloReject(ctx, classifyHelloReject(err))
		_ = conn.Close()
		return
	}

	s := newStream(announced, conn)

	t.streamsMu.Lock()
	// If we already have a stream to this announced address, drop
	// the newcomer and keep the existing. Two pods racing to
	// initiate land here exactly once each — the second one wins
	// the race on the dialing side or this side, never both.
	if existing, ok := t.streams[announced]; ok && !existing.closed.Load() {
		t.streamsMu.Unlock()
		log.Debugf("cluster: incoming dup stream from %s — keeping existing", announced)
		_ = s.Close()
		return
	}
	t.streams[announced] = s
	t.streamsMu.Unlock()

	if ctx.Err() != nil {
		t.dropStream(announced, s)
		_ = s.Close()
		return
	}

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		s.readLoop(t.handler)
		t.dropStream(announced, s)
	}()
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
