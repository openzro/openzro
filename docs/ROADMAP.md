# openZro Roadmap

Snapshot taken 2026-05-05. Items are ordered by priority within each
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

### ADR-0014 chaos validation in lab — 🟢

The multi-pod relay fabric ([ADR-0014](adr/0014-coordinated-multi-pod-relay.md))
landed in alpha.38 with full implementation, HMAC inter-pod auth, and
metrics. **Phase 7 of the ADR (chaos test) hasn't run yet.** Pre-prod
validation needs:

- Pod kill mid-stream → assert peer reconnects sub-second, locator
  caches recover via re-broadcast within 5 min cache TTL
- Scale-up `replicaCount` 2 → 5 → assert new pods join the fabric
  on next discovery tick (≤ 10 s) and start receiving FWD frames
- Network partition between pod-pair → assert HELLO replay window
  catches stale streams; `relay_cluster_hello_rejects_total{reason="stale_timestamp"}`
  bumps; recovery on heal

Run on the lab cluster; fail conditions block the move from alpha → beta
on the chart. ~1 day of work.

### Inter-pod keepalive (PING/PONG loop) — 🟢

Frame types `MsgPing` (`0x20`) and `MsgPong` (`0x21`) are defined in
[`relay/server/cluster/frame.go`](../relay/server/cluster/frame.go)
but no goroutine emits them today. Long-lived inter-pod TCP streams
behind a stateful firewall can go zombie until the next write fails
— a 30 s keepalive loop on the transport closes that gap and lets us
detect partitions earlier than the kernel's 2h TCP keepalive default.

Wires into the existing `relay_cluster_pings_{sent,lost}_total`
metrics (already declared, no consumers yet). ~2 hours of work.

---

## P2 — Coverage / scale

### Ansible `openzro_routing_peer` role — 🟢

The K8s path for routing peers / exit nodes is solved via the
operator's `OZRoutingPeer` CRD. Bare-metal / VM operators have no
equivalent — they install the binary by hand and `openzro up`
manually. Mirror the existing `openzro_management` /
`openzro_signal` / `openzro_relay` roles in
[openzro/openzro-ansible](https://github.com/openzro/openzro-ansible)
with a fourth role:

- Per-host idempotent enrollment via setup key (Ansible Vault var)
- Marker file (`/var/lib/openzro/.enrolled`) to skip re-enroll on
  re-runs
- systemd unit + drop-in for routes
- Per-component playbooks (`relay-only.yml`, `peer-gateway-only.yml`,
  `control-plane.yml`, `full.yml`) so operators can pick what to
  install
- Pre-flight check that management API is reachable before
  attempting enrollment

Pair with chart docs (already updated) that point operators at the
operator path for K8s and Ansible for bare-metal. ~1 day of work.

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

### Native OPNsense / pfSense plugins — 🟡

Operators today install the upstream NetBird package
(`os-netbird` from Deciso for OPNsense; `pfSense-pkg-NetBird` from
Netgate for pfSense) and point the **Management URL** at their
openZro server. The wire protocol is stable enough that this works
unmodified.

A native `os-openzro` / `pfSense-pkg-openZro` would mean:

- Branding correct in the firewall UI (no NetBird label).
- Default Management URL pointing at openZro out of the box.
- Source pinned to openZro's release cadence, not NetBird's.

The forks are legally clean — both repos are permissively licensed
([netbirdio/OPNsensePlugins](https://github.com/netbirdio/OPNsensePlugins/tree/Netbird-devel) is BSD-2-Clause, the upstream OPNsense plugins fork;
[netbirdio/pfsense-netbird](https://github.com/netbirdio/pfsense-netbird) is Apache 2.0, by Netgate).
The catch is distribution:

- **OPNsense plugin manager**: Deciso curates packages and is unlikely
  to accept a duplicate `os-openzro` while `os-netbird` exists.
  Realistic path is a third-party Git repo for manual install.
- **pfSense .pkg**: standalone `pkg add` flow — autonomous, no
  vendor approval. Easier to ship.

**Deferred until either:**
- The protocol drifts and the upstream NetBird package no longer
  talks to openZro (then we *have* to ship our own), or
- A pilot customer asks for native branding on their firewall.

The docs intentionally instruct operators to use the upstream
package today — see [pfSense](../docs/src/pages/get-started/install/pfsense.mdx)
and [OPNsense](../docs/src/pages/get-started/install/opnsense.mdx).

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

## Possibilities (not roadmap — open ideas)

Different from P1/P2/P3 above: these aren't committed work. They're
ideas that came up during design, where the cost/benefit isn't yet
clear and we don't have the trigger condition that would justify
sequencing them. We keep them written down so we don't lose the
analysis when someone asks "have you considered X?".

Picking one of these up means:
1. Naming a concrete trigger condition that's now been met (a
   pilot customer with a specific need, a regulatory requirement,
   a security advisory, etc.).
2. Writing an ADR with the design and the cost.
3. Then promoting it to P2 or P3 in this document.

### Browser Client (in-browser SSH / RDP via WASM)

NetBird's upstream ships a "Browser Client" — a full WireGuard peer
compiled to WebAssembly that runs in the user's browser tab and
provides SSH and RDP without installing anything locally. We
intentionally removed the docs page for it
([commit](https://github.com/openzro/docs/commit/0af05bb)) because
the feature doesn't exist in the openZro fork.

**To rebuild it ourselves we'd need:**

- WireGuard userspace in Go compiled to WASM
- gRPC-Web (or WebSocket) bridge in the management server, since
  browsers can't speak HTTP/2 binary natively
- WebRTC DataChannel for the actual peer-to-peer transport
  (browsers can't open raw UDP)
- Mandatory TURN server for the cases where DataChannel can't
  hole-punch
- Short-lived browser session tokens distinct from PATs
- xterm.js + an SSH protocol implementation, plus an in-browser
  RDP client (Guacamole-style or homegrown — RDP-in-browser is
  notoriously hard)

**Honest budget:** ~3–4 eng-months for one engineer to reach
upstream parity, plus continuous maintenance every time Go's WASM
runtime, the browser WebRTC SDK, or browser security policies
change.

**Why we'd defer:** the typical "I want SSH without installing"
case is already covered by the openZro CLI on every desktop OS
plus a normal `ssh` into the peer — the browser-native path
shaves install steps for one specific edge case at a very high
maintenance cost.

**Trigger conditions to revisit:**
- A pilot customer requirement that explicitly forbids installing
  software on the user's device.
- A self-host operator with the engineering capacity to maintain
  the WASM build offering to drive it as a contribution.

### Native pfSense / OPNsense plugins

See the corresponding entry under P3.

---

## Recently shipped (don't re-implement)

For context — these were on the gap list at one point and are now
done. Skip these when picking up tasks.

- **Coordinated multi-pod relay** ([ADR-0014](adr/0014-coordinated-multi-pod-relay.md))
  — landed in `v0.53.1-alpha.38`. Inter-pod TCP fabric, broadcast-on-miss
  peer locator with 5min cache, K8s Headless Service discovery,
  HMAC-SHA256 authenticated HELLO frames, full `relay_cluster_*`
  metric set. Chart auto-enables at `relay.replicaCount > 1`
  (chart `2.1.0-alpha.11`). Phase 7 chaos test pending — see P1.
- **Flow event policy correlation on Linux nftables**
  ([ADR-0013](adr/0013-flow-policy-correlation.md)) — peers using
  the kernel firewall now stamp the policy ID into ct mark via a
  bit-split layout; conntrack reader resolves it back to the
  PolicyID in flow events. Dashboard's traffic events list now
  shows "Policy default allowed the connection" instead of empty.
- **MaxMind GeoLite2 auto-update + license-key opt-in** —
  `--disable-geolite-update` default flipped to `false`, fresh
  installs populate the country/city posture-check dropdowns
  without operator action. New `--maxmind-license-key` for
  operators who prefer fetching directly from MaxMind instead of
  the openZro mirror.
- **Next.js 14.2.28 → 15.5.15+** (closes
  [GHSA-q4gf-8mx6-v5v3](https://github.com/advisories/GHSA-q4gf-8mx6-v5v3),
  [GHSA-h25m-26qc-wcjf](https://github.com/advisories/GHSA-h25m-26qc-wcjf),
  jumps over CVE-2025-55182 RCE window). Pulled in
  react-day-picker 9.x at the same time — Calendar.tsx rewritten
  for v9 API. See `dashboard/package.json` and the
  `fix(dashboard): audit page works on Next 15` commit.
- **Helm chart + K8s operator** ([ADR-0008](adr/0008-kubernetes-helm-operator.md)):
  `openzro-2.1.0-alpha.11` published at https://openzro.github.io/helms,
  operator image at `ghcr.io/openzro/openzro-operator:0.3.2-alpha.1`.
  Chart now ships HA modes (`cluster.mode: embedded|external|disabled`),
  multi-pod relay (ADR-0014), MaxMind license-key wiring, and
  postgres / mysql auto-wiring with restricted-grant provisioning Job.
- **Native installers** ([ADR-0007](adr/0007-client-packaging.md)
  Phase 1): unsigned Windows MSI (WiX 4) + macOS PKG (pkgbuild) +
  Homebrew tap on every tag push. Linux apt/yum repos at
  `pkg.openzro.io` populate from the same pipeline.
- **Embedded Dex IdP** ([ADR-0006](adr/0006-embed-dex.md)):
  Dashboard's Settings → Authentication Providers manages
  Google / GitHub / Microsoft / Keycloak / Okta / generic OIDC
  connectors at runtime via Dex's gRPC API. Dex shipped as a
  helm subchart in the openzro chart.
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
