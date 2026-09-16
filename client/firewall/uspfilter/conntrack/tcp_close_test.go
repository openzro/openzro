package conntrack

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

// The teardown states must only advance on the segment from the side that
// the state is waiting on. A retransmit from the other side is valid
// traffic -- it passes the firewall -- but it must not move the machine,
// or a duplicate FIN closes a flow the peer has not finished with.

type closeHarness struct {
	t       *testing.T
	tracker *TCPTracker
	src     netip.Addr
	dst     netip.Addr
	sport   uint16
	dport   uint16
	key     ConnKey
}

func newCloseHarness(t *testing.T) *closeHarness {
	t.Helper()
	h := &closeHarness{
		t:       t,
		tracker: NewTCPTracker(DefaultTCPTimeout, logger, flowLogger),
		src:     netip.MustParseAddr("100.64.0.1"),
		dst:     netip.MustParseAddr("100.64.0.2"),
		sport:   40000,
		dport:   443,
	}
	h.key = ConnKey{SrcIP: h.src, DstIP: h.dst, SrcPort: h.sport, DstPort: h.dport}
	t.Cleanup(h.tracker.Close)
	establishConnection(t, h.tracker, h.src, h.dst, h.sport, h.dport)
	return h
}

// ours sends a segment from the initiating side (same direction as the SYN).
func (h *closeHarness) ours(flags uint8) {
	h.tracker.TrackOutbound(h.src, h.dst, h.sport, h.dport, flags, 60)
}

// peers sends a segment from the other side and asserts the firewall let it through.
func (h *closeHarness) peers(flags uint8) {
	h.t.Helper()
	require.True(h.t, h.tracker.IsValidInbound(h.dst, h.src, h.dport, h.sport, flags, 60),
		"peer segment %x should pass", flags)
}

func (h *closeHarness) state() TCPState {
	h.tracker.mutex.RLock()
	defer h.tracker.mutex.RUnlock()
	return h.tracker.connections[h.key].GetState()
}

func TestTCPCloseWaitIgnoresPeerFinRetransmit(t *testing.T) {
	h := newCloseHarness(t)
	h.peers(TCPFin | TCPAck)
	require.Equal(t, TCPStateCloseWait, h.state())

	h.peers(TCPFin | TCPAck) // retransmit
	require.Equal(t, TCPStateCloseWait, h.state(), "peer's FIN retransmit must not advance CloseWait")

	h.ours(TCPFin | TCPAck)
	require.Equal(t, TCPStateLastAck, h.state(), "only our FIN advances CloseWait")
}

func TestTCPLastAckIgnoresOwnAck(t *testing.T) {
	h := newCloseHarness(t)
	h.peers(TCPFin | TCPAck)
	h.ours(TCPFin | TCPAck)
	require.Equal(t, TCPStateLastAck, h.state())

	h.ours(TCPAck) // our own ACK retransmit
	require.Equal(t, TCPStateLastAck, h.state(), "our own ACK must not close LastAck")

	h.peers(TCPAck)
	require.Equal(t, TCPStateClosed, h.state(), "the peer's ACK closes LastAck")
}

func TestTCPFinWait2IgnoresOwnFin(t *testing.T) {
	h := newCloseHarness(t)
	h.ours(TCPFin | TCPAck)
	h.peers(TCPAck)
	require.Equal(t, TCPStateFinWait2, h.state())

	h.ours(TCPFin | TCPAck) // duplicate of our own FIN
	require.Equal(t, TCPStateFinWait2, h.state(), "our own FIN must not advance FinWait2")

	h.peers(TCPFin | TCPAck)
	require.Equal(t, TCPStateTimeWait, h.state(), "the peer's FIN completes FinWait2")
}

func TestTCPClosingNeedsPeerAck(t *testing.T) {
	h := newCloseHarness(t)
	h.ours(TCPFin | TCPAck)
	h.peers(TCPFin) // simultaneous close: lone FIN, not acking ours
	require.Equal(t, TCPStateClosing, h.state())

	h.ours(TCPAck) // we ack their FIN
	require.Equal(t, TCPStateClosing, h.state(), "our own ACK must not complete Closing")

	h.peers(TCPAck) // they ack our FIN
	require.Equal(t, TCPStateTimeWait, h.state(), "the peer's ACK completes Closing")
}

func TestTCPFinWait1PeerFinAckGoesStraightToTimeWait(t *testing.T) {
	h := newCloseHarness(t)
	h.ours(TCPFin | TCPAck)
	require.Equal(t, TCPStateFinWait1, h.state())

	// The peer acknowledges our FIN and sends its own in one segment --
	// the common case. RFC 9293 3.10.7.4: our FIN is acked, enter TIME-WAIT.
	h.peers(TCPFin | TCPAck)
	require.Equal(t, TCPStateTimeWait, h.state(), "FIN+ACK in FinWait1 goes straight to TimeWait")
}
