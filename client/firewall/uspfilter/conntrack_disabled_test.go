package uspfilter

import (
	"net"
	"net/netip"
	"testing"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/stretchr/testify/require"

	fw "github.com/openzro/openzro/client/firewall/manager"
	"github.com/openzro/openzro/client/iface/device"
	"github.com/openzro/openzro/client/iface/wgaddr"
)

// With OZ_DISABLE_CONNTRACK set the trackers are never created, so every
// tracking call has to be gated on m.stateful. Missing that gate means the
// first packet of the given kind dereferences a nil tracker and takes the
// daemon down.

var (
	conntrackTestLocalIP  = netip.MustParseAddr("100.10.0.100")
	conntrackTestRemoteIP = netip.MustParseAddr("100.10.0.1")
)

func newDisabledConntrackManager(t *testing.T) *Manager {
	t.Helper()

	t.Setenv(EnvDisableConntrack, "true")

	manager, err := Create(&IFaceMock{
		SetFilterFunc: func(device.PacketFilter) error { return nil },
		AddressFunc: func() wgaddr.Address {
			return wgaddr.Address{
				IP:      conntrackTestLocalIP,
				Network: netip.MustParsePrefix("100.10.0.0/16"),
			}
		},
	}, false, flowLogger)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, manager.Close(nil))
	})

	require.False(t, manager.stateful, "conntrack should be disabled")
	require.Nil(t, manager.udpTracker, "UDP tracker should not be created")
	require.Nil(t, manager.tcpTracker, "TCP tracker should not be created")
	require.Nil(t, manager.icmpTracker, "ICMP tracker should not be created")

	return manager
}

// buildPacket serializes an IPv4 packet of the given transport protocol from
// src to dst.
func buildPacket(t *testing.T, src, dst netip.Addr, proto layers.IPProtocol) []byte {
	t.Helper()

	ip := &layers.IPv4{
		TTL:      64,
		Version:  4,
		SrcIP:    src.AsSlice(),
		DstIP:    dst.AsSlice(),
		Protocol: proto,
	}

	var transport gopacket.SerializableLayer
	switch proto {
	case layers.IPProtocolUDP:
		udp := &layers.UDP{SrcPort: 51334, DstPort: 53}
		require.NoError(t, udp.SetNetworkLayerForChecksum(ip))
		transport = udp
	case layers.IPProtocolTCP:
		tcp := &layers.TCP{SrcPort: 51334, DstPort: 443, SYN: true}
		require.NoError(t, tcp.SetNetworkLayerForChecksum(ip))
		transport = tcp
	case layers.IPProtocolICMPv4:
		transport = &layers.ICMPv4{
			TypeCode: layers.CreateICMPv4TypeCode(layers.ICMPv4TypeEchoRequest, 0),
			Id:       1,
			Seq:      1,
		}
	default:
		t.Fatalf("unsupported protocol %v", proto)
	}

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{ComputeChecksums: true, FixLengths: true}
	require.NoError(t, gopacket.SerializeLayers(buf, opts, ip, transport, gopacket.Payload("test")))

	return buf.Bytes()
}

func TestConntrackDisabledOutboundDoesNotPanic(t *testing.T) {
	manager := newDisabledConntrackManager(t)

	for name, proto := range map[string]layers.IPProtocol{
		"UDP":  layers.IPProtocolUDP,
		"TCP":  layers.IPProtocolTCP,
		"ICMP": layers.IPProtocolICMPv4,
	} {
		t.Run(name, func(t *testing.T) {
			packet := buildPacket(t, conntrackTestLocalIP, conntrackTestRemoteIP, proto)

			require.NotPanics(t, func() {
				drop := manager.FilterOutbound(packet, 0)
				require.False(t, drop, "outbound packet should not be dropped")
			})
		})
	}
}

func TestConntrackDisabledInboundDoesNotPanic(t *testing.T) {
	manager := newDisabledConntrackManager(t)

	// An accept rule so the packet passes the peer ACLs and reaches the
	// tracking call at the end of handleLocalTraffic.
	_, err := manager.AddPeerFiltering(
		nil,
		net.ParseIP("0.0.0.0"),
		fw.ProtocolALL,
		nil,
		nil,
		fw.ActionAccept,
		"",
	)
	require.NoError(t, err)

	for name, proto := range map[string]layers.IPProtocol{
		"UDP":  layers.IPProtocolUDP,
		"TCP":  layers.IPProtocolTCP,
		"ICMP": layers.IPProtocolICMPv4,
	} {
		t.Run(name, func(t *testing.T) {
			packet := buildPacket(t, conntrackTestRemoteIP, conntrackTestLocalIP, proto)

			require.NotPanics(t, func() {
				drop := manager.FilterInbound(packet, 0)
				require.False(t, drop, "allowed inbound packet should not be dropped")
			})
		})
	}
}
