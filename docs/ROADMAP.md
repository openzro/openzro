# openZro Roadmap

Snapshot taken 2026-04-27. Items are ordered by priority within each
band; difficulty estimate is a rough engineering-week budget for one
contributor (multiply by ~1.5 for review + integration polish).

This document is intentionally short. Each item links to the file or
ADR where the real design lives — the roadmap exists so contributors
know what's queued, not as a substitute for reading the code.

---

## Status legend

- 🟢 **Simple** — < 1 eng-week. Mechanical work, well-defined contract.
- 🟡 **Medium** — 1–3 eng-weeks. Some design judgment, multiple files.
- 🔴 **Complex** — 3+ eng-weeks. Architectural lift, cross-team coordination.

---

## P1 — Maintenance / security

### Bump Next.js 14.2.28 → 15.5.15+ (or 16.x) — 🟡

Closes [GHSA-q4gf-8mx6-v5v3](https://github.com/advisories/GHSA-q4gf-8mx6-v5v3)
and [GHSA-h25m-26qc-wcjf](https://github.com/advisories/GHSA-h25m-26qc-wcjf),
both DoS advisories on the App Router. Triage details in
[`docs/security/advisories.md`](security/advisories.md).

The bump itself is not hard; the surface that needs verification is the
App Router rewrites, the dynamic-import boundaries, and the few
`"use client"` leaves that touch `next/navigation`. Plan:

1. Bump `next` + `eslint-config-next` in `dashboard/package.json`.
2. Run `npm run build` and `npx tsc --noEmit`.
3. Smoke-test the dashboard locally: peers list, settings, integrations,
   network-traffic.
4. Run the Cypress suite (`make test.dashboard`).

Skip Next 15.0.0–15.5.14 entirely — that range carries
[CVE-2025-55182](https://github.com/advisories/GHSA-f82v-jwr5-mffw)
(RCE in Server Components). Jump straight to 15.5.15+.

---

## P2 — Coverage / scale

### Tanium posture provider — 🟢

Same `Provider` interface as Intune / SentinelOne / Huntress / CrowdStrike
in [`management/server/mdm/`](../management/server/mdm/). Tanium has
both a SaaS and on-prem flavor; we target the SaaS REST API.

- **Auth:** API token (`session=<token>` header).
- **Lookup:** `GET /api/v2/computers?name=<host>` returns the asset
  record with `last_registration` (online check) +
  `endpoint_protection.compliant` if the EPP module is licensed.
- **Compliance rule:** asset present + last_registration recent +
  EPP compliant if licensed; otherwise asset present + last
  registration ≤ 24h.

Reference shape: [`management/server/mdm/crowdstrike.go`](../management/server/mdm/crowdstrike.go).

### Jamf Pro posture provider (macOS) — 🟢

macOS-only MDM. Same pattern.

- **Auth:** OAuth2 client_credentials → `/api/v1/auth/token`.
- **Lookup:** `GET /api/v1/computers-inventory?filter=hardware.serialNumber:'<sn>'`
  or `general.name:'<host>'`. The body has `general.lastReportedIp`,
  `general.userApprovedMdm`, `security.gatekeeper`,
  `security.systemIntegrityProtectionEnabled`, `security.firewallEnabled`,
  `security.fileVault2Enabled`.
- **Compliance rule:** SIP on + Gatekeeper on + FileVault2 on (the
  baseline most security teams enforce). Operators can extend the
  rule via posture-check parameters later.

Identifier matching is awkward — peers report hostname, Jamf prefers
serial number. Document the trade-off and resolve by hostname first,
fall back to serial if peer.Meta carries it.

### Kandji posture provider (macOS) — 🟢

Modern competitor to Jamf, same shape.

- **Auth:** Bearer token (`Authorization: Bearer <token>`).
- **Lookup:** `GET /api/v1/devices?platform=Mac&hostname=<host>` →
  device list, then `GET /api/v1/devices/{id}/details` for the
  compliance flags.
- **Compliance rule:** `is_managed=true` +
  `last_check_in` recent + Mac-specific flags
  (`mdm_enabled`, `agent_installed`).

### Custom EDR/MDM via OpenAPI — 🟡

Once Tanium/Jamf/Kandji land, the obvious next step is a
config-driven provider where the operator points openZro at a vendor's
REST endpoint with a hostname-keyed lookup and a JSONPath assertion
("device.compliant must be true"). Not strictly necessary, but it
unblocks every long-tail vendor without touching Go code. Defer until
a real customer asks.

---

## P3 — Deferred (waiting on demand or major lift)

### SAML 2.0 direct (without OIDC bridge) — 🔴

The IdPs that currently work go through OIDC. Some enterprise customers
need SAML directly (especially Okta SAML, ADFS). Implementation is
**~2 eng-weeks** and adds a real dependency on a SAML library
(`github.com/crewjam/saml` is the standard).

Prerequisites:
- Decide assertion-encryption support (most customers don't use it).
- Decide if we host SP metadata at `/saml/metadata` or only the ACS
  URL `/saml/acs`.
- Pick an ADR template and document the trust model.

Open this when a customer signs a contract that names SAML as a
requirement.

### MSP portal / multi-tenant — 🔴

Letting an MSP manage multiple openZro tenants from one console is a
**major architectural lift**: tenant scoping in the data layer, billing
hooks, per-tenant audit isolation, role propagation. Realistic budget
is **6–10 eng-weeks** plus product-design time.

Defer until there's an MSP customer asking. Until then, MSPs can run N
parallel openZro deployments — clunky but not blocking.

### Port forwarding (real implementation) — 🟡

Currently a no-op stub at
[`management/integrations/integrations/port_forwarding.go`](../management/integrations/integrations/port_forwarding.go).
Not in NetBird's public pricing tier, but operators do request the
feature occasionally for legacy services that can't be re-fronted.

The lift is moderate — the proto / wire format already exists; what's
missing is the dataplane integration. ~1 eng-week if we limit scope to
TCP forwarding on the gateway peer; longer if UDP + per-peer rules
are in scope.

Open this when a customer asks. Don't speculate on the design.

### DORA / SOC2 / ISO 27001 reports — 🔴 (process work)

This is **not code** — it's retention policies, evidence collection
runbooks, vendor audit binders, and yearly rituals. The compliance
mapping for Bacen is in
[`docs/compliance/bacen-4893-mapping.md`](compliance/bacen-4893-mapping.md);
DORA / SOC2 / ISO follow the same pattern.

Realistic timeline: **3–6 months** of process work plus ~1 eng-week
of audit-evidence tooling (CSV exports, signed audit-log archives).

Defer until openZro is being sold to a customer that names one of
these compliance frameworks as a requirement.

---

## Out of scope (intentionally)

- **Mobile clients.** iOS / Android forks of NetBird's mobile
  codebase exist, but mobile is not on the openZro roadmap until the
  desktop story is fully stable.
- **Premium goreleaser pipelines** (Docker Hub, Homebrew tap, signed
  Windows / notarized macOS builds). Tracked in
  [`.goreleaser.yaml`](../.goreleaser.yaml) for the day signing certs
  are sponsored — see `.goreleaser.binaries.yaml` for the lightweight
  config we actually run today.
- **NetBird-specific cloud features** (the `app.netbird.io` SaaS
  surface). openZro is a self-host-first project; a hosted SaaS
  is its own product decision.

---

## Recently shipped (don't re-implement)

For context — these were on the gap list at one point and are now
done. Skip these when picking up tasks.

- Peer approval (real implementation, not the no-op stub) —
  [`management/integrations/integrations/validator.go`](../management/integrations/integrations/validator.go)
- SCIM 2.0 endpoints —
  [`management/server/http/handlers/scim/`](../management/server/http/handlers/scim/)
- Activity Streamer (Generic HTTP webhook + Datadog + Elastic +
  custom payload templates) —
  [`management/server/activity/exporter/`](../management/server/activity/exporter/)
- Traffic events ingestion + UI —
  [`management/server/flow_service.go`](../management/server/flow_service.go),
  [`dashboard/src/app/(dashboard)/events/network-traffic/`](../dashboard/src/app/(dashboard)/events/network-traffic/)
- MDM/EDR providers: Intune, SentinelOne, Huntress, CrowdStrike Falcon —
  [`management/server/mdm/`](../management/server/mdm/)
- Flow exports: S3, GCS, Datadog Logs Intake, Elastic, generic HTTP —
  [`flow/sinks/`](../flow/sinks/)
- Device Admission gate + admission bypass + group-scope exemption —
  [ADR-0003](adr/0003-peer-device-admission.md),
  [ADR-0004](adr/0004-admission-bypass-and-group-scope.md)
- Version-check via GitHub Releases API —
  [`version/update.go`](../version/update.go)
- Lightweight binary release pipeline —
  [`.goreleaser.binaries.yaml`](../.goreleaser.binaries.yaml),
  [`.github/workflows/release-binaries.yml`](../.github/workflows/release-binaries.yml)
