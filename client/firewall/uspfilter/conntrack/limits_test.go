package conntrack

import (
	"fmt"
	"net/netip"
	"testing"

	"github.com/google/gopacket/layers"
	"github.com/stretchr/testify/require"
)

// Without a cap the tables grow with every distinct 4-tuple a sender can
// forge, and the sender does not need a reply to make that happen. The cap
// bounds memory; eviction prefers entries that are already dead and falls
// back to the least recently seen.

func tableLen[K comparable, V any](m map[K]V, lock interface {
	RLock()
	RUnlock()
}) int {
	lock.RLock()
	defer lock.RUnlock()
	return len(m)
}

func TestTCPTableCapEvictsTombstoneFirst(t *testing.T) {
	t.Setenv(EnvTCPMaxEntries, "4")
	tracker := NewTCPTracker(DefaultTCPTimeout, logger, flowLogger)
	defer tracker.Close()

	srcIP := netip.MustParseAddr("100.64.0.1")
	dstIP := netip.MustParseAddr("100.64.0.2")

	for port := uint16(40000); port < 40004; port++ {
		tracker.TrackOutbound(srcIP, dstIP, port, 443, TCPSyn, 60)
	}
	require.Equal(t, 4, tableLen(tracker.connections, &tracker.mutex))

	// Kill one with a RST so it is tombstoned but still in the table.
	tracker.TrackOutbound(srcIP, dstIP, 40001, 443, TCPRst|TCPAck, 60)
	dead := ConnKey{SrcIP: srcIP, DstIP: dstIP, SrcPort: 40001, DstPort: 443}
	tracker.mutex.RLock()
	require.True(t, tracker.connections[dead].IsTombstone())
	tracker.mutex.RUnlock()

	// One more flow: the table must not grow, and the tombstone must be
	// the one that went, not a live handshake.
	tracker.TrackOutbound(srcIP, dstIP, 40004, 443, TCPSyn, 60)
	require.Equal(t, 4, tableLen(tracker.connections, &tracker.mutex))

	tracker.mutex.RLock()
	_, deadStillThere := tracker.connections[dead]
	_, newOneThere := tracker.connections[ConnKey{SrcIP: srcIP, DstIP: dstIP, SrcPort: 40004, DstPort: 443}]
	tracker.mutex.RUnlock()
	require.False(t, deadStillThere, "the tombstoned entry should be the one evicted")
	require.True(t, newOneThere, "the new flow must have been admitted")
}

func TestTCPTableCapEvictsLeastRecentlySeen(t *testing.T) {
	t.Setenv(EnvTCPMaxEntries, "4")
	tracker := NewTCPTracker(DefaultTCPTimeout, logger, flowLogger)
	defer tracker.Close()

	srcIP := netip.MustParseAddr("100.64.0.1")
	dstIP := netip.MustParseAddr("100.64.0.2")

	for port := uint16(40000); port < 40004; port++ {
		tracker.TrackOutbound(srcIP, dstIP, port, 443, TCPSyn, 60)
	}
	// Touch all but the first, so 40000 is the stalest.
	for port := uint16(40001); port < 40004; port++ {
		tracker.TrackOutbound(srcIP, dstIP, port, 443, TCPAck, 60)
	}

	tracker.TrackOutbound(srcIP, dstIP, 40004, 443, TCPSyn, 60)
	require.Equal(t, 4, tableLen(tracker.connections, &tracker.mutex))

	tracker.mutex.RLock()
	_, stalestThere := tracker.connections[ConnKey{SrcIP: srcIP, DstIP: dstIP, SrcPort: 40000, DstPort: 443}]
	tracker.mutex.RUnlock()
	require.False(t, stalestThere, "with no tombstone, the least recently seen entry goes")
}

func TestTCPTableNeverExceedsCapUnderFlood(t *testing.T) {
	t.Setenv(EnvTCPMaxEntries, "16")
	tracker := NewTCPTracker(DefaultTCPTimeout, logger, flowLogger)
	defer tracker.Close()

	dstIP := netip.MustParseAddr("100.64.0.2")
	for i := 0; i < 2000; i++ {
		srcIP := netip.MustParseAddr(fmt.Sprintf("10.%d.%d.%d", (i>>16)&255, (i>>8)&255, i&255))
		tracker.TrackOutbound(srcIP, dstIP, uint16(10000+i%50000), 443, TCPSyn, 60)
		require.LessOrEqual(t, tableLen(tracker.connections, &tracker.mutex), 16)
	}
}

func TestUDPTableNeverExceedsCap(t *testing.T) {
	t.Setenv(EnvUDPMaxEntries, "8")
	tracker := NewUDPTracker(DefaultUDPTimeout, logger, flowLogger)
	defer tracker.Close()

	srcIP := netip.MustParseAddr("100.64.0.1")
	dstIP := netip.MustParseAddr("100.64.0.2")
	for port := uint16(1); port <= 200; port++ {
		tracker.TrackOutbound(srcIP, dstIP, port, 53, 60)
		require.LessOrEqual(t, tableLen(tracker.connections, &tracker.mutex), 8)
	}
}

func TestICMPTableNeverExceedsCap(t *testing.T) {
	t.Setenv(EnvICMPMaxEntries, "8")
	tracker := NewICMPTracker(DefaultICMPTimeout, logger, flowLogger)
	defer tracker.Close()

	srcIP := netip.MustParseAddr("100.64.0.1")
	dstIP := netip.MustParseAddr("100.64.0.2")
	echo := layers.CreateICMPv4TypeCode(layers.ICMPv4TypeEchoRequest, 0)
	for id := uint16(1); id <= 200; id++ {
		tracker.TrackOutbound(srcIP, dstIP, id, echo, nil, 60)
		require.LessOrEqual(t, tableLen(tracker.connections, &tracker.mutex), 8)
	}
}

func TestTableCapEnvFallsBackOnBadInput(t *testing.T) {
	for _, bad := range []string{"", "abc", "0", "-5"} {
		t.Run(fmt.Sprintf("%q", bad), func(t *testing.T) {
			t.Setenv(EnvTCPMaxEntries, bad)
			tracker := NewTCPTracker(DefaultTCPTimeout, logger, flowLogger)
			defer tracker.Close()
			require.Equal(t, DefaultMaxTCPEntries, tracker.maxEntries)
		})
	}
}
