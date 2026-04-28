package api

// DNS-as-a-service types — clean-room additions on top of the
// upstream NetBird v0.53.0 BSD-3 surface. Upstream added DNS Zones
// + Records as a managed feature post-fork; openZro reintroduces
// them here so the Kubernetes operator (openzro/openzro-operator)
// can compile against our REST client. Server-side handlers and
// storage migrations are tracked separately in the enterprise
// feature gap memo (DNS Zones — milestone TBD).
//
// These structs intentionally live in a hand-written file
// (not in types.gen.go) so the OpenAPI codegen workflow doesn't
// fight us until we actually add the spec entries.

// Zone is a managed DNS zone owned by an openZro account. The
// account-id-scoped zone holds DNSRecord entries pointing at peers,
// network resources, or external IPs.
type Zone struct {
	// Id is the opaque server-assigned identifier.
	Id string `json:"id"`

	// Name is the operator-friendly label of the zone (e.g. "Acme-internal").
	Name string `json:"name"`

	// Description is operator-supplied free text.
	Description string `json:"description,omitempty"`

	// Domain is the FQDN suffix the zone serves (e.g. "internal.example.com").
	// All Records in the zone are appended to this suffix.
	Domain string `json:"domain"`

	// DistributionGroups is the set of group IDs that receive the
	// zone's records pushed to their /etc/resolv.conf via the
	// daemon's DNS interceptor.
	DistributionGroups []string `json:"distribution_groups,omitempty"`

	// EnableSearchDomain reports whether the zone's domain is added
	// to the search list (so peers can resolve `foo` → `foo.<domain>`).
	EnableSearchDomain *bool `json:"enable_search_domain,omitempty"`

	// Enabled toggles whether peers should respect this zone in
	// their resolver.
	Enabled bool `json:"enabled"`

	// Records is the (optionally embedded) list of records in this
	// zone — populated on detail endpoints, omitted on list views.
	Records []DNSRecord `json:"records,omitempty"`
}

// ZoneRequest is the payload for creating or updating a zone.
// Pointer fields denote partial-update semantics: nil = leave unchanged.
type ZoneRequest struct {
	// Name is required.
	Name string `json:"name"`

	// Description is operator-supplied free text.
	Description string `json:"description,omitempty"`

	// Domain is the FQDN suffix the zone serves. Required on create.
	Domain string `json:"domain"`

	// DistributionGroups is the set of group IDs that receive the
	// zone's records pushed to their resolver config.
	DistributionGroups []string `json:"distribution_groups,omitempty"`

	// EnableSearchDomain — see Zone.EnableSearchDomain.
	EnableSearchDomain *bool `json:"enable_search_domain,omitempty"`

	// Enabled — pointer so callers can omit (leaves the persisted
	// value untouched on update).
	Enabled *bool `json:"enabled,omitempty"`
}

// DNSRecordType enumerates the supported DNS record types. Mirrors
// the standard RFC 1035 / RFC 3596 set; openZro currently surfaces
// A and AAAA via the operator (CNAME / TXT / SRV are storage-ready
// but not yet wired to controllers).
type DNSRecordType string

const (
	DNSRecordTypeA     DNSRecordType = "A"
	DNSRecordTypeAAAA  DNSRecordType = "AAAA"
	DNSRecordTypeCNAME DNSRecordType = "CNAME"
	DNSRecordTypeTXT   DNSRecordType = "TXT"
	DNSRecordTypeSRV   DNSRecordType = "SRV"
)

// DNSRecord is a single record inside a Zone.
type DNSRecord struct {
	// Id is the server-assigned identifier.
	Id string `json:"id"`

	// Name is the record's hostname relative to the zone's apex.
	Name string `json:"name"`

	// Type is the record kind (A, AAAA, CNAME, …).
	Type DNSRecordType `json:"type"`

	// Content is the type-specific payload — for A records it's an
	// IPv4 string; for AAAA an IPv6 string; for CNAME a target FQDN.
	Content string `json:"content"`

	// Ttl is the cache duration in seconds.
	Ttl int `json:"ttl"`
}

// DNSRecordRequest is the payload for creating or updating a
// record inside a zone. ZoneID is taken from the URL path; this
// type carries only the body fields.
type DNSRecordRequest struct {
	Name    string        `json:"name"`
	Type    DNSRecordType `json:"type"`
	Content string        `json:"content"`
	Ttl     int           `json:"ttl"`
}
