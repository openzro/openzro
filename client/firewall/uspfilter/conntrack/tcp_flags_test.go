package conntrack

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Segments carrying an illegal flag combination (SYN+FIN, RST+SYN, RST+FIN)
// never belong to a real flow. They must not create a conntrack entry, must
// not be admitted against an existing one, and must not refresh lastSeen --
// otherwise a stream of forged segments keeps a dead flow alive past its
// timeout, and the "allow everything on Established" fallback lets them
// straight through the firewall.

func TestTCPIllegalSynComboCreatesNoEntry(t *testing.T) {
	tracker := NewTCPTracker(DefaultTCPTimeout, logger, flowLogger)
	defer tracker.Close()

	srcIP := netip.MustParseAddr("100.64.0.1")
	dstIP := netip.MustParseAddr("100.64.0.2")

	for _, tc := range []struct {
		name  string
		flags uint8
	}{
		{"SYN+FIN", TCPSyn | TCPFin},
		{"SYN+RST", TCPSyn | TCPRst},
		{"SYN+FIN+RST", TCPSyn | TCPFin | TCPRst},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tracker.TrackOutbound(srcIP, dstIP, 40000, 443, tc.flags, 60)

			tracker.mutex.RLock()
			n := len(tracker.connections)
			tracker.mutex.RUnlock()
			require.Equal(t, 0, n, "%s must not create a conntrack entry", tc.name)
		})
	}
}

func TestTCPIllegalFlagsRejectedOnEstablished(t *testing.T) {
	tracker := NewTCPTracker(DefaultTCPTimeout, logger, flowLogger)
	defer tracker.Close()

	srcIP := netip.MustParseAddr("100.64.0.1")
	dstIP := netip.MustParseAddr("100.64.0.2")
	establishConnection(t, tracker, srcIP, dstIP, 40000, 443)

	for _, tc := range []struct {
		name  string
		flags uint8
	}{
		{"SYN+FIN", TCPSyn | TCPFin},
		{"RST+FIN", TCPRst | TCPFin},
		{"RST+SYN", TCPRst | TCPSyn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok := tracker.IsValidInbound(dstIP, srcIP, 443, 40000, tc.flags, 60)
			require.False(t, ok, "%s must not pass on an established connection", tc.name)
		})
	}

	// The connection itself is untouched: a legitimate ACK still passes.
	require.True(t, tracker.IsValidInbound(dstIP, srcIP, 443, 40000, TCPAck, 60))
}

func TestTCPIllegalFlagsDoNotRefreshLastSeen(t *testing.T) {
	tracker := NewTCPTracker(DefaultTCPTimeout, logger, flowLogger)
	defer tracker.Close()

	srcIP := netip.MustParseAddr("100.64.0.1")
	dstIP := netip.MustParseAddr("100.64.0.2")
	establishConnection(t, tracker, srcIP, dstIP, 40000, 443)

	key := ConnKey{SrcIP: srcIP, DstIP: dstIP, SrcPort: 40000, DstPort: 443}
	tracker.mutex.RLock()
	conn := tracker.connections[key]
	tracker.mutex.RUnlock()
	require.NotNil(t, conn)

	before := conn.GetLastSeen()
	time.Sleep(5 * time.Millisecond)

	// Both directions go through updateState; neither may touch lastSeen.
	tracker.TrackOutbound(srcIP, dstIP, 40000, 443, TCPSyn|TCPFin, 60)
	tracker.IsValidInbound(dstIP, srcIP, 443, 40000, TCPRst|TCPFin, 60)

	require.Equal(t, before, conn.GetLastSeen(),
		"illegal segments must not refresh lastSeen")
	require.Equal(t, TCPStateEstablished, conn.GetState(),
		"illegal segments must not drive state")
}
