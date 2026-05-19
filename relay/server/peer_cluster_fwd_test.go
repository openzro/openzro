package server

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/openzro/openzro/relay/messages"
	"github.com/openzro/openzro/relay/metrics"
	"github.com/openzro/openzro/relay/server/cluster"
	"github.com/openzro/openzro/relay/server/store"
)

// fakeForwarder implements CrossPodForwarder. It records the bytes
// it was asked to forward and lets the test choose the error it
// returns — including cluster.ErrPeerNotFound to simulate
// "nobody owns this peer".
type fakeForwarder struct {
	mu      sync.Mutex
	calls   int
	lastDst messages.PeerID
	lastMsg []byte
	retErr  error
	// locateOK governs the Locate response. Defaults to false (peer
	// not found anywhere) to keep older tests working unchanged;
	// the subscribe-via-fabric tests flip this on per-case.
	locateOK  bool
	locatePod string
}

func (f *fakeForwarder) Forward(_ context.Context, dst messages.PeerID, msg []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastDst = dst
	f.lastMsg = append([]byte(nil), msg...)
	return f.retErr
}

func (f *fakeForwarder) Locate(_ context.Context, _ messages.PeerID) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.locatePod, f.locateOK
}

// recordingConn is a minimal net.Conn that buffers everything
// written to it. Read/Close/etc. are never used by handleTransportMsg
// so they can be no-ops.
type recordingConn struct {
	mu      sync.Mutex
	written []byte
}

func (c *recordingConn) Read([]byte) (int, error)         { return 0, errors.New("not used") }
func (c *recordingConn) Close() error                     { return nil }
func (c *recordingConn) LocalAddr() net.Addr              { return &net.UnixAddr{} }
func (c *recordingConn) RemoteAddr() net.Addr             { return &net.UnixAddr{} }
func (c *recordingConn) SetDeadline(time.Time) error      { return nil }
func (c *recordingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *recordingConn) SetWriteDeadline(time.Time) error { return nil }
func (c *recordingConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.written = append(c.written, b...)
	return len(b), nil
}
func (c *recordingConn) snapshot() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.written...)
}

func mustMetrics(t *testing.T) *metrics.Metrics {
	t.Helper()
	m, err := metrics.NewMetrics(context.Background(), otel.Meter("relay-test"))
	require.NoError(t, err)
	return m
}

func newPeerForTest(t *testing.T, idByte byte, fwd CrossPodForwarder) (*Peer, *recordingConn, *store.Store) {
	t.Helper()
	id := newPeerID(idByte)
	conn := &recordingConn{}
	st := store.NewStore()
	notif := store.NewPeerNotifier()
	p := NewPeer(mustMetrics(t), id, conn, st, notif, fwd)
	// Work() normally wires this on first run; tests bypass Work
	// and exercise handlers directly, so set it up here for the
	// subscribe-path tests that depend on it.
	p.peersListener = notif.NewListener(p.sendPeersOnline, p.sendPeersWentOffline)
	return p, conn, st
}

// helper kept local so the test file is self-contained even though
// cluster has its own newPeerID.
func newPeerID(b byte) messages.PeerID {
	var id messages.PeerID
	copy(id[:], "sha-")
	for i := 4; i < len(id); i++ {
		id[i] = b
	}
	return id
}

func TestHandleTransportMsg_LocalPeerStillTakesPriority(t *testing.T) {
	// Even with a forwarder configured, a peer connected to *this*
	// pod must be served from the local store — the cluster
	// fabric is only the fallback path.
	fwd := &fakeForwarder{}
	src, _, st := newPeerForTest(t, 0xAA, fwd)

	dstID := newPeerID(0xBB)
	dstConn := &recordingConn{}
	dst := NewPeer(mustMetrics(t), dstID, dstConn, st, store.NewPeerNotifier(), nil)
	st.AddPeer(dst)

	msg, err := messages.MarshalTransportMsg(dstID, []byte("hello"))
	require.NoError(t, err)

	src.handleTransportMsg(msg)

	require.Equal(t, 0, fwd.calls,
		"forwarder must not be called when destination is local")
	require.NotEmpty(t, dstConn.snapshot(),
		"local destination must receive the transport-msg bytes")
}

func TestHandleTransportMsg_RemotePeerForwardsThroughCluster(t *testing.T) {
	fwd := &fakeForwarder{}
	src, _, _ := newPeerForTest(t, 0xAA, fwd)

	dstID := newPeerID(0xCC) // never added to local store
	original, err := messages.MarshalTransportMsg(dstID, []byte("payload"))
	require.NoError(t, err)
	msg := append([]byte(nil), original...)

	src.handleTransportMsg(msg)

	require.Equal(t, 1, fwd.calls,
		"unknown destination must be forwarded across the cluster")
	require.Equal(t, dstID, fwd.lastDst,
		"forwarder must be told the *original* destination, not the rewritten src")
	require.NotEmpty(t, fwd.lastMsg)

	// The wire format carries a single peer-ID slot which the relay
	// rewrites from "dst" to "src" before forwarding (so the
	// receiving peer learns who sent it). After handleTransportMsg
	// the peerID embedded in the forwarded bytes must be the asker.
	gotPeerID, err := messages.UnmarshalTransportID(fwd.lastMsg)
	require.NoError(t, err)
	require.Equal(t, src.id, *gotPeerID,
		"UpdateTransportMsg must run before cluster handoff so the src is stamped")
}

func TestHandleTransportMsg_ClusterPeerNotFoundDropsQuietly(t *testing.T) {
	fwd := &fakeForwarder{retErr: cluster.ErrPeerNotFound}
	src, _, _ := newPeerForTest(t, 0xAA, fwd)

	dstID := newPeerID(0xDD)
	msg, err := messages.MarshalTransportMsg(dstID, []byte("ghost"))
	require.NoError(t, err)

	src.handleTransportMsg(msg) // must not panic
	require.Equal(t, 1, fwd.calls)
}

func TestHandleTransportMsg_NoForwarderDropsOnMiss(t *testing.T) {
	// Single-pod path: crossPodFwd is nil, so a miss must drop
	// silently — the legacy behavior ADR-0014 promises to keep
	// byte-for-byte for non-clustered deployments.
	src, _, _ := newPeerForTest(t, 0xAA, nil)

	dstID := newPeerID(0xEE)
	msg, err := messages.MarshalTransportMsg(dstID, []byte("nowhere"))
	require.NoError(t, err)

	src.handleTransportMsg(msg) // must not panic
}

func TestHandleSubscribePeerState_CrossPodPeerReturnsAsOnline(t *testing.T) {
	// Multi-pod fabric (ADR-0014): A asks the relay "is peer B
	// online?" When B is on a sibling pod, the local store says no
	// — but the cluster locator can answer yes. Without consulting
	// the fabric the asker silently times out as if B didn't exist
	// anywhere, which is what the user-reported "peers connect but
	// don't talk to each other unless they happen to land on the
	// same pod" symptom looked like in production.
	fwd := &fakeForwarder{locateOK: true, locatePod: "10.0.0.42:7090"}
	src, conn, _ := newPeerForTest(t, 0xAA, fwd)

	dstID := newPeerID(0xBB) // never added to local store
	subMsgs, err := messages.MarshalSubPeerStateMsg([]messages.PeerID{dstID})
	require.NoError(t, err)
	require.NotEmpty(t, subMsgs)

	src.handleSubscribePeerState(subMsgs[0])

	// The asker must have received a PeersOnline notification —
	// MarshalPeersOnline embeds the dst peer ID so the client's
	// stateSubscription unblocks WaitToBeOnlineAndSubscribe.
	require.NotEmpty(t, conn.snapshot(),
		"a cross-pod hit must surface as a PeersOnline notification, not as silence")
}

func TestHandleSubscribePeerState_AbsentEverywhereStaysSilent(t *testing.T) {
	// Negative case: when neither the local store nor the fabric
	// has the peer, we keep the existing "stay silent" semantics so
	// the asker times out exactly like in single-pod deployments.
	fwd := &fakeForwarder{locateOK: false}
	src, conn, _ := newPeerForTest(t, 0xAA, fwd)

	dstID := newPeerID(0xCC)
	subMsgs, err := messages.MarshalSubPeerStateMsg([]messages.PeerID{dstID})
	require.NoError(t, err)
	require.NotEmpty(t, subMsgs)

	src.handleSubscribePeerState(subMsgs[0])

	require.Empty(t, conn.snapshot(),
		"a true miss (local + cluster) must not fabricate an online notification")
}

func TestLocalPeerDispatcher_HasAndDispatch(t *testing.T) {
	st := store.NewStore()
	disp := NewLocalPeerDispatcher(st)

	id := newPeerID(0xAB)
	require.False(t, disp.HasPeer(id),
		"empty store must report HasPeer=false")

	conn := &recordingConn{}
	p := NewPeer(mustMetrics(t), id, conn, st, store.NewPeerNotifier(), nil)
	st.AddPeer(p)

	require.True(t, disp.HasPeer(id))

	msg, err := messages.MarshalTransportMsg(id, []byte("from-cluster"))
	require.NoError(t, err)
	require.NoError(t, disp.DispatchToLocal(id, msg))
	require.Equal(t, msg, conn.snapshot(),
		"DispatchToLocal must hand the bytes to the local peer untouched")
}

func TestLocalPeerDispatcher_GoneBetweenHasAndDispatch(t *testing.T) {
	st := store.NewStore()
	disp := NewLocalPeerDispatcher(st)
	missing := newPeerID(0xCD)
	err := disp.DispatchToLocal(missing, []byte{0})
	require.ErrorIs(t, err, ErrLocalPeerGone)
}
