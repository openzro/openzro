# Bacen Resolução BCB nº 4.893 / Circular nº 3.909 → openZro

This document maps the cybersecurity controls Bacen requires of
fintechs and Brazilian banks to the openZro features that implement
them. It is **not** a substitute for an internal compliance review —
it is a starting point so the security team can speak to the auditor
in concrete openZro terms ("event Y in our Activity log proves
control X").

The text follows the structure of:

- **Resolução BCB nº 4.893** of 2021-02-26 (cybersecurity policy and
  outsourced data processing for Brazilian Central Bank-supervised
  institutions).
- **Circular nº 3.909** of 2018-08-16 (the operational requirements
  that pre-dated 4.893 and remain in force where 4.893 does not
  override them).

Where article numbers are cited, they refer to 4.893 unless flagged
otherwise.

> ⚠️ This is a self-host product. A self-host operator is responsible
> for their own compliance posture; openZro the project does not hold
> a Bacen registration nor make legal guarantees. The mapping below
> is engineering correspondence, not legal advice.

## At a glance

| Bacen control area | openZro feature(s) | Audit artefact |
|---|---|---|
| Endpoint identity & admission | Device Admission gate (Phase 1+2) + Posture Checks + MDM/EDR integration | `peer.admission.deny` events; `/events/admission.csv` |
| Authentication of users | OIDC (8 IdP providers) + JWT validation; SCIM 2.0 for IdP-side lifecycle | `user.join`, `user.role.update`, IdP-side audit |
| Authorization / segmentation | Policies + Groups + Posture Checks per policy | `policy.add/update/delete` events |
| Logging & retention | Activity log (built-in) + Streamer (HTTP / Datadog / Elastic / templates) | Same activity events fanned out to operator's SIEM |
| Incident detection | Flow Events stack (real-time + cold archive) | Flow Events table + S3 archive |
| Incident response (revocation) | Device Admission worker (Phase 2) — closes session on compliance flip | `peer.admission.deny` paired with the prior `peer.added`/login event |
| Data-at-rest protection | Credentials encrypted via DataStoreEncryptionKey | n/a — verified by inspecting DB columns |
| Periodic review | Auditor CSV export (Phase 3) | `/api/events/admission.csv?from=…&to=…` |

## Detailed mapping

### Art. 3º, §1º, II — "verificação prévia de identidade" (pre-access identity verification)

The article requires the institution to verify the identity of every
endpoint that connects to systems handling client data **before**
access. openZro provides this in two layers:

1. **User identity** via the OIDC integration. Every dashboard login
   and every interactive client login is a federated SSO flow
   against the operator's IdP (Okta, Entra, Auth0, Zitadel,
   Keycloak, Authentik, JumpCloud, Google). JWT validation is in
   [`management/server/auth/jwt/validator.go`](../../management/server/auth/jwt/validator.go);
   it enforces issuer, audience, signature, and `iat` not-in-the-
   future. Device Admission's `EndpointSecurityCheck` answers the
   parallel "is the device itself in good standing?" question.

2. **Device identity** via WireGuard public key plus the operator-
   chosen Device Admission posture checks. The peer's WireGuard
   public key is the immutable device identifier; admission attaches
   a compliance check (Intune / SentinelOne / Huntress) to that
   identifier so a key whose device flunks compliance does not
   reach the mesh.

**Audit artefact:** every refused login produces a
`peer.admission.deny` event with the failing check, the reason, and
the peer's hostname. The Phase 3 CSV export turns the table into a
deliverable.

### Art. 3º, §1º, III — "controles de acesso baseados em segregação de funções"

Implemented by the Permissions / Roles surface plus per-account
scoping on every API endpoint. The SCIM 2.0 endpoint at `/scim/v2`
lets the IdP push role/group assignments, so segregation is
maintained from the operator's IdM source of truth without manual
sync. Activity events for role changes:
`user.role.update`, `user.group.add/delete`, `service.user.create`.

### Art. 3º, §1º, IV — "registro e acompanhamento de eventos"

Activity log captures every administrative action and every Device
Admission decision. Events live in `management/server/activity/store/`
(durable in the management's primary DB) and stream out via the
Activity Streamer to the operator's SIEM in real time. Two layers:

- **Process-wide env-var baseline** — `OPENZRO_ACTIVITY_EXPORT_*`
  configures HTTP webhook / Datadog / Elastic for the whole instance.
  Used by self-host operators with a single tenant.
- **Per-account DB-backed config** — `/api/admin/activity-exporters`
  + dashboard's Settings → Integrations → Activity Streamer. Each
  tenant streams into their own SIEM. Credentials encrypted at rest
  via `DataStoreEncryptionKey`.

Both layers support custom payload templates (Go text/template) so
the operator can match the SIEM's expected schema without standing
up Vector or Fluent Bit in the middle.

### Art. 3º, §1º, V — "criptografia de dados em trânsito e em repouso"

- **In-transit:** Every link is WireGuard (mesh data plane), gRPC
  over TLS (control plane), HTTPS (dashboard + API).
- **At rest:** credentials for outbound integrations (MDM/EDR
  vendors, SIEM endpoints, S3 archives) live in `ConfigCipher`
  columns encrypted with AES-256-GCM under the management's
  `DataStoreEncryptionKey`. See [`management/server/flow_exports/crypt.go`](../../management/server/flow_exports/crypt.go);
  the same envelope is reused by the MDM and Activity Streamer
  packages.

### Art. 3º, §1º, VI — "registro e tratamento de incidentes"

Two evidence streams feed incident response:

- **Activity log** answers "who did what and when": admin actions,
  posture-check changes, admission denials, account-setting flips.
- **Flow Events** answers "what traffic went where": per-connection
  records (src/dst IP, port, protocol, bytes, drops, rule ID) ingested
  by `FlowService.Events`. Stored in the hot tier (queryable via the
  dashboard's Network Traffic tab) and optionally archived to S3 /
  GCS / R2 / B2 / MinIO via Flow Exports for long-term retention.
  See [ADR-0002](../adr/0002-flow-events-storage.md) for retention
  decisions.

### Art. 3º, §1º, VII — "rotinas para detecção de vulnerabilidades"

openZro itself ships with `make` targets that run `golangci-lint`,
`govulncheck` (via `make vuln` if the operator wires it), and
`npm audit` on the dashboard. The project tracks open advisories in
[`docs/security/advisories.md`](../security/advisories.md) — every
CVE that touches our dependency tree is triaged, assigned a status,
and either Fixed (with a commit reference), Not applicable (with the
reasoning), or Open (with a tracking note).

### Art. 14 — "manutenção de dados por no mínimo 5 (cinco) anos"

The Activity log's primary store is whichever DB the management is
configured against (`OPENZRO_STORE_ENGINE`); the operator is
responsible for backup retention. The recommended posture for Bacen
tenants:

1. Stream Activity events to the operator's long-term SIEM via the
   Activity Streamer (Datadog Logs, Elastic with ILM policy, or HTTP
   webhook into a custom archiver).
2. Use the Phase 3 CSV export quarterly as a portable artefact.
3. For Flow events, configure both the hot tier (for investigation)
   and an S3-compatible cold archive (NDJSON+gzip, ~5 years
   recommended retention on Glacier / Coldline).

### Circular 3.909, Art. 6º (cont.) — "controles para acesso remoto"

Remote-access scenarios in fintechs are exactly what openZro's mesh
is designed for. Specific controls the article lists:

- **Multi-factor authentication.** Delegated to the IdP via OIDC.
  Every IdP we integrate supports MFA enforcement on the IdP side.
- **Device verification.** Device Admission with MDM/EDR posture
  checks satisfies this — the device must be in good standing per
  the corporate MDM **and** registered in the IdP.
- **Least-privilege access.** Policies + Groups + per-policy posture
  checks compose to per-application network segmentation.
- **Session monitoring.** Flow Events provides per-connection
  monitoring; the Activity log provides per-admin-action monitoring.
- **Anomaly detection.** Out of openZro's scope; the operator runs
  this in their SIEM with the Flow + Activity streams as inputs.

## Auditor walkthrough — what to show

When the auditor asks "show me how non-compliant devices are blocked":

1. Open Settings → Device Admission. Show the **Enforce admission**
   toggle is on and the posture checks list is populated.
2. Open Settings → Posture Checks. Click into the Endpoint Security
   check that points at the corporate Intune. Show the provider in
   Settings → Integrations → MDM/EDR.
3. Open Activity. Filter by `peer.admission.deny`. Walk through a
   recent denial — the row shows the peer hostname, the failing
   check, the reason verbatim from the vendor.
4. From Settings → Device Admission, click **Audit CSV**. The
   downloaded file is what the auditor takes home.

When the auditor asks "show me how a compromised endpoint loses
access":

1. Walk through the Phase 2 worker's interval (default 60s) and the
   MDM cache TTL (5 min) — total revocation budget ~6 min.
2. Show a `peer.admission.deny` event paired with the close timestamp
   on the prior session in the Activity log.
3. Show that the peer is back in `peer.admission.deny` on each
   subsequent retry until the device is fixed at the vendor.
