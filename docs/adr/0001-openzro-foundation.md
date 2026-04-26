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
- Brand casing: `openzro` (lowercase identifier), `openZro` (Title case for prose/UI)
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

#### 3.4 HA architecture

HA is opt-in. Single-instance deployments need no broker and no extra
configuration. Multi-instance deployments require **one** of three
coordination backends, and the same backend serves both signal and
management — operators run **one** stateful coordination piece at most,
not separate ones for each component.

**Supported coordination backends:**

| Backend | Pub/sub mechanism | Locks | Operates as |
|---|---|---|---|
| **Redis-compatible** (Redis, Valkey, Dragonfly) | Redis pub/sub | `SET NX EX` | external service |
| **NATS** (external) | NATS subjects | NATS KV / DB advisory locks | external service |
| **NATS** (embedded in openzro) | NATS subjects | NATS KV / DB advisory locks | in-process; no external infra |

Recommendation order, in line with the openzro license posture:

1. **Postgres + Valkey** — Valkey is the BSD-3-licensed continuation of
   the last freely-licensed Redis. Same license family as openzro, same
   philosophy, and our code uses only standard RESP2 commands so the
   client speaks Valkey, Dragonfly, and Redis identically.
2. **Postgres + embedded NATS** — zero infrastructure outside the
   openzro binaries themselves. Each openzro instance starts an
   in-process NATS server; instances find each other through a static
   peer list (`OPENZRO_CLUSTER_PEERS`).
3. **Postgres + external NATS** — appropriate when the deployment
   already runs NATS for other workloads.
4. Postgres + upstream Redis (works, but Redis is no longer OSI-licensed
   since 2024 — operators may prefer Valkey).
5. Postgres + Dragonfly (works; BSL→Apache license, performance focus).

**Why broker-mandatory for HA?** A "Postgres-only" HA path was
considered (advisory locks + LISTEN/NOTIFY) and rejected because:

- LISTEN/NOTIFY is Postgres-specific; MySQL has no native pub/sub
  equivalent. A polling-based fallback is several hundred ms slower per
  hop and constantly loads the database. The combination would push the
  most painful ops experience as the path-of-least-resistance.
- In practice, deployments large enough to want HA already run a broker
  for other reasons. The marginal operational cost of formalizing one
  is small. The cost of maintaining a polling-based fallback that
  performs poorly is large.
- SQLite is fundamentally incompatible with multi-instance writes;
  HA + SQLite is not a coherent configuration. SQLite users stay
  single-instance, no broker required.

**Signal (ephemeral, hot path).** The fork point ships an in-memory
peer registry; cross-instance forwarding does not exist. The
implemented design (see `signal/dispatcher/`):

- `dispatcher.Dispatcher` interface with three implementations:
  `inmem`, `redis`, `nats` (the last serves both external and
  embedded NATS — they differ only in the URL).
- The Redis backend uses an explicit registry
  (`oz:signal:peer:<peerID> = <instanceID>`, TTL-renewed by a
  per-peer heartbeat) plus per-instance pub/sub channels.
- The NATS backend sidesteps registry entirely: subscribing to
  `oz.signal.peer.<peerID>` IS the registration. SendMessage
  publishes to the same subject; whichever instance holds the
  subscription receives. Cleanup happens automatically when the
  subscription drops.
- Both backends do a local fast path: if the destination peer is
  registered on this instance, the local handler is invoked
  synchronously without any broker round-trip.

**Management (persistent + coordinated).** The fork point already
ships multi-backend store
(`SqliteStoreEngine`, `PostgresStoreEngine`, `MysqlStoreEngine`) per
[`management/server/store/store.go:206`](../../management/server/store/store.go).
Plan (next session):

- A `cluster.Coordinator` interface with `redis` and `nats`
  implementations, mirroring the signal dispatcher layout.
- Replace in-process `sync.Mutex` per account with distributed locks
  scoped by `accountID`. SQL-engine `pg_advisory_lock`/`GET_LOCK` is
  available as an additional fallback when the chosen broker doesn't
  support locks (e.g. plain NATS without JetStream KV).
- Cross-instance cache invalidation via the chosen backend's pub/sub.
- The client is unaware of which instance it talks to; load balancing
  can be naive (round-robin or random) once HA is enabled.

**Provenance note.** The high-level approach (per-peer registry,
per-instance pub/sub channel, TTL-based liveness, local fast path)
is a recurring pattern in distributed-server design — not borrowed
code. A separate AGPL-licensed third-party fork
([`nik-dev-ops/netbird-ha`](https://github.com/nik-dev-ops/netbird-ha))
implements a similar shape; its README was read for conceptual
confirmation but none of its code was consulted, and our package
layout (`signal/dispatcher/{redis,nats,inmem}` and
`cluster/embedded/`) is unrelated to its `management/server/distributed/`
tree.

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
