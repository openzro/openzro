package conntrack

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

// An ephemeral source port that is reused while the previous incarnation of
// the same 4-tuple still sits in the table must start a fresh flow. Otherwise
// track() adopts the stale entry, no new entry is created, and the inbound
// SYN-ACK is handed to the peer ACLs and silently dropped.

func TestTCPPortReuseAfterPeerClose(t *testing.T) {
	tracker := NewTCPTracker(DefaultTCPTimeout, logger, flowLogger)
	defer tracker.Close()

	srcIP := netip.MustParseAddr("100.64.0.1")
	dstIP := netip.MustParseAddr("100.64.0.2")
	srcPort := uint16(56486)
	dstPort := uint16(8443)

	key := ConnKey{SrcIP: srcIP, DstIP: dstIP, SrcPort: srcPort, DstPort: dstPort}

	establishConnection(t, tracker, srcIP, dstIP, srcPort, dstPort)

	// Peer closes first: Established -> CloseWait -> LastAck -> Closed, which
	// tombstones the entry. It stays in the table until the next cleanup tick.
	require.True(t, tracker.IsValidInbound(dstIP, srcIP, dstPort, srcPort, TCPFin|TCPAck, 100),
		"peer FIN should be allowed")
	tracker.TrackOutbound(srcIP, dstIP, srcPort, dstPort, TCPFin|TCPAck, 100)
	require.True(t, tracker.IsValidInbound(dstIP, srcIP, dstPort, srcPort, TCPAck, 100),
		"final ACK should be allowed")

	tracker.mutex.RLock()
	closed := tracker.connections[key]
	tracker.mutex.RUnlock()
	require.NotNil(t, closed, "closed entry should still be in the table")
	require.True(t, closed.IsTombstone(), "peer-initiated close should tombstone the entry")

	// Same 4-tuple again, before cleanup ran.
	tracker.TrackOutbound(srcIP, dstIP, srcPort, dstPort, TCPSyn, 100)

	tracker.mutex.RLock()
	reused := tracker.connections[key]
	tracker.mutex.RUnlock()
	require.NotNil(t, reused, "reused port should be tracked")
	require.False(t, reused.IsTombstone(), "reused port should not inherit the tombstone")
	require.NotEqual(t, closed.FlowId, reused.FlowId, "reused port should start a new flow")

	require.True(t, tracker.IsValidInbound(dstIP, srcIP, dstPort, srcPort, TCPSyn|TCPAck, 100),
		"SYN-ACK for the reused port must be accepted, not dropped to the ACLs")
	require.Equal(t, TCPStateEstablished, reused.GetState(), "handshake should complete")
}

func TestTCPPortReuseDuringTimeWait(t *testing.T) {
	tracker := NewTCPTracker(DefaultTCPTimeout, logger, flowLogger)
	defer tracker.Close()

	srcIP := netip.MustParseAddr("100.64.0.1")
	dstIP := netip.MustParseAddr("100.64.0.2")
	srcPort := uint16(56486)
	dstPort := uint16(8443)

	key := ConnKey{SrcIP: srcIP, DstIP: dstIP, SrcPort: srcPort, DstPort: dstPort}

	establishConnection(t, tracker, srcIP, dstIP, srcPort, dstPort)

	// We close first: Established -> FinWait1 -> FinWait2 -> TimeWait.
	tracker.TrackOutbound(srcIP, dstIP, srcPort, dstPort, TCPFin|TCPAck, 100)
	require.True(t, tracker.IsValidInbound(dstIP, srcIP, dstPort, srcPort, TCPAck, 100),
		"ACK of our FIN should be allowed")
	require.True(t, tracker.IsValidInbound(dstIP, srcIP, dstPort, srcPort, TCPFin|TCPAck, 100),
		"peer FIN should be allowed")
	tracker.TrackOutbound(srcIP, dstIP, srcPort, dstPort, TCPAck, 100)

	tracker.mutex.RLock()
	waiting := tracker.connections[key]
	tracker.mutex.RUnlock()
	require.NotNil(t, waiting, "entry should still be in the table")
	require.Equal(t, TCPStateTimeWait, waiting.GetState(), "local close should end in Time Wait")

	// Same 4-tuple again, well within the Time Wait window.
	tracker.TrackOutbound(srcIP, dstIP, srcPort, dstPort, TCPSyn, 100)

	tracker.mutex.RLock()
	reused := tracker.connections[key]
	tracker.mutex.RUnlock()
	require.NotNil(t, reused, "reused port should be tracked")
	require.NotEqual(t, waiting.FlowId, reused.FlowId, "reused port should start a new flow")
	require.Equal(t, TCPStateSynSent, reused.GetState(),
		"reused port must start a fresh handshake instead of inheriting Time Wait")

	require.True(t, tracker.IsValidInbound(dstIP, srcIP, dstPort, srcPort, TCPSyn|TCPAck, 100),
		"SYN-ACK for the reused port must be accepted")
	require.Equal(t, TCPStateEstablished, reused.GetState(), "handshake should complete")
}
