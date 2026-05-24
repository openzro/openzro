// Custom DNS Zones — dashboard-side TypeScript mirror of the OpenAPI
// schema shipped in issue #108 Phase 1.
//
// The dashboard does not auto-generate types from the management
// OpenAPI; this file mirrors the schema by hand, same as
// interfaces/Nameserver.ts. If drift becomes painful we can adopt
// codegen later — for now the API surface is small (10 endpoints,
// 2 main shapes + 2 request bodies).

export type DNSRecordType = "A" | "AAAA" | "CNAME";

// DNSRecord is a single resource record under a zone. v1 supports
// A / AAAA / CNAME only; the CNAME ↔ A/AAAA mutex is enforced
// server-side per ADR-0022 D5.
export interface DNSRecord {
  id: string;
  name: string;
  type: DNSRecordType;
  content: string;
  // TTL in seconds. Default 300 server-side when omitted. OpenAPI
  // declares `minimum: 1` (see Phase 1 review #4 — TTL=0 would bypass
  // resolver caches and defeat the authoritative-zone purpose).
  ttl?: number;
}

// DNSRecordRequest is the create/update body for a record. Same
// shape as DNSRecord minus the server-assigned id.
export interface DNSRecordRequest {
  name: string;
  type: DNSRecordType;
  content: string;
  ttl?: number;
}

// DNSZone is an operator-managed authoritative DNS namespace
// distributed to peers in selected groups. Resolution on the agent
// is authoritative for the zone (NXDOMAIN on miss within the zone
// — see ADR-0022 D1). The agent does NOT fall through to upstream
// nameservers for records the zone could carry; this is the
// "shadow domain" guarantee.
export interface DNSZone {
  id: string;
  // Human-readable name (1-255 chars).
  name: string;
  // FQDN apex of the zone (e.g. "internal.example"). Immutable
  // after creation (ADR-0022 D5). The dashboard renders the domain
  // input disabled on edit.
  domain: string;
  // When false, the zone is NOT distributed to peers — even if it
  // has records. Useful for "pause without deleting" workflows.
  // Default true on create.
  enabled?: boolean;
  // When true, the agent appends the zone's Domain to the OS DNS
  // search list (so a bare-name lookup like `db` resolves as
  // `db.<Domain>`). Default false for user-managed zones; the
  // synthetic peer zone has this true unconditionally (set
  // server-side, not exposed in this API).
  enable_search_domain?: boolean;
  // Peer group IDs that receive this zone. ≥1 required at
  // create AND update; the server enforces.
  distribution_groups: string[];
  // Records currently registered under the zone. Populated on
  // GET responses by the server's preload chain. The dashboard
  // also fetches records via the dedicated records endpoints
  // when the modal is open; this slice is what comes back on the
  // list / get path.
  records: DNSRecord[];
}

// DNSZoneRequest is the create/update body. Excludes id and
// records (records are managed via the dedicated /records
// endpoints, not bundled into the zone PUT body — the server
// preserves existing records on PUT regardless).
export interface DNSZoneRequest {
  name: string;
  domain: string;
  enabled?: boolean;
  enable_search_domain?: boolean;
  distribution_groups: string[];
}
