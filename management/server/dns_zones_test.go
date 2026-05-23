// Issue #108 Phase 1 tests — covers ADR-0022 D5 validation matrix +
// CRUD happy path. Mirrors the bootstrap shape of nameserver_test.go.
// License posture per ADR-0022 D8: AGPL clean-room.
package server

import (
	"context"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	nbdns "github.com/openzro/openzro/dns"
	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/integrations/port_forwarding"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/permissions"
	"github.com/openzro/openzro/management/server/settings"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/telemetry"
	"github.com/openzro/openzro/management/server/types"
)

const (
	dnsZoneTestAccountID = "dns-zones-test-acc"
	dnsZoneTestUserID    = "dns-zones-test-user"
	dnsZoneTestGroupID   = "dns-zones-test-group"
	dnsZoneTestDomain    = "internal.example"
)

func TestDNSZone_CRUDHappyPath(t *testing.T) {
	am, _ := initDNSZoneTestAccount(t)
	ctx := context.Background()

	zone := &types.DNSZone{
		Name:    "Internal services",
		Domain:  dnsZoneTestDomain,
		Enabled: true,
		DistributionGroups: []types.DNSZoneGroup{
			{GroupID: dnsZoneTestGroupID},
		},
	}

	created, err := am.CreateDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, zone)
	require.NoError(t, err)
	require.NotEmpty(t, created.ID, "create must assign an ID")
	require.Equal(t, dnsZoneTestDomain, created.Domain)
	require.Equal(t, []string{dnsZoneTestGroupID}, created.GroupIDs())

	got, err := am.GetDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)

	list, err := am.ListDNSZones(ctx, dnsZoneTestAccountID, dnsZoneTestUserID)
	require.NoError(t, err)
	require.Len(t, list, 1)

	// Add a record.
	rec, err := am.CreateDNSRecord(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, created.ID, &types.DNSRecord{
		Name:    "db." + dnsZoneTestDomain,
		Type:    types.DNSRecordTypeA,
		Content: "10.0.0.5",
	})
	require.NoError(t, err)
	require.Equal(t, types.DNSRecordDefaultTTL, rec.TTL, "ttl defaults to 300 when omitted")

	records, err := am.ListDNSRecords(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, created.ID)
	require.NoError(t, err)
	require.Len(t, records, 1)

	// Delete the record + zone.
	require.NoError(t, am.DeleteDNSRecord(ctx, dnsZoneTestAccountID, created.ID, rec.ID, dnsZoneTestUserID))
	require.NoError(t, am.DeleteDNSZone(ctx, dnsZoneTestAccountID, created.ID, dnsZoneTestUserID))
}

// TestDNSZone_RejectsPeerDNSOverlap_Bidirectional pins ADR-0022 D5.
// The peer DNS domain is "openzro" by default; user zones must not be
// equal, ancestor, or descendant.
func TestDNSZone_RejectsPeerDNSOverlap_Bidirectional(t *testing.T) {
	am, _ := initDNSZoneTestAccount(t)
	ctx := context.Background()

	cases := []struct {
		name   string
		domain string
		reject bool
	}{
		{"same domain", "openzro", true},
		{"descendant of peer zone", "private.openzro", true},
		{"deep descendant", "x.y.openzro", true},
		{"case-insensitive descendant", "DB.OpenZro", true},
		{"sibling at TLD level", "internal.example", false},
		{"unrelated TLD", "io", false}, // openzro is not a descendant of io
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			zone := &types.DNSZone{
				Name:    tc.name,
				Domain:  tc.domain,
				Enabled: true,
				DistributionGroups: []types.DNSZoneGroup{
					{GroupID: dnsZoneTestGroupID},
				},
			}
			_, err := am.CreateDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, zone)
			if tc.reject {
				require.Error(t, err, "domain %q must be rejected", tc.domain)
				require.True(t, strings.Contains(err.Error(), "overlap") ||
					strings.Contains(err.Error(), "InvalidArgument"),
					"error must surface overlap rejection; got %v", err)
			} else {
				require.NoError(t, err, "domain %q must be accepted", tc.domain)
			}
		})
	}
}

// TestDNSZone_DomainImmutableOnPUT pins ADR-0022 D5's
// "domain immutable" rule: a PUT changing the domain is rejected with
// 400, not silently kept-as-old.
func TestDNSZone_DomainImmutableOnPUT(t *testing.T) {
	am, _ := initDNSZoneTestAccount(t)
	ctx := context.Background()

	created, err := am.CreateDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, &types.DNSZone{
		Name:               "Original",
		Domain:             "internal.example",
		Enabled:            true,
		DistributionGroups: []types.DNSZoneGroup{{GroupID: dnsZoneTestGroupID}},
	})
	require.NoError(t, err)

	// Mutate the domain — must error.
	created.Domain = "other.example"
	_, err = am.SaveDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, created)
	require.Error(t, err, "zone domain must be immutable")
}

// TestDNSZone_RequiresAtLeastOneGroup pins D5 distribution-group
// constraint.
func TestDNSZone_RequiresAtLeastOneGroup(t *testing.T) {
	am, _ := initDNSZoneTestAccount(t)
	ctx := context.Background()

	_, err := am.CreateDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, &types.DNSZone{
		Name:               "no groups",
		Domain:             "internal.example",
		Enabled:            true,
		DistributionGroups: []types.DNSZoneGroup{},
	})
	require.Error(t, err, "zone with zero distribution groups must be rejected")
}

// TestDNSZone_RejectsUnknownGroupID pins D5 group existence check.
func TestDNSZone_RejectsUnknownGroupID(t *testing.T) {
	am, _ := initDNSZoneTestAccount(t)
	ctx := context.Background()

	_, err := am.CreateDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, &types.DNSZone{
		Name:    "bogus group",
		Domain:  "internal.example",
		Enabled: true,
		DistributionGroups: []types.DNSZoneGroup{
			{GroupID: "i-do-not-exist"},
		},
	})
	require.Error(t, err, "unknown group id must be rejected")
}

// TestDNSRecord_RejectsNameOutsideZone pins D5 record-within-zone
// constraint.
func TestDNSRecord_RejectsNameOutsideZone(t *testing.T) {
	am, zoneID := initDNSZoneTestAccountWithZone(t)
	ctx := context.Background()

	_, err := am.CreateDNSRecord(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, zoneID, &types.DNSRecord{
		Name:    "db.other.example",
		Type:    types.DNSRecordTypeA,
		Content: "10.0.0.5",
	})
	require.Error(t, err, "record name outside zone domain must be rejected")
}

// TestDNSRecord_CNAMEMutex pins D5 CNAME ↔ A/AAAA mutex.
func TestDNSRecord_CNAMEMutex(t *testing.T) {
	am, zoneID := initDNSZoneTestAccountWithZone(t)
	ctx := context.Background()

	// First, an A record.
	_, err := am.CreateDNSRecord(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, zoneID, &types.DNSRecord{
		Name: "db." + dnsZoneTestDomain, Type: types.DNSRecordTypeA, Content: "10.0.0.5",
	})
	require.NoError(t, err)

	// Now a CNAME for the SAME hostname → must be rejected.
	_, err = am.CreateDNSRecord(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, zoneID, &types.DNSRecord{
		Name: "db." + dnsZoneTestDomain, Type: types.DNSRecordTypeCNAME, Content: "other.example",
	})
	require.Error(t, err, "CNAME on a hostname that has an A must be rejected")
}

// TestDNSRecord_ContentShapePerType pins D5 per-type content shape.
func TestDNSRecord_ContentShapePerType(t *testing.T) {
	am, zoneID := initDNSZoneTestAccountWithZone(t)
	ctx := context.Background()

	cases := []struct {
		name    string
		recType string
		content string
		ok      bool
	}{
		{"A with IPv4", types.DNSRecordTypeA, "10.0.0.5", true},
		{"A with IPv6", types.DNSRecordTypeA, "2001:db8::1", false},
		{"AAAA with IPv6", types.DNSRecordTypeAAAA, "2001:db8::1", true},
		{"AAAA with IPv4", types.DNSRecordTypeAAAA, "10.0.0.5", false},
		{"CNAME with FQDN", types.DNSRecordTypeCNAME, "other.example", true},
		{"CNAME pointing at IPv4", types.DNSRecordTypeCNAME, "10.0.0.5", false},
		{"CNAME with whitespace", types.DNSRecordTypeCNAME, "bad host.example", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := am.CreateDNSRecord(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, zoneID, &types.DNSRecord{
				Name:    "test-" + tc.name + "." + dnsZoneTestDomain,
				Type:    tc.recType,
				Content: tc.content,
			})
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

// TestDNSRecord_TTLDefault pins the default-300s contract for omitted
// TTL.
func TestDNSRecord_TTLDefault(t *testing.T) {
	am, zoneID := initDNSZoneTestAccountWithZone(t)
	ctx := context.Background()

	rec, err := am.CreateDNSRecord(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, zoneID, &types.DNSRecord{
		Name:    "ttl-default." + dnsZoneTestDomain,
		Type:    types.DNSRecordTypeA,
		Content: "10.0.0.1",
		// TTL omitted
	})
	require.NoError(t, err)
	require.Equal(t, types.DNSRecordDefaultTTL, rec.TTL)
}

// TestDNSZone_RejectsOverlapWithAccountSpecificDNSDomain pins Phase 1
// review finding #1: the overlap check must run against the EFFECTIVE
// peer DNS domain (account.Settings.DNSDomain || global default), not
// only the global default. Without this fix a tenant on a custom
// DNSDomain could create a zone that shadows their own peer resolution.
func TestDNSZone_RejectsOverlapWithAccountSpecificDNSDomain(t *testing.T) {
	am, _ := initDNSZoneTestAccount(t)
	ctx := context.Background()

	// Switch the account's DNSDomain to a non-default value.
	account, err := am.Store.GetAccount(ctx, dnsZoneTestAccountID)
	require.NoError(t, err)
	require.NotNil(t, account.Settings)
	account.Settings.DNSDomain = "mesh.acme.internal"
	require.NoError(t, am.Store.SaveAccount(ctx, account))

	// Creating a zone whose domain overlaps the account-specific DNS
	// domain (descendant) must be rejected — even though the GLOBAL
	// default ("openzro" in tests) does NOT overlap.
	_, err = am.CreateDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, &types.DNSZone{
		Name:               "shadows account dns",
		Domain:             "private.mesh.acme.internal",
		Enabled:            true,
		DistributionGroups: []types.DNSZoneGroup{{GroupID: dnsZoneTestGroupID}},
	})
	require.Error(t, err, "zone overlapping account-specific DNSDomain must be rejected")

	// Sanity: a clearly non-overlapping zone still passes.
	_, err = am.CreateDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, &types.DNSZone{
		Name:               "unrelated",
		Domain:             "team.example.test",
		Enabled:            true,
		DistributionGroups: []types.DNSZoneGroup{{GroupID: dnsZoneTestGroupID}},
	})
	require.NoError(t, err)
}

// TestDNSZone_RejectsCrossZoneOverlap pins Phase 1 review finding #2:
// two zones in the same account whose domains have a bidirectional
// suffix overlap (same / ancestor / descendant) must not coexist —
// the agent's local resolver flattens records across zones and would
// otherwise break D1's "NXDOMAIN authoritative for the more-specific
// zone" property.
func TestDNSZone_RejectsCrossZoneOverlap(t *testing.T) {
	am, _ := initDNSZoneTestAccount(t)
	ctx := context.Background()

	first, err := am.CreateDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, &types.DNSZone{
		Name:               "outer",
		Domain:             "example.test",
		Enabled:            true,
		DistributionGroups: []types.DNSZoneGroup{{GroupID: dnsZoneTestGroupID}},
	})
	require.NoError(t, err)

	cases := []struct {
		name   string
		domain string
		reject bool
	}{
		{"identical domain", "example.test", true},
		{"descendant", "sub.example.test", true},
		{"ancestor", "test", true},
		{"unrelated", "other.test", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := am.CreateDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, &types.DNSZone{
				Name:               tc.name,
				Domain:             tc.domain,
				Enabled:            true,
				DistributionGroups: []types.DNSZoneGroup{{GroupID: dnsZoneTestGroupID}},
			})
			if tc.reject {
				require.Error(t, err, "cross-zone overlap with %q must be rejected", first.Domain)
			} else {
				require.NoError(t, err, "non-overlapping %q must be accepted", tc.domain)
			}
		})
	}

	// PUT on the first zone with its OWN domain must NOT trip the
	// cross-zone overlap check (exclusion of self is critical).
	first.Name = "outer-renamed"
	_, err = am.SaveDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, first)
	require.NoError(t, err, "self-domain on PUT must not trigger cross-zone overlap")
}

// TestDNSZone_RejectsDuplicateDistributionGroups pins Phase 1 review
// finding #3: a payload with duplicated group IDs hits the composite
// PK on `dns_zone_groups` as a DB-level duplicate-key 500 unless we
// dedup at the API layer with a 400. This is the API guard.
func TestDNSZone_RejectsDuplicateDistributionGroups(t *testing.T) {
	am, _ := initDNSZoneTestAccount(t)
	ctx := context.Background()

	_, err := am.CreateDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, &types.DNSZone{
		Name:    "dup groups",
		Domain:  "internal.example",
		Enabled: true,
		DistributionGroups: []types.DNSZoneGroup{
			{GroupID: dnsZoneTestGroupID},
			{GroupID: dnsZoneTestGroupID},
		},
	})
	require.Error(t, err, "duplicated distribution group ids must be rejected with 400")
}

// TestDNSRecord_TTLZeroNormalizesAtManagerLayer pins the
// defense-in-depth path at the manager. The wire-level rejection
// of `ttl: 0` is enforced in the HTTP handler (see
// validateRecordTTLAtAPI + TestZonesHandler_RejectsExplicitTTLZero
// below); direct manager callers (tests, future internal callers)
// get the historical "normalize to 300" behavior so they aren't
// surprised by an InvalidArgument from a zero-value struct field.
func TestDNSRecord_TTLZeroNormalizesAtManagerLayer(t *testing.T) {
	am, zoneID := initDNSZoneTestAccountWithZone(t)
	ctx := context.Background()

	rec := &types.DNSRecord{
		Name:    "ttl-default-via-manager." + dnsZoneTestDomain,
		Type:    types.DNSRecordTypeA,
		Content: "10.0.0.7",
		TTL:     0, // zero-value → manager normalizes to 300
	}
	created, err := am.CreateDNSRecord(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, zoneID, rec)
	require.NoError(t, err)
	require.Equal(t, types.DNSRecordDefaultTTL, created.TTL)

	// Negative also normalizes at the manager layer for the same
	// reason — direct callers shouldn't have to know about the
	// internal default.
	created.TTL = -1
	saved, err := am.SaveDNSRecord(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, zoneID, created)
	require.NoError(t, err)
	require.Equal(t, types.DNSRecordDefaultTTL, saved.TTL)
}

// TestStore_AccountRoundtripPreservesDNSZoneChildren pins Phase 1
// review (high) finding: GetAccount → SaveAccount cycle MUST NOT
// wipe DNSZone children (records + distribution groups). Without
// the deep `Preload("DNSZonesG.Records").Preload("DNSZonesG.DistributionGroups")`
// in GetAccount, the generic SaveAccount path
// (Delete-with-Associations + Create-with-FullSaveAssociations)
// would recreate zones with empty Records and DistributionGroups,
// orphaning the children rows.
func TestStore_AccountRoundtripPreservesDNSZoneChildren(t *testing.T) {
	am, _ := initDNSZoneTestAccount(t)
	ctx := context.Background()

	// Create a zone + record via the manager so the children are
	// persisted in the dedicated tables.
	zone, err := am.CreateDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, &types.DNSZone{
		Name:               "round-trip",
		Domain:             "rt.example",
		Enabled:            true,
		DistributionGroups: []types.DNSZoneGroup{{GroupID: dnsZoneTestGroupID}},
	})
	require.NoError(t, err)

	rec, err := am.CreateDNSRecord(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, zone.ID, &types.DNSRecord{
		Name:    "host.rt.example",
		Type:    types.DNSRecordTypeA,
		Content: "10.0.0.42",
	})
	require.NoError(t, err)
	require.NotEmpty(t, rec.ID)

	// Now exercise the GetAccount → mutate (unrelated field) → SaveAccount
	// cycle that gets triggered by various manager paths.
	account, err := am.Store.GetAccount(ctx, dnsZoneTestAccountID)
	require.NoError(t, err)
	require.Contains(t, account.DNSZones, zone.ID, "GetAccount must hydrate the zone map")
	got := account.DNSZones[zone.ID]
	require.Lenf(t, got.Records, 1, "GetAccount must hydrate zone records; got %#v", got)
	require.Lenf(t, got.DistributionGroups, 1, "GetAccount must hydrate distribution groups; got %#v", got)

	// Mutate something unrelated and save.
	account.Domain = "round-trip-mutation.test"
	require.NoError(t, am.Store.SaveAccount(ctx, account))

	// Re-fetch via the dedicated path — children MUST still be there.
	refreshed, err := am.Store.GetDNSZoneByID(ctx, store.LockingStrengthShare, dnsZoneTestAccountID, zone.ID)
	require.NoError(t, err)
	require.Lenf(t, refreshed.Records, 1,
		"SaveAccount round-trip dropped the record children (review finding #1); got %#v", refreshed.Records)
	require.Equal(t, rec.ID, refreshed.Records[0].ID)
	require.Lenf(t, refreshed.DistributionGroups, 1,
		"SaveAccount round-trip dropped the distribution-group links; got %#v", refreshed.DistributionGroups)
	require.Equal(t, dnsZoneTestGroupID, refreshed.DistributionGroups[0].GroupID)
}

// TestNetworkMap_CustomZonesComposition pins Phase 2 per-peer
// composition rules from ADR-0022 D5/D1. Exercised through the
// public `am.GetNetworkMap(peerID)` path (same shape
// dns_test.go uses for the synthetic-only case at
// TestGetNetworkMap_DNSConfigSync). A user-managed zone reaches a
// peer's DNSConfig.CustomZones iff: peer in distribution group AND
// zone.Enabled AND len(zone.Records) > 0. Synthetic peer zone is
// always present alongside.
func TestNetworkMap_CustomZonesComposition(t *testing.T) {
	am, _ := initDNSZoneTestAccount(t)
	ctx := context.Background()

	// Add two peers — one in the test group, one outside.
	peer1, _, _, err := am.AddPeer(ctx, "", dnsZoneTestUserID, &nbpeer.Peer{
		Key: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", Name: "host1@openzro.example",
		Meta: nbpeer.PeerSystemMeta{Hostname: "host1@openzro.example", GoOS: "linux"},
	})
	require.NoError(t, err)
	peer2, _, _, err := am.AddPeer(ctx, "", dnsZoneTestUserID, &nbpeer.Peer{
		Key: "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=", Name: "host2@openzro.example",
		Meta: nbpeer.PeerSystemMeta{Hostname: "host2@openzro.example", GoOS: "linux"},
	})
	require.NoError(t, err)

	// Add peer1 to the test group via the existing group save path —
	// this is the operator's natural flow and exercises the group
	// membership wiring end-to-end.
	g := &types.Group{ID: dnsZoneTestGroupID, Name: "test-group", Peers: []string{peer1.ID}}
	require.NoError(t, am.SaveGroup(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, g, true))

	// Three zones distributed to dnsZoneTestGroupID:
	//   - "ok": enabled with a record -> peer1 must see, peer2 must not
	//   - "empty": no records -> nobody sees
	//   - "disabled": records but disabled -> nobody sees
	okZone, err := am.CreateDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, &types.DNSZone{
		Name: "ok", Domain: "ok.example", Enabled: true,
		DistributionGroups: []types.DNSZoneGroup{{GroupID: dnsZoneTestGroupID}},
	})
	require.NoError(t, err)
	_, err = am.CreateDNSRecord(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, okZone.ID, &types.DNSRecord{
		Name: "h.ok.example", Type: types.DNSRecordTypeA, Content: "10.0.0.5",
	})
	require.NoError(t, err)

	_, err = am.CreateDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, &types.DNSZone{
		Name: "empty", Domain: "empty.example", Enabled: true,
		DistributionGroups: []types.DNSZoneGroup{{GroupID: dnsZoneTestGroupID}},
	})
	require.NoError(t, err)

	disabledZone, err := am.CreateDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, &types.DNSZone{
		Name: "disabled", Domain: "disabled.example", Enabled: true,
		DistributionGroups: []types.DNSZoneGroup{{GroupID: dnsZoneTestGroupID}},
	})
	require.NoError(t, err)
	_, err = am.CreateDNSRecord(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, disabledZone.ID, &types.DNSRecord{
		Name: "h.disabled.example", Type: types.DNSRecordTypeA, Content: "10.0.0.6",
	})
	require.NoError(t, err)
	disabledZone.Enabled = false
	_, err = am.SaveDNSZone(ctx, dnsZoneTestAccountID, dnsZoneTestUserID, disabledZone)
	require.NoError(t, err)

	// peer1 (in group) — synthetic peer zone + ok zone, that's it.
	nm1, err := am.GetNetworkMap(ctx, peer1.ID)
	require.NoError(t, err)
	userZones1 := filterUserZones(nm1.DNSConfig.CustomZones)
	require.Len(t, userZones1, 1, "peer-in-group: only the ok zone; got %+v", userZones1)
	require.Equal(t, "ok.example.", userZones1[0].Domain)
	require.Equal(t, nbdns.CustomZoneSourceUser, userZones1[0].Source)

	// peer2 (out of group) — only the synthetic peer zone.
	nm2, err := am.GetNetworkMap(ctx, peer2.ID)
	require.NoError(t, err)
	require.Empty(t, filterUserZones(nm2.DNSConfig.CustomZones),
		"peer NOT in distribution group sees no user-managed zones")
}

// TestSyntheticPeerZone_SourceAndSearchDomainFlag pins ADR-0022 D4b
// migration of the existing synthetic peer zone — it MUST arrive on
// the wire with Source=PEERS + SearchDomainEnabled=true so the agent
// continues to add the peer DNS domain to the OS search list
// (preserves `dig myhost` working as `myhost.<dnsDomain>`).
func TestSyntheticPeerZone_SourceAndSearchDomainFlag(t *testing.T) {
	am, _ := initDNSZoneTestAccount(t)
	ctx := context.Background()

	peer, _, _, err := am.AddPeer(ctx, "", dnsZoneTestUserID, &nbpeer.Peer{
		Key: "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC=", Name: "host3@openzro.example",
		Meta: nbpeer.PeerSystemMeta{Hostname: "host3@openzro.example", GoOS: "linux"},
	})
	require.NoError(t, err)
	nm, err := am.GetNetworkMap(ctx, peer.ID)
	require.NoError(t, err)
	var peerZone *nbdns.CustomZone
	for i := range nm.DNSConfig.CustomZones {
		if nm.DNSConfig.CustomZones[i].Source == nbdns.CustomZoneSourcePeers {
			peerZone = &nm.DNSConfig.CustomZones[i]
			break
		}
	}
	require.NotNil(t, peerZone, "synthetic peer zone must be present on the wire")
	require.True(t, peerZone.SearchDomainEnabled,
		"synthetic peer zone must keep SearchDomainEnabled=true (legacy compat)")
}

// filterUserZones returns the user-managed zones (Source=USER) from
// a slice that mixes synthetic peer zone + user zones. The composition
// tests don't care about the synthetic zone; this helper keeps the
// assertions focused.
func filterUserZones(zones []nbdns.CustomZone) []nbdns.CustomZone {
	out := make([]nbdns.CustomZone, 0, len(zones))
	for _, z := range zones {
		if z.Source == nbdns.CustomZoneSourceUser {
			out = append(out, z)
		}
	}
	return out
}

// TestDNSZoneOverlap unit-tests the helper directly. Same matrix as
// TestDNSZone_RejectsPeerDNSOverlap_Bidirectional but without the
// account-manager wrapping, useful as a fast-fail regression for the
// validation primitive.
func TestDNSZoneOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"openzro", "openzro", true},
		{"PRIVATE.openzro", "openzro", true},
		{"openzro", "private.openzro", true},
		{"x.y.openzro", "openzro", true},
		{"openzro", "x.y.openzro", true},
		{"internal.example", "openzro", false},
		{"openzro", "internal.example", false},
		{"openzr", "openzro", false}, // substring, not label-aligned
		{"openzro", "openzr", false},
		{"", "openzro", false},
		{"openzro", "", false},
	}
	for _, tc := range cases {
		got := dnsZoneOverlap(tc.a, tc.b)
		require.Equalf(t, tc.want, got, "dnsZoneOverlap(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
	}
}

// -- bootstrap helpers --------------------------------------------------

func initDNSZoneTestAccount(t *testing.T) (*DefaultAccountManager, string) {
	t.Helper()
	am := newDNSZoneTestManager(t)

	account := newAccountWithId(context.Background(), dnsZoneTestAccountID, dnsZoneTestUserID, "openzro.example", false)
	account.Groups[dnsZoneTestGroupID] = &types.Group{
		ID:   dnsZoneTestGroupID,
		Name: "test-group",
	}
	require.NoError(t, am.Store.SaveAccount(context.Background(), account))

	return am, dnsZoneTestAccountID
}

func initDNSZoneTestAccountWithZone(t *testing.T) (*DefaultAccountManager, string) {
	t.Helper()
	am, _ := initDNSZoneTestAccount(t)
	zone, err := am.CreateDNSZone(context.Background(), dnsZoneTestAccountID, dnsZoneTestUserID, &types.DNSZone{
		Name:    "test-zone",
		Domain:  dnsZoneTestDomain,
		Enabled: true,
		DistributionGroups: []types.DNSZoneGroup{
			{GroupID: dnsZoneTestGroupID},
		},
	})
	require.NoError(t, err)
	return am, zone.ID
}

func newDNSZoneTestManager(t *testing.T) *DefaultAccountManager {
	t.Helper()
	dataDir := t.TempDir()
	st, cleanUp, err := store.NewTestStoreFromSQL(context.Background(), "", dataDir)
	require.NoError(t, err)
	t.Cleanup(cleanUp)

	eventStore := &activity.InMemoryEventStore{}
	metrics, err := telemetry.NewDefaultAppMetrics(context.Background())
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	settingsMockManager := settings.NewMockManager(ctrl)
	settingsMockManager.
		EXPECT().
		GetExtraSettings(gomock.Any(), gomock.Any()).
		Return(&types.ExtraSettings{}, nil).
		AnyTimes()

	permissionsManager := permissions.NewManager(st)
	am, err := BuildManager(context.Background(), st, NewPeersUpdateManager(nil), nil, "", "openzro", eventStore, nil, false, MockIntegratedValidator{}, metrics, port_forwarding.NewControllerMock(), settingsMockManager, permissionsManager, false)
	require.NoError(t, err)
	return am
}
