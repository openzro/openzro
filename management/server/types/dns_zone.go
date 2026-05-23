// Package types — Custom DNS Zone GORM models. Issue #108, Phase 1
// of ADR-0022. License posture per ADR D8: AGPL clean-room,
// reconstructed from docs.netbird.io/manage/dns/custom-zones +
// docs.netbird.io/api/resources/dns-zones + openZro's nameserver-group
// code as template. No upstream AGPL diff consulted.
package types

import (
	"time"
)

// DNSRecordType enumerates the record types accepted in v1 (A, AAAA,
// CNAME). Stored as a free-form string column so future types
// (MX/TXT/SRV/PTR — see ADR-0022 "Out of scope") can be added without
// a schema migration. Validation at write time restricts to these
// three.
const (
	DNSRecordTypeA     = "A"
	DNSRecordTypeAAAA  = "AAAA"
	DNSRecordTypeCNAME = "CNAME"
)

// DNSRecordDefaultTTL matches the NetBird-documented default (300s)
// and what the agent's local resolver already enforces as cache
// lifetime when a record has no TTL set.
const DNSRecordDefaultTTL = 300

// DNSZone is a user-managed authoritative DNS namespace distributed
// to peers in selected groups. See ADR-0022 D1 for the resolution
// precedence + NXDOMAIN-authoritative semantics this enables on the
// agent.
//
// AGPL clean-room note: shape reconstructed from public OpenAPI
// documented at docs.netbird.io/api/resources/dns-zones; record
// children + group distribution materialized as separate GORM models
// (DNSRecord, DNSZoneGroup) per ADR-0022 D4.
type DNSZone struct {
	// ID is the stable identifier returned by the REST API.
	ID string `gorm:"primaryKey"`

	// AccountID scopes the zone to a tenant. Indexed because every
	// list query filters by it.
	AccountID string `gorm:"index"`

	// Name is the operator-facing label, free-form. 1-255 chars per
	// the documented OpenAPI schema.
	Name string

	// Domain is the FQDN apex of the zone (e.g. "internal.example").
	// Immutable after creation per ADR-0022 D5 — a PUT that changes
	// it is rejected, not silently accepted. Validation also rejects
	// any bidirectional suffix overlap with the account's peer DNS
	// domain (see dnsZoneOverlap in management/server/dns_zones.go).
	Domain string

	// Enabled is the operator's on/off switch independent of empty-zone
	// distribution. A disabled zone is not distributed to peers even
	// if it has records. An enabled zone with zero records is also not
	// distributed (ADR-0022 D5 empty-zone semantics).
	Enabled bool `gorm:"default:true"`

	// SearchDomainEnabled controls whether the agent appends this
	// zone's Domain to the OS DNS search list (see ADR-0022 D6).
	// Defaults false for user-managed zones; the synthetic peer zone
	// (built in Account.GetPeersCustomZone) is emitted with this flag
	// = true to preserve the current peer-name search behavior.
	SearchDomainEnabled bool `gorm:"default:false"`

	// Records is the set of A / AAAA / CNAME records under the zone
	// apex, loaded by the store as a separate JOIN. Each record's
	// Name MUST be a subdomain of (or equal to) the zone's Domain;
	// enforced server-side.
	Records []DNSRecord `gorm:"foreignKey:ZoneID;constraint:OnDelete:CASCADE"`

	// DistributionGroups is the set of peer-group IDs that decide
	// which peers see this zone. At least one is required (ADR-0022
	// D5). Materialized as the dns_zone_groups join table.
	DistributionGroups []DNSZoneGroup `gorm:"foreignKey:ZoneID;constraint:OnDelete:CASCADE"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// DNSRecord is a single resource record under a zone. v1 supports
// A / AAAA / CNAME only; the CNAME mutex with A/AAAA at the same
// hostname is enforced at write time (ADR-0022 D5).
type DNSRecord struct {
	// ID is the stable identifier returned by the REST API. Records
	// have their own IDs so PATCH/PUT operations are addressable
	// without round-tripping the whole zone.
	ID string `gorm:"primaryKey"`

	// ZoneID is the FK back to the parent DNSZone. Indexed for the
	// per-zone listing path.
	ZoneID string `gorm:"index"`

	// Name is the FQDN of the record (e.g. "db.internal.example").
	// MUST be the zone domain or a subdomain of it; rejected
	// server-side otherwise.
	Name string

	// Type is one of "A" | "AAAA" | "CNAME" in v1. Free-form column
	// so future types are additive without a schema migration; the
	// API rejects unknown values at write time.
	Type string

	// Content is the record value — IPv4 for A, IPv6 for AAAA,
	// hostname for CNAME. Shape validation per type happens at the
	// manager layer.
	Content string

	// TTL in seconds; 300 default. >= 0; no upper bound.
	TTL int `gorm:"default:300"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// DNSZoneGroup is the (zone_id, group_id) many-to-many materialized
// per ADR-0022 D4. Composite primary key prevents duplicate group
// assignments without an extra unique index.
type DNSZoneGroup struct {
	// ZoneID is the FK back to the parent DNSZone (composite PK,
	// part 1). CASCADE on delete via DNSZone.DistributionGroups.
	ZoneID string `gorm:"primaryKey"`

	// GroupID references the peer-group ID. NOT a hard FK to the
	// groups table — the manager layer validates membership at write
	// time (mirrors how nameserver-group's Groups []string works at
	// dns/nameserver.go:63) so a stale group reference triggers a
	// validation error rather than a DB-level CASCADE/RESTRICT
	// surprise on group deletion.
	GroupID string `gorm:"primaryKey"`
}

// EventMeta returns activity-event metadata for the DNS zone. Mirrors
// the NameServerGroup pattern at dns/nameserver.go:85-87.
func (z *DNSZone) EventMeta() map[string]any {
	return map[string]any{"name": z.Name, "domain": z.Domain}
}

// EventMeta returns activity-event metadata for a single record.
func (r *DNSRecord) EventMeta() map[string]any {
	return map[string]any{"name": r.Name, "type": r.Type}
}

// Copy returns a deep clone of the zone, including records and group
// memberships. Mirrors NameServerGroup.Copy() at
// dns/nameserver.go:136-154; used by Account.Copy() to keep the
// in-memory map detached from any caller mutation.
func (z *DNSZone) Copy() *DNSZone {
	clone := &DNSZone{
		ID:                  z.ID,
		AccountID:           z.AccountID,
		Name:                z.Name,
		Domain:              z.Domain,
		Enabled:             z.Enabled,
		SearchDomainEnabled: z.SearchDomainEnabled,
		Records:             make([]DNSRecord, len(z.Records)),
		DistributionGroups:  make([]DNSZoneGroup, len(z.DistributionGroups)),
		CreatedAt:           z.CreatedAt,
		UpdatedAt:           z.UpdatedAt,
	}
	copy(clone.Records, z.Records)
	copy(clone.DistributionGroups, z.DistributionGroups)
	return clone
}

// Copy returns a value-deep clone of the record. DNSRecord is already
// value-only (no slices/maps); a struct copy via assignment is
// sufficient, but the method exists for symmetry with DNSZone.Copy().
func (r *DNSRecord) Copy() *DNSRecord {
	c := *r
	return &c
}

// GroupIDs returns just the peer-group IDs for the zone in a flat
// slice — convenience for the validation + per-peer membership check
// paths that don't care about the join-table struct shape.
func (z *DNSZone) GroupIDs() []string {
	out := make([]string, len(z.DistributionGroups))
	for i, g := range z.DistributionGroups {
		out[i] = g.GroupID
	}
	return out
}
