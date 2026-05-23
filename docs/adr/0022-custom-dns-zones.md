# ADR-0022 — Custom DNS Zones: private authoritative zones with group-based distribution

## Status

**Proposed**. Phase 0 of [openzro/openzro#108](https://github.com/openzro/openzro/issues/108).
This ADR settles the gating decisions (D1–D8) so the phased
implementation can start. **No code lands with this ADR** — it is the
prerequisite gate the issue mandates.

**Clean-room mandate.** `management/`, `signal/`, `relay/`, `combined/`
are AGPL; the upstream `netbirdio/netbird` v0.63 Custom DNS Zones
implementation lives there. This ADR and every implementation phase
reimplement from **public sources only** — the NetBird docs
([dns/custom-zones](https://docs.netbird.io/manage/dns/custom-zones),
[api/resources/dns-zones](https://docs.netbird.io/api/resources/dns-zones),
[knowledge-hub/custom-dns-zones](https://netbird.io/knowledge-hub/custom-dns-zones))
and openZro's own existing DNS code (nameserver groups in
`management/server/nameserver.go` + `client/internal/dns/`). The
upstream AGPL diff is **not** consulted or ported; each commit cites
its public sources and confirms this. `client/` is BSD-3 and may
extend existing in-tree DNS code freely.

## Context

NetBird shipped Custom DNS Zones in v0.63 — internal DNS namespaces
(e.g. `internal.company.io`) managed centrally, distributed to peer
groups, resolved locally on the agent. openZro lacks this feature; the
demand pattern is real (homelab and small-enterprise teams that don't
want to stand up a separate authoritative DNS server). The feature
parity gap was already tracked on the roadmap memory.

**The architectural insight that lowers the cost.** The openZro agent
**already** runs a DNS server: [client/internal/dns/service_listener.go:32-78](../../client/internal/dns/service_listener.go#L32-L78)
embeds `*dns.Server` from `github.com/miekg/dns` and binds UDP+TCP on
the WireGuard interface gateway address. A handler chain
([handler_chain.go](../../client/internal/dns/handler_chain.go)) already
multiplexes between nameserver-group forwarders and the system
fallback. Adding Custom DNS Zones is therefore *not* "build a DNS
server inside the agent" — it is "add one more handler to the existing
chain, before the upstream forwarders." Same data-plane shape openZro
already uses for nameserver groups; same `management → sync → agent`
distribution path; no new sockets, no new binaries, no central DNS
server.

**Storage model on the management side.** openZro's
[nameserver-group code](../../management/server/nameserver.go) already
demonstrates the canonical pattern: persisted in dedicated tables,
included in the per-peer network map at recompute time
([management.proto:455](../../management/proto/management.proto#L455)),
delivered over the existing sync stream. Custom zones follow the same
pattern.

**What needs deciding NOW** is *behavior under conflict*, not
plumbing. Specifically: resolution precedence when a zone overlaps a
nameserver-group match-domain, NXDOMAIN semantics for authoritative
zones, sync model (full state vs deltas), and the scope of the v1
record types. Once these are pinned, the implementation is largely
mechanical extension of established patterns.

## Decision

### D1 — Resolution precedence: custom zones win over nameserver groups, NXDOMAIN is authoritative FOR DISTRIBUTED ZONES

The agent's DNS handler chain (already implemented at
[client/internal/dns/handler_chain.go:13-19](../../client/internal/dns/handler_chain.go#L13-L19)
with `PriorityLocal=100` > `PriorityUpstream=50` > `PriorityFallback=-100`)
runs in this order:

1. **Custom zones** (a known zone is one the peer received over sync) — if the queried name is the apex or any subdomain:
   - Lookup the exact record; if found, synthesize the response and
     return.
   - If no record matches, **return NXDOMAIN authoritatively**. The
     chain stops here (NXDOMAIN without the `Zero` bit terminates the
     chain per
     [handler_chain.go:48-55](../../client/internal/dns/handler_chain.go#L48-L55)).
2. **Nameserver groups (existing)** — match-domain rules forward to
   configured upstream nameservers.
3. **System resolver (existing)** — fallthrough.

**Scope of "known zone".** A zone is "known to the agent" if and only
if it arrived in `DNSConfig.CustomZones` on the latest sync update. An
empty zone (no records) is **not distributed** (see D5) — the agent
never sees it — so the NXDOMAIN-authoritative behavior applies only to
zones that have at least one record. This matches the existing
defensive skip at
[client/internal/dns/server.go:603-607](../../client/internal/dns/server.go#L603-L607).

The NXDOMAIN-authoritative behavior is what gives "shadow domain" its
security property: an operator can name an internal zone
`internal.openzro.io` without queries for nonexistent records leaking
to the public DNS for that domain. Falling through to nameserver
groups would break that property AND create a confusing semantic
("the operator owns this zone but the system still asks externally").

**Reviewer note for the implementer**: the temptation to "fix" the
NXDOMAIN branch by falling through is high. Comment heavily on the
custom-zone handler with a reference to this ADR section and add an
explicit unit test that pins NXDOMAIN return for an unknown record
within a known zone.

### D2 — Sync model: full zone state per peer, not deltas

Every sync update delivers the full set of zones+records visible to
the peer (computed from the zone's distribution groups vs the peer's
group membership). No incremental delta protocol.

Rationale:
- Idempotent. A reconnecting peer that missed N intermediate updates
  converges on the first sync.
- Matches the existing pattern: nameserver groups, ACL rules, and
  peer-to-peer routes all ship full-state per sync.
- Scale headroom is enormous: 100 zones × 10 records × ~50 bytes ≈
  50 KB per peer per sync. Network maps already carry this order of
  magnitude in policy + peer data.
- Future delta protocol is non-breaking to add later (deltas would
  ride alongside; the full-state path stays as the fallback).

### D3 — Record types in v1: A, AAAA, CNAME only

Match upstream's v1 surface. Defer:

- **MX, TXT, SRV** — rarely useful in internal mesh DNS; complicate
  validation (TXT) and UI (per-record priority for MX/SRV). Add per
  demand.
- **Wildcard records** (`*.foo.zone`) — surprising semantics; demand
  a separate ADR on precedence vs explicit records.
- **PTR (reverse)** — not in the NetBird v1 either; out of scope.

### D4 — Storage: three relational tables, not a JSON blob

Three new GORM models, AutoMigrate'd alongside the existing ones in
[sql_store.go](../../management/server/store/sql_store.go):

- `dns_zones` — `(id, account_id, name, domain, enabled, enable_search_domain, created_at, updated_at)`
- `dns_records` — `(id, zone_id [FK], name, type, content, ttl)`
- `dns_zone_groups` — `(zone_id [FK], group_id [FK])` many-to-many

Rationale over a single JSON blob on the account:

- Indexable per-record (`type`, `name`) — recompute can JOIN-load
  efficiently per peer's group set.
- Validation enforced at insert time (FK to groups, FK to zone).
- Activity events reference stable IDs.
- Same pattern as `nameserver_groups` already in the schema.

**Indexes**: `dns_records(zone_id)`, `dns_zone_groups(group_id)`,
`dns_zones(account_id)`. The recompute path is
`SELECT zones JOIN zone_groups ON group_id IN (peer.groups) JOIN records ON zone_id`
— bounded by zones-the-peer-sees, not by global zone count.

### D4b — Wire model: extend the existing `CustomZone`, do NOT add a parallel field

openZro **already** has a `CustomZones` distribution path. It is
populated today with a single synthetic entry — the auto-generated
peer DNS zone (`<account.DNSDomain>` with one A record per
`peer.DNSLabel` + extras, built at
[management/server/types/account.go:453-510](../../management/server/types/account.go#L453-L510)
and emitted at line 301). Proto: `DNSConfig.CustomZones` at
[management.proto:455](../../management/proto/management.proto#L455);
domain type: `nbdns.CustomZone` at [dns/dns.go:38-42](../../dns/dns.go#L38-L42);
client-side ingestion: `DefaultServer.buildLocalHandlerUpdate` at
[server.go:599](../../client/internal/dns/server.go#L599).

User-managed zones **reuse the same `CustomZones` list** rather than
shipping in a parallel `repeated DNSZone` field. The network-map
computation appends user zones (computed from the peer's group set,
per D2) onto the same slice that already carries the peer zone. One
wire path, one client handler, one mental model.

To support the new flags (search-domain per zone, type-of-zone
distinction), `CustomZone` gains two **additive, optional** fields —
which protobuf treats as default-zero on the wire so older agents
ignore them gracefully:

```proto
message CustomZone {
  string Domain = 1;
  repeated SimpleRecord Records = 2;
  bool SearchDomainEnabled = 3;  // NEW — see D6
  CustomZoneSource Source = 4;   // NEW — distinguishes synthetic peer zone vs user-managed
}

enum CustomZoneSource {
  CUSTOM_ZONE_SOURCE_UNSPECIFIED = 0;  // legacy / older agents
  CUSTOM_ZONE_SOURCE_PEERS = 1;        // the synthetic peer DNS zone
  CUSTOM_ZONE_SOURCE_USER = 2;         // operator-managed via the new API
}
```

`Source` is informational (lets the client distinguish for telemetry/
logging); semantics for resolution are driven by `Domain`/`Records`/
`SearchDomainEnabled`, not by `Source`.

**Migration of existing behavior**:
- Peer zone today (synthetic) → emitted with `SearchDomainEnabled=true`
  (preserves current `host.go:112-118` behavior of adding the peer
  domain to the search list — that's what makes `dig myhost` work
  without typing `.openzro`).
- User-managed zones → emitted with `SearchDomainEnabled=` whatever
  the operator set per zone (default `false`).
- Old agents on the network: see `SearchDomainEnabled=0` for ALL
  zones. Their existing code (host.go:112) adds every zone to the
  search list — that's the legacy behavior, no regression.

### D5 — Validation surface

Enforced server-side at zone/record write time, returned as
`InvalidArgument` errors mapped to HTTP 400:

- **Zone domain**: FQDN syntax. Immutable after creation (immutable
  field documented in the OpenAPI schema; PUT silently ignores
  `domain` changes — matches NetBird's documented behavior).
- **Zone domain must NOT overlap with the peer DNS zone**, in either
  direction. The peer DNS zone is rooted at
  `account.Settings.DNSDomain` (default `openzro`) and is generated
  with one A record per peer's `DNSLabel` plus extras
  ([account.go:453-510](../../management/server/types/account.go#L453-L510)).
  A user-managed zone domain MUST be rejected if it is:
  - Identical to the peer DNS domain (e.g. `openzro`); OR
  - An ancestor of the peer DNS domain (e.g. `io` when peers live
    at `*.openzro`) — would shadow all peer resolution; OR
  - A descendant of the peer DNS domain (e.g. `private.openzro`) —
    a more-specific zone wins by D1's precedence, so the peer
    sub-FQDN `private.openzro` itself would resolve via the user
    zone, breaking the mesh's own name resolution.

  The check is a **bidirectional suffix overlap** on labelized FQDNs.
  Reuse a helper sketched as `dnsZoneOverlap(zoneDomain, peerDomain) bool`
  that returns true iff one is a suffix of the other (label-aligned).
- **Record `name` must be a subdomain of (or equal to) the zone's
  `domain`**. Reject otherwise. Same constraint upstream enforces.
- **CNAME mutex**: a hostname with a CNAME may NOT also have an A or
  AAAA. RFC 1034 §3.6.2. Enforced via a unique constraint
  `(zone_id, name)` AND a runtime check on insert when type is mixed.
- **TTL**: integer ≥ 0, default 300s. No upper bound.
- **Distribution groups**: at least one group required at creation
  AND at every update. A zone with zero groups is hidden from all
  peers — accepting that state is a foot-gun (operator believes the
  zone is "live" when nobody resolves it). Better to reject at the
  API layer.
- **Empty-zone distribution**: a zone with zero records is allowed
  to exist on the management side (operator may be mid-edit between
  delete-old-record and add-new-record) but is **NOT included in any
  peer's `DNSConfig.CustomZones`**. This matches NetBird's
  documented "empty zones receive no distribution" semantics AND
  the existing defensive skip at
  [client/internal/dns/server.go:603-607](../../client/internal/dns/server.go#L603-L607).
  Consequence for D1: an empty zone is *not* "known to the agent" →
  not subject to NXDOMAIN-authoritative behavior → falls through.
  Dashboard MUST surface a clear "this zone has no records — it is
  effectively disabled until you add one" hint to the operator.

### D6 — Search-domain semantics: per-zone flag on the wire, client respects it

`enable_search_domain` (boolean on the zone, defaulting `false`)
extends the peer's DNS search list with the zone's domain. When
enabled, a bare-name query like `db` is tried as `db.<zone-domain>`
if the literal `db` doesn't resolve. Same behavior as nameserver-group
`SearchDomainsEnabled` already in openZro.

**Wire-level requirement**: the flag travels as
`CustomZone.SearchDomainEnabled` (see D4b). Without this field on the
wire, the client at
[client/internal/dns/host.go:112-118](../../client/internal/dns/host.go#L112-L118)
unconditionally adds every non-reverse `CustomZone.Domain` to the
search list — that's the current behavior for the synthetic peer
zone (where we DO want it in the search list). Generalizing that to
user-managed zones without a per-zone flag would force EVERY
user-managed zone into the search list, breaking D6's "default off,
opt-in" promise.

The Phase 3 client change at `host.go:112` reads as:

```go
for _, customZone := range dnsConfig.CustomZones {
  matchOnly := strings.HasSuffix(customZone.Domain, ipv4ReverseZone) ||
               strings.HasSuffix(customZone.Domain, ipv6ReverseZone) ||
               !customZone.SearchDomainEnabled
  config.Domains = append(config.Domains, DomainConfig{
    Domain:    strings.ToLower(dns.Fqdn(customZone.Domain)),
    MatchOnly: matchOnly,
  })
}
```

The synthetic peer zone is emitted with `SearchDomainEnabled=true`
(see D4b "Migration of existing behavior") so the current peer-name
search behavior is preserved.

Edge case: when two zones distributed to the same peer BOTH have
`enable_search_domain: true` and the OS resolver tries the same bare
name against both, results are OS-dependent and may be
non-deterministic. The agent logs a once-per-(name, conflict-set)
warning. This is documented operator foot-gun, not a feature.

### D7 — Cross-platform Phase 3 gate

The agent's DNS interceptor is *mature only on Android* per
[openzro/openzro#14](https://github.com/openzro/openzro/issues/14)
(fake-IP/DNAT path). On Linux, macOS, Windows, iOS the DNS-server
binding works but the integration with the OS resolver settings has
known fragility.

Custom zones do not introduce new platform-specific code, but they
amplify any existing fragility (every internal query now goes through
the openZro resolver, where today queries to internal names may have
hit nameserver groups OR fallen through to OS resolution). Phase 3
(client) is **gated by platform**:

- Phase 3a (lands with the rest): Linux + Android verified end-to-end.
- Phase 3b (separate PR, gated on #14 progress): macOS, Windows, iOS.

Dashboard does NOT hide the feature by platform — operators can
configure zones any time; the per-platform readiness shows in agent
release notes. This keeps the management-side surface stable.

### D8 — License posture, per phase

| Phase | Path | License | Source-of-truth |
|---|---|---|---|
| 0 | `docs/adr/0022-*.md` | docs (CC-BY-style) | This ADR. |
| 1 | `management/server/dns_zones*.go`, `management/server/types/dns_zone*.go`, `management/server/store/sql_store.go`, `management/server/http/handlers/dns/zones_handler.go`, `management/server/http/api/openapi.yml` | **AGPL clean-room** | NetBird public docs (linked above) + openZro's own nameserver-group code. |
| 2 | `management/proto/management.proto` + `dns/dns.go` + `management/server/dns.go` + sync recompute | **AGPL clean-room** | Same. **Extend** the existing `CustomZone` message with `SearchDomainEnabled` + `Source` (additive, wire-compatible — older agents see field-zero defaults and behave exactly as today). User-managed zones append onto the same `DNSConfig.CustomZones` list that already carries the synthetic peer zone. |
| 3 | `client/internal/dns/host.go`, `client/internal/dns/server.go` (honor `SearchDomainEnabled`; existing `buildLocalHandlerUpdate` already handles `CustomZones`) + `dns/dns.go` (the shared BSD struct gains the same fields as the proto) | BSD-3 | Extension of existing BSD agent code. **Public docs + in-tree openZro code only** — even though upstream `client/` is BSD-licensed and could legally be consulted, mixing "consult some upstream paths, not others" within this initiative muddies the clean-room signal. Keep the discipline uniform across all four phases. |
| 4 | `dashboard/src/app/(v2-dashboard)/dns/zones/` | BSD-3 | Pattern-identical to existing nameserver-group dashboard UI. |

Every Phase 1+2 commit cites public sources and confirms `no AGPL
diff consulted` — same convention as ADRs 0020, 0021 and the security
backports (`0f956e72`, `3196cbbf`, `c761e80f`).

## Phase plan (concrete)

| Phase | Scope | Deliverable | Estimate |
|---|---|---|---|
| 0 | This ADR | merged docs PR | done |
| 1 | Backend CRUD + validation + activity events | `POST/GET/PUT/DELETE /api/dns/zones[/records]` per the OpenAPI shape; tables + AutoMigrate; permission checks (admin-only writes) | 3–4 days |
| 2 | Sync proto + UpdateAccountPeers integration | Extend the existing `CustomZone` proto with `SearchDomainEnabled` + `Source` (additive, wire-compatible per D4b); append user-managed zones to `DNSConfig.CustomZones` alongside the synthetic peer zone; recompute path JOIN-loads zones+records+groups; `zoneChangesAffectPeers` analog to `areNameServerGroupChangesAffectPeers` | 2–3 days |
| 3a | Linux + Android client | Extend the existing `buildLocalHandlerUpdate` in `client/internal/dns/server.go` (already handles `CustomZones`) to honor the new `SearchDomainEnabled` flag in `host.go:112`. NXDOMAIN authoritative for distributed zones (already terminates the chain via `handler_chain.go:48-55`). TTL respected. | 1–2 days |
| 3b | macOS / Windows / iOS | platform-specific verification + bug-fix PRs gated on #14 | TBD per platform |
| 4 | Dashboard `DNS → Zones` CRUD | Pattern-identical to nameserver-group editor; record sub-resource UI; distribution-group picker reusing existing `PeerGroupSelector` | 2–3 days |

**Total focused work for the management-side initiative (Phases 1+2+3a+4)**: ~9–13 days, ~2 weeks calendar with review cadence.

## Risks accepted

- **Operator self-shadows the peer DNS domain**: Validation in D5
  prevents creating a zone with the peer DNS domain, but does NOT
  prevent the operator from changing the peer DNS domain AFTER zones
  are configured. Accepted: documented in the settings UI; operator
  rotation of the peer DNS domain is rare and recoverable.
- **NXDOMAIN authoritative confuses operators**: An operator who
  forgets they configured `internal.example.com` as a private zone
  will find that public `internal.example.com` is now unreachable
  from the mesh. Mitigation: dashboard surfaces a clear "this zone
  shadows DNS for `<domain>` and everything below it on peers in the
  selected groups" warning.
- **Recompute path performance on huge accounts**: Custom zones add
  rows to the per-sync recompute. The query plan is bounded by
  zones-the-peer-sees, but a pathological account with thousands of
  zones could push sync latency. Mitigation: the existing
  `UpdateAccountPeers` benchmark suite gains a custom-zone-heavy
  scenario before Phase 2 merges. If the slope is bad, we cache the
  serialized per-(account, group-set) blob — same trick already used
  for some other recompute paths.
- **#14 cross-platform fragility leaks bugs into the feature**:
  Mitigated by D7's platform-gated Phase 3 split. If a macOS bug
  surfaces during Phase 3b, it goes into its own PR rather than
  blocking 3a from shipping.

## Out of scope (separate ADR if pursued)

- **MX / TXT / SRV / PTR record types**. Add per demand.
- **Wildcard records** (`*.foo.zone`). Demands its own precedence
  ADR vs explicit records.
- **DNSSEC**. Resolvers don't chain DNSSEC for private names, and
  authoritative DNSSEC inside the mesh adds key-management ops with
  zero query-time benefit.
- **DNS aliases for routed networks** (separate NetBird feature
  documented at
  [docs.netbird.io/manage/dns/dns-aliases-for-routed-networks](https://docs.netbird.io/manage/dns/dns-aliases-for-routed-networks)).
  Distinct enough to warrant a separate issue + ADR if pursued.
- **Per-record TTL override** (vs zone-default). v1 ships zone-default
  TTL applied to all records (or per-record TTL if the v1 storage
  already carries it — the OpenAPI does, so the field is included
  but the dashboard exposes only zone-default in v1).
- **Bulk import / zone-file ingestion** (RFC 1035 syntax). Useful
  for migrations off BIND but additive UX that doesn't gate v1.

## Verification

Backend (Phase 1+2):

```bash
go test -race -timeout 5m -count=1 ./management/server/... \
  -run "DNSZone|DNSRecord|TestUpdateAccountPeers_DNSZones"
golangci-lint run --timeout=12m ./management/server/...
make fmt.check
```

Client (Phase 3a):

```bash
go test -race -timeout 5m -count=1 ./client/internal/dns/...
# manual smoke: configure a zone with a record; on a peer-member,
# `dig @100.x.255.254 db.internal.example.com` returns the synthetic
# A record; `dig @100.x.255.254 missing.internal.example.com` returns
# NXDOMAIN (NOT a forward to the upstream nameserver).
```

End-to-end (post Phase 4):

```bash
# Dashboard: create zone "internal.example", add A "db.internal.example → 10.0.0.5",
# assign to group "team-backend".
# Peer in team-backend: dig db.internal.example → 10.0.0.5. ✓
# Peer NOT in team-backend: dig db.internal.example → SERVFAIL or fallthrough. ✓
# Same peer, removed from team-backend: next sync drops the record;
# dig returns SERVFAIL within bounded time. ✓
# CNAME mutex: API rejects adding A record on a host that already has CNAME. ✓
# Search domain: with enable_search_domain=true, `dig db` (no dots)
# resolves via the search list. ✓
```

## References

- Issue: [openzro/openzro#108](https://github.com/openzro/openzro/issues/108)
- Upstream public docs (clean-room source-of-truth):
  - [docs.netbird.io/manage/dns/custom-zones](https://docs.netbird.io/manage/dns/custom-zones)
  - [docs.netbird.io/manage/dns](https://docs.netbird.io/manage/dns)
  - [docs.netbird.io/api/resources/dns-zones](https://docs.netbird.io/api/resources/dns-zones)
  - [netbird.io/knowledge-hub/custom-dns-zones](https://netbird.io/knowledge-hub/custom-dns-zones)
- Sibling ADRs in the same clean-room-initiative pattern:
  - [ADR-0020](./0020-openzro-ssh-identity-protocol.md) — openZro SSH identity protocol.
  - [ADR-0021](./0021-policy-propagation-consistency-model.md) — Policy propagation consistency model.
- Existing openZro code referenced:
  - [management/server/nameserver.go](../../management/server/nameserver.go) — CRUD + propagation template.
  - [management/proto/management.proto:455](../../management/proto/management.proto#L455) — NetworkMap extension point.
  - [client/internal/dns/handler_chain.go](../../client/internal/dns/handler_chain.go) — DNS handler chain Phase 3 hooks into.
  - [client/internal/dns/service_listener.go:32-78](../../client/internal/dns/service_listener.go#L32-L78) — agent's existing `dns.Server` bind.
