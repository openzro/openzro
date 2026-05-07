//go:build linux && !android

package conntrack

import (
	"net/netip"
	"testing"

	nbnet "github.com/openzro/openzro/util/net"
)

// TestRelevantFlow_OnlyMeshMarkedFlows is the regression test for
// ADR-0015. The pre-fix implementation also accepted any flow whose
// src or dst happened to land in the WireGuard subnet, which
// captured kernel-side bind()-to-wgaddr connections to the public
// internet as well as stale conntrack from before the agent
// restarted. These flows have no rule_id and surface in the
// dashboard as "external" rows with no policy attribution — useless
// signal, frequent confusion, never legitimate audit material.
//
// Post-fix: only flows whose mark is in the data-plane range
// (set by acl_linux.go and router_linux.go via the policymark
// indexer) are forwarded to the flow logger.
func TestRelevantFlow_OnlyMeshMarkedFlows(t *testing.T) {
	ct := &ConnTrack{} // iface is unused now; the test exercises the mark path only

	wgaddr := netip.MustParseAddr("100.85.101.253") // a WG-network IP
	publicIP := netip.MustParseAddr("95.216.195.133")

	// Construct a mark that carries a rule_index but is in the data
	// plane range — this is what acl/router nftables rules stamp.
	dpMarkWithIndex := nbnet.DataPlaneMarkOut | (uint32(7) << nbnet.RuleIndexShift)

	cases := []struct {
		name string
		mark uint32
		src  netip.Addr
		dst  netip.Addr
		want bool
	}{
		{
			name: "data plane mark — peer to peer",
			mark: nbnet.DataPlaneMarkOut,
			src:  wgaddr,
			dst:  netip.MustParseAddr("100.85.101.42"),
			want: true,
		},
		{
			name: "data plane mark with rule_index — Network Resource flow",
			mark: dpMarkWithIndex,
			src:  wgaddr,
			dst:  publicIP, // routing peer NATs out
			want: true,
		},
		{
			name: "no mark, src in WG net — pre-ADR-0015 false positive",
			mark: 0,
			src:  wgaddr,
			dst:  publicIP,
			want: false,
		},
		{
			name: "no mark, dst in WG net — pre-ADR-0015 false positive",
			mark: 0,
			src:  publicIP,
			dst:  wgaddr,
			want: false,
		},
		{
			name: "no mark, neither side in WG net — was already rejected",
			mark: 0,
			src:  publicIP,
			dst:  netip.MustParseAddr("8.8.8.8"),
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ct.relevantFlow(c.mark, c.src, c.dst); got != c.want {
				t.Errorf("relevantFlow(mark=%#x) = %v, want %v", c.mark, got, c.want)
			}
		})
	}
}
