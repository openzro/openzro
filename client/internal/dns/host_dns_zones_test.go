// Phase 2 (issue #108) — host config tests for the new
// SearchDomainEnabled flag. Pins ADR-0022 D6: a custom zone with
// SearchDomainEnabled=false must be added with MatchOnly=true so the
// OS resolver routes queries to us WITHOUT appending the zone to
// its bare-name search list. Reverse zones always stay MatchOnly
// regardless of the flag.
//
// License posture: BSD-3 (client). Reconstructed from public
// docs + in-tree CustomZones pipeline; no upstream AGPL diff
// consulted.
package dns

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	nbdns "github.com/openzro/openzro/dns"
)

func TestDNSConfigToHostDNSConfig_RespectsSearchDomainEnabled(t *testing.T) {
	ip := netip.MustParseAddr("100.64.0.254")
	cfg := nbdns.Config{
		ServiceEnable: true,
		CustomZones: []nbdns.CustomZone{
			{
				Domain:              "search.example",
				SearchDomainEnabled: true,
				Source:              nbdns.CustomZoneSourceUser,
			},
			{
				Domain:              "match-only.example",
				SearchDomainEnabled: false,
				Source:              nbdns.CustomZoneSourceUser,
			},
			{
				Domain:              "10.in-addr.arpa.",
				SearchDomainEnabled: true, // even when true, reverse stays MatchOnly
				Source:              nbdns.CustomZoneSourceUser,
			},
		},
	}
	host := dnsConfigToHostDNSConfig(cfg, ip, 53)

	got := map[string]bool{} // domain → MatchOnly
	for _, d := range host.Domains {
		got[d.Domain] = d.MatchOnly
	}
	require.Equalf(t, false, got["search.example."],
		"SearchDomainEnabled=true → MatchOnly false; got %+v", host.Domains)
	require.Equalf(t, true, got["match-only.example."],
		"SearchDomainEnabled=false → MatchOnly true; got %+v", host.Domains)
	require.Equalf(t, true, got["10.in-addr.arpa."],
		"reverse zone is MatchOnly regardless of SearchDomainEnabled; got %+v", host.Domains)
}

func TestDNSConfigToHostDNSConfig_LegacyPeerZoneStillInSearchList(t *testing.T) {
	// Sanity check on the backward-compat path: engine.toDNSConfig
	// already normalizes a legacy synthetic peer zone (Source=
	// UNSPECIFIED) to SearchDomainEnabled=true. This test mirrors
	// that — confirms host.go does the right thing once the
	// normalization has happened.
	ip := netip.MustParseAddr("100.64.0.254")
	cfg := nbdns.Config{
		ServiceEnable: true,
		CustomZones: []nbdns.CustomZone{
			{
				Domain:              "openzro",
				SearchDomainEnabled: true, // post-normalization
				Source:              nbdns.CustomZoneSourceUnspecified,
			},
		},
	}
	host := dnsConfigToHostDNSConfig(cfg, ip, 53)
	require.Len(t, host.Domains, 1)
	require.False(t, host.Domains[0].MatchOnly,
		"legacy synthetic peer zone must stay in the search list")
}
