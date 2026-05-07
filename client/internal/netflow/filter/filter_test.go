package filter

import (
	"testing"

	nftypes "github.com/openzro/openzro/client/internal/netflow/types"
)

// TestFilter_DefaultExcludesDiscoveryPorts is the headline guarantee:
// out of the box, an enterprise VPN drops the noisy discovery
// protocols without any operator config. Each entry in DefaultExcluded
// must be excluded; a representative real port (HTTPS, DNS) must NOT be
// excluded. This test is the canary that flags any future PR shrinking
// the default skip list to the operator.
func TestFilter_DefaultExcludesDiscoveryPorts(t *testing.T) {
	f := New(false, nil)

	cases := []struct {
		name     string
		proto    nftypes.Protocol
		port     uint16
		excluded bool
	}{
		{"SSDP UDP/1900 — UPnP discovery", nftypes.UDP, 1900, true},
		{"mDNS UDP/5353 — Bonjour/Avahi", nftypes.UDP, 5353, true},
		{"NetBIOS Name UDP/137", nftypes.UDP, 137, true},
		{"NetBIOS Datagram UDP/138", nftypes.UDP, 138, true},
		{"LLMNR UDP/5355", nftypes.UDP, 5355, true},

		// Same ports on TCP must NOT be excluded — the default list
		// targets UDP discovery only. A TCP/137 connection is rare
		// but legitimate (some legacy SMB tooling) and carries audit
		// value when it shows up.
		{"TCP/137 (rare but real)", nftypes.TCP, 137, false},
		{"TCP/1900 (rare but real)", nftypes.TCP, 1900, false},

		// Real protocols an operator typically DOES want to track.
		{"HTTPS TCP/443", nftypes.TCP, 443, false},
		{"DNS UDP/53", nftypes.UDP, 53, false},
		{"NTP UDP/123", nftypes.UDP, 123, false},
		{"SSH TCP/22", nftypes.TCP, 22, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := f.Excludes(c.proto, c.port); got != c.excluded {
				t.Errorf("Excludes(%v, %d) = %v, want %v", c.proto, c.port, got, c.excluded)
			}
		})
	}
}

// TestFilter_DisableDefault drops the built-in skip list entirely.
// Operator wants to capture discovery traffic for whatever reason
// (incident response into a compromised IoT segment, threat hunt, etc).
func TestFilter_DisableDefault(t *testing.T) {
	f := New(true, nil)
	if f.Excludes(nftypes.UDP, 1900) {
		t.Error("disableDefault=true must let SSDP/UDP through")
	}
	if f.Excludes(nftypes.UDP, 5353) {
		t.Error("disableDefault=true must let mDNS through")
	}
}

// TestFilter_ExtraIsAdditive — operator adds custom internal-protocol
// ports on top of the defaults. Both built-in and custom matches drop.
func TestFilter_ExtraIsAdditive(t *testing.T) {
	extra := []nftypes.FlowPortFilter{
		{Port: 8500, Protocol: "tcp"}, // imaginary internal heartbeat
		{Port: 7777, Protocol: "udp"},
	}
	f := New(false, extra)

	// Built-in still applies.
	if !f.Excludes(nftypes.UDP, 1900) {
		t.Error("default SSDP filter dropped when extra was supplied")
	}
	// Custom applies.
	if !f.Excludes(nftypes.TCP, 8500) {
		t.Error("operator custom TCP/8500 not excluded")
	}
	if !f.Excludes(nftypes.UDP, 7777) {
		t.Error("operator custom UDP/7777 not excluded")
	}
	// Wrong protocol on a custom rule must NOT match.
	if f.Excludes(nftypes.UDP, 8500) {
		t.Error("custom TCP/8500 leaked to UDP/8500")
	}
}

// TestFilter_AnyProtocol — protocol "any" (or empty) matches both
// TCP and UDP for the same port. Useful for ports that genuinely
// don't care about L4 (some custom heartbeats use both).
func TestFilter_AnyProtocol(t *testing.T) {
	for _, proto := range []string{"any", "ANY", "", "  "} {
		t.Run(proto, func(t *testing.T) {
			f := New(true, []nftypes.FlowPortFilter{
				{Port: 9999, Protocol: proto},
			})
			if !f.Excludes(nftypes.TCP, 9999) {
				t.Errorf("protocol=%q should match TCP", proto)
			}
			if !f.Excludes(nftypes.UDP, 9999) {
				t.Errorf("protocol=%q should match UDP", proto)
			}
		})
	}
}

// TestFilter_NilSafe — a Logger before its first UpdateConfig has
// portFilter=nil and the hot path must short-circuit cleanly.
func TestFilter_NilSafe(t *testing.T) {
	var f *Filter
	if f.Excludes(nftypes.UDP, 1900) {
		t.Error("nil filter must report no exclusions")
	}
}

// TestFilter_PortZeroNeverMatches — port 0 isn't a real conntrack
// destination, and a misconfigured rule with port=0 must not silently
// match every event.
func TestFilter_PortZeroNeverMatches(t *testing.T) {
	f := New(true, []nftypes.FlowPortFilter{
		{Port: 0, Protocol: "udp"},
	})
	if f.Excludes(nftypes.UDP, 0) {
		t.Error("port 0 events are filtered separately upstream; the filter must ignore port=0 entries")
	}
	if f.Excludes(nftypes.UDP, 1900) {
		t.Error("disableDefault=true with no valid extra entries should let everything through")
	}
}
