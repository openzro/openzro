// Phase 2 (issue #108) — wire-level conversion tests for the proto
// → nbdns.Config path. Pins ADR-0022 D4b + D6: the agent must
// surface SearchDomainEnabled + Source from the wire, and treat
// Source=UNSPECIFIED as legacy (SearchDomainEnabled forced true so a
// pre-Phase-2 management daemon keeps peer-name resolution working).
//
// License posture: BSD-3 (client). Reconstructed from public NetBird
// docs + the in-tree CustomZones pipeline; no upstream AGPL diff
// consulted.
package internal

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	nbdns "github.com/openzro/openzro/dns"
	mgmProto "github.com/openzro/openzro/management/proto"
)

func TestToDNSConfig_MapsSearchDomainAndSource(t *testing.T) {
	net := netip.MustParsePrefix("100.64.0.0/10")
	proto := &mgmProto.DNSConfig{
		ServiceEnable: true,
		CustomZones: []*mgmProto.CustomZone{
			{
				Domain:              "ok.example",
				SearchDomainEnabled: true,
				Source:              mgmProto.CustomZoneSource_CUSTOM_ZONE_SOURCE_USER,
			},
			{
				Domain:              "match-only.example",
				SearchDomainEnabled: false,
				Source:              mgmProto.CustomZoneSource_CUSTOM_ZONE_SOURCE_USER,
			},
		},
	}
	got := toDNSConfig(proto, net)
	// toDNSConfig auto-appends a reverse-DNS zone for the network
	// prefix on top of the proto zones; assert by domain lookup
	// rather than slice length.
	byDomain := map[string]nbdns.CustomZone{}
	for _, z := range got.CustomZones {
		byDomain[z.Domain] = z
	}
	ok, has := byDomain["ok.example"]
	require.True(t, has, "ok.example must round-trip from proto: %+v", got.CustomZones)
	require.True(t, ok.SearchDomainEnabled)
	require.Equal(t, nbdns.CustomZoneSourceUser, ok.Source)

	mo, has := byDomain["match-only.example"]
	require.True(t, has, "match-only.example must round-trip from proto: %+v", got.CustomZones)
	require.False(t, mo.SearchDomainEnabled)
	require.Equal(t, nbdns.CustomZoneSourceUser, mo.Source)
}

func TestToDNSConfig_LegacyManagementForcesSearchDomain(t *testing.T) {
	// A pre-Phase-2 management daemon doesn't populate the new fields
	// → Source=UNSPECIFIED, SearchDomainEnabled=false on the wire.
	// New agents MUST treat that as "legacy synthetic peer zone,
	// search-domain enabled" or peers upgraded ahead of management
	// lose `dig myhost` (regression).
	net := netip.MustParsePrefix("100.64.0.0/10")
	proto := &mgmProto.DNSConfig{
		ServiceEnable: true,
		CustomZones: []*mgmProto.CustomZone{
			{
				Domain: "openzro", // synthetic peer zone, old format
				// SearchDomainEnabled, Source both default-zero on the wire
			},
		},
	}
	got := toDNSConfig(proto, net)
	// Find the legacy peer zone in the result (toDNSConfig also
	// auto-appends a reverse-DNS zone for the network prefix, which
	// is fine for this assertion — we only care that the legacy zone
	// got the normalization).
	var legacyZone *nbdns.CustomZone
	for i := range got.CustomZones {
		if got.CustomZones[i].Domain == "openzro" {
			legacyZone = &got.CustomZones[i]
			break
		}
	}
	require.NotNil(t, legacyZone, "legacy zone must be preserved on the wire: %+v", got.CustomZones)
	require.True(t, legacyZone.SearchDomainEnabled,
		"UNSPECIFIED source must be normalized to SearchDomainEnabled=true (legacy compat)")
	require.Equal(t, nbdns.CustomZoneSourceUnspecified, legacyZone.Source)
}
