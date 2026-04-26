# ADR-0001: openzro foundation

- **Status**: Accepted
- **Date**: 2026-04-25
- **Decision-makers**: openzro maintainers
- **Supersedes**: —
- **Superseded by**: —

## Context

In **August 2025**, the upstream project [netbirdio/netbird](https://github.com/netbirdio/netbird) — until then a fully BSD-3-Clause licensed WireGuard-based zero-trust overlay — relicensed three core components (`management/`, `signal/`, `relay/`) to **AGPLv3** starting with `v0.53.0`. The dashboard followed the same pattern shortly after.

The relicense was framed by upstream as "embracing AGPL", but the practical effect for self-hosted users is twofold:

1. The "core" pieces of any non-trivial deployment (management, signal, relay) are now AGPL — meaning any organization that integrates them or extends them and exposes the result over a network is forced to disclose modifications.
2. Several limits and behaviors that exist in the open-source code are *artificially* constrained, presumably to nudge users toward the upstream's commercial cloud offering. Examples found in `v0.52.2` (the last BSD release):
   - `channelBufferSize = 100` in [`management/server/updatechannel.go`](../../management/server/updatechannel.go) — a hardcoded 100-message-per-peer buffer with **silent drops**, with a `// todo shouldn't it be more? or configurable?` comment from the original author. In accounts with high churn, this causes peers above ~100 to silently miss updates.
   - No HA support in the **signal** server (single-instance, in-memory peer registry).
   - No distributed lock / pub-sub coordination in **management**, despite it already supporting external Postgres/MySQL backends since well before the relicense.
   - Peer approval and several enterprise-grade features fenced off behind "Cloud" tiers in upstream marketing, even though the code paths exist in the BSD baseline.

A self-hosted, BSD-licensed, community-maintained continuation is needed for users who want full zero-trust networking without (a) AGPL contamination of internal/SaaS deployments and (b) artificial self-host limits.

## Decision

We are forking [netbirdio/netbird](https://github.com/netbirdio/netbird) and [netbirdio/dashboard](https://github.com/netbirdio/dashboard) at their **last BSD-3-Clause tags** and continuing development under the name **openzro**.

### 1. Fork point and license posture

| Component        | Upstream                          | Tag     | Date       | License at this ref |
|------------------|-----------------------------------|---------|------------|---------------------|
| Go core          | `netbirdio/netbird`               | v0.52.2 | 2025-07-30 | BSD-3-Clause (whole tree) |
| Web dashboard    | `netbirdio/dashboard`             | v2.15.0 | 2025-07-30 | BSD-3-Clause |

- **License**: openzro stays BSD-3-Clause **everywhere, forever**. Upstream `LICENSE` and `AUTHORS` files (root and `dashboard/`) are preserved verbatim per BSD-3 attribution clause; we add to `AUTHORS` rather than modifying it.
- **No AGPL ingestion**: post-`v0.52.2` upstream commits to `management/`, `signal/`, `relay/` (or any directory now AGPL upstream) are **never** cherry-picked or copied. Cross-pollination requires clean-room re-implementation from public descriptions only.
- **No CLA**: deleted `CONTRIBUTOR_LICENSE_AGREEMENT.md` (a NetBird GmbH–specific instrument under German law). openzro accepts contributions under the inbound-equals-outbound BSD-3 rule.

### 2. Vision: full-featured zero-trust, no artificial limits

openzro must be a **drop-in self-hostable zero-trust overlay** with feature parity to (and surpassing) upstream's free tier. Specifically:

- **No paywalls**, no hidden flags, no "Cloud-only" features — anything that exists in any open-source Netbird release at the BSD baseline is expected to also exist and be unconstrained in openzro.
- **No artificial limits**. The 100-message buffer is symptomatic; there will be a code audit to find and remove similar caps.
- **HA self-hostable**: signal and management must scale horizontally via standard primitives (Redis for ephemeral state and pub/sub; Postgres for persistent state).

### 3. Technical strategy

#### 3.1 Branding and rename
- Go module: `github.com/netbirdio/netbird` → `github.com/openzro/openzro`
- Binaries: `netbird-*` → `openzro-*`
- Env vars: `NB_*`, `NETBIRD_*` → `OZ_*`, `OPENZRO_*`
- Config paths: `/etc/netbird`, `~/.config/netbird`, `/var/lib/netbird` → `/etc/openzro`, etc. (no compat shim — this is a clean fork, not a drop-in replacement)
- Domains: `netbird.io` → `openzro.io`
- Brand casing: `openzro` (lowercase identifier), `Openzro` (Title case for prose/UI)
- Upstream `LICENSE` and `AUTHORS` are **not** rewritten — they retain "Copyright (c) 2022 NetBird GmbH & AUTHORS" verbatim as required by BSD-3.

#### 3.2 Repository layout
Single monorepo at `github.com/openzro/openzro`:
```
openzro/
├── docs/adr/        ADRs (this file lives here)
├── docs/FORK.md     Fork point provenance
├── client/  management/  signal/  relay/  ...   Go core
└── dashboard/       Next.js web UI (no separate .git)
```
The dashboard's independent upstream Git history is *not* preserved in this repo — it exists at `netbirdio/dashboard` if needed for archaeology.

#### 3.3 Clean-room reimplementation policy
For any change motivated by upstream activity *after* the fork point (security fixes, HA designs, performance work, etc.):

1. **OK to read**: CVE descriptions, GHSA advisories, CWE classifications, public blog posts, ZITADEL/Postgres/Redis upstream documentation, names of vulnerable functions and line numbers, descriptive prose about the bug or design.
2. **NOT OK to read**: the actual diff, PR, or commit content of the upstream fix or feature implementation in any AGPL code path.
3. **Implementation must be original** — written from the description of the problem, not from translation of upstream code.
4. **Each commit must explicitly state** which public sources informed it and that no AGPL code was consulted (see [commit `0f956e72`](https://github.com/openzro/openzro/commit/0f956e72) for the template).

This is the single non-negotiable engineering rule of this project, because violating it would force a relicense to AGPL across the entire repository.

#### 3.4 HA architecture (clean-room, planned)

**Signal (ephemeral, hot path)** — currently a single in-memory peer registry. Plan:
- Redis `SET`/`HSET` registry keyed by peer id, with a per-instance pub/sub channel. When a peer connects to instance A and a message arrives at instance B for that peer, B publishes on A's channel and A forwards over the open gRPC stream.
- TTL-based cleanup for stale entries; instance heartbeat into Redis so dead instances are pruned.
- Designed entirely from the public description in the [`nik-dev-ops/netbird-ha`](https://github.com/nik-dev-ops/netbird-ha) README (which itself is AGPL-tainted code we did **not** read) and from generic Redis pub/sub patterns. Concrete implementation is openzro's own.

**Management (persistent + coordinated)** — already has multi-backend store (`SqliteStoreEngine`, `PostgresStoreEngine`, `MysqlStoreEngine`) per [`management/server/store/store.go:206`](../../management/server/store/store.go) at the BSD baseline. Plan:
- Run multiple management instances pointed at a shared Postgres via `OPENZRO_STORE_ENGINE=postgres`.
- Add Redis-backed distributed lock (`SET NX EX`) wrapping operations that today rely on in-process mutexes.
- Add Redis pub/sub for cross-instance cache invalidation when account state changes locally on instance A.
- The client is unaware of which instance it talks to.

#### 3.5 Security backports

Public advisories filed against upstream after our fork point that affect code we inherit are reimplemented clean-room. Initial set (in priority order):

| ID                          | Severity | Status |
|-----------------------------|----------|--------|
| CVE-2025-10678              | High     | Fixed in `0f956e72` |
| Mgmt API Authorization Bypass | High   | Pending |
| GHSA-rxmp-8h9v-56cx (race UpdateUser → priv esc) | Moderate | Pending |
| CVE-2025-55182 (React RSC, dashboard) | Critical | Pending |

#### 3.6 Removing artificial caps

First targets (more will be found by audit):
- [`management/server/updatechannel.go`](../../management/server/updatechannel.go): `channelBufferSize = 100` → make configurable via `OPENZRO_PEER_UPDATE_CHANNEL_BUFFER_SIZE`, raise default to `10000`, and convert silent `default:`-drop to a metric-tracked, log-loud drop.
- Audit for: account-level peer count caps, group size caps, per-account user caps, hardcoded rate limits without env override, hardcoded timeouts that masquerade as security but are really pricing levers.

## Consequences

### Positive
- **Self-hosted users get a usable, BSD-licensed zero-trust stack** without AGPL obligations.
- Forks/SaaS built on openzro can stay closed-source if they want to (BSD allows it) — the ecosystem is broader than what AGPL allows.
- Clean-room policy gives us a defensible legal posture for backporting fixes from upstream.
- HA story exists from day one of the roadmap rather than being an enterprise-only feature.

### Negative / risks
- **Permanent maintenance burden** — security advisories and architectural improvements upstream must be tracked and re-implemented manually, not cherry-picked.
- **No upstream goodwill** — we do not expect cooperation from NetBird GmbH, and that is fine; everything we do is from public sources.
- **Brand confusion** — users coming from netbird need to know openzro is a fork, not a rebadge or a malicious clone. `docs/FORK.md` and this ADR exist precisely to make provenance explicit.
- **Compatibility break** — config paths, env vars, and binary names are renamed without a compat shim. Migrating an existing netbird install requires reconfiguration. This is intentional: a clean break makes the fork's identity unambiguous and avoids accidental data crossover.

### Neutral
- The dashboard's per-component upstream Git history is dropped (single-repo squash); the URL `netbirdio/dashboard` is documented as the historical source.

## References
- [docs/FORK.md](../FORK.md) — fork-point provenance details
- Upstream relicense announcement: <https://netbird.io/knowledge-hub/netbird-agpl-announcement>
- Last BSD release: <https://github.com/netbirdio/netbird/releases/tag/v0.52.2>
- ZITADEL upstream defaults (used for clean-room CVE-2025-10678 fix): <https://github.com/zitadel/zitadel/blob/main/cmd/defaults.yaml>
