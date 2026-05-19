# ADR-0020 — openZro SSH: identity-based access via a dedicated policy protocol

## Status

**Proposed**. Phase 0 of [openzro/openzro#75](https://github.com/openzro/openzro/issues/75).
This ADR settles the gating decisions (D1–D6) so the phased
implementation can start. **No code lands with this ADR** — it is the
prerequisite gate the issue mandates.

**Clean-room mandate.** `management/`, `signal/`, `relay/`, `combined/`
are AGPL; the upstream `netbirdio/netbird` SSH rewrite lives there. This
ADR and every implementation phase reimplement from **public sources
only** — the public NetBird docs (the openZro page's `Source:` line),
openZro's own legacy SSH code, and the OIDC/JWT/SSH specifications.
No upstream AGPL diff is consulted or ported; each commit cites its
public sources and confirms this. `client/` is BSD-3.

## Context

`docs.openzro.io/manage/peers/ssh` is a NetBird-forked page
(`Source: https://docs.netbird.io/manage/peers/ssh`, string-substituted)
that documents a feature openZro **partially** has. A code study (logged
in [openzro/docs#7](https://github.com/openzro/docs/issues/7)) found two
distinct realities:

**openZro implements the LEGACY NetBird SSH model — fully, end to end:**

- Embedded SSH server in every client: `client/ssh/server.go`, fixed
  port **44338**, bound to the WireGuard interface IP
  (`client/internal/engine.go`); the *server* does not run on Windows.
- Per-peer `SSHEnabled` toggle: proto `SSHConfig{sshEnabled, sshPubKey}`,
  `PeerKeys.sshPubKey`; dashboard `Peer.ts ssh_enabled` +
  `PeerActionCell.toggleSSH`; REST `SshEnabled`; audit `PeerSSHEnabled`.
- **Machine-identity auth**: each peer's SSH pubkey is distributed to
  the peers in its networkMap via `RemotePeerConfig.sshConfig.sshPubKey`;
  the server authorizes by a `WG-pubkey → SSH-pubkey` map
  (`authorizedKeys`).
- Reachability is governed by the **ordinary access-control policy**
  (networkMap + firewall rules). There is no SSH-specific protocol.

**openZro does NOT implement the NetBird 0.60/0.61 rewrite** the page
actually describes:

- Policy `Protocol` is strictly `all | tcp | udp | icmp` (Go
  `management/server` + `dashboard/src/interfaces/Policy.ts` +
  OpenAPI). No `ssh` value, no `AuthorizedGroups`.
- No JWT/OIDC SSH identity; no user→OS-user mapping; port not moved to
  22↔22022. The page cites `v0.60.0`/`v0.61.0`, versions that do not
  exist in openZro (current `v0.53.1-alpha.75`).

This is roadmap-scale and security-sensitive: a new policy dimension
(AGPL), a Sync-distribution change (AGPL), a JWT/OIDC SSH auth path with
privilege-drop (BSD client), a dashboard authz editor, and a migration
story. It must not be re-derived from upstream AGPL code, and the
client auth path must be **fail-closed** (a permissive bug grants shell
access to arbitrary OS users).

## Decision

### D1 — Adopt identity-based SSH as a dedicated policy protocol

Introduce an `ssh` protocol value on access-control policies. An
`ssh`-protocol policy expresses "these source groups may SSH into these
destinations **as these local OS users**", enforced by user identity,
not machine key. Network-level SSH (TCP/22 or the legacy embedded
server) remains expressible with the existing `tcp`/`all` protocols.

### D2 — JWT source is the existing Dex/OIDC + management JWKS

Reuse openZro's existing OIDC stack (local-users-via-Dex; the
management JWKS endpoint). No new IdP integration. The client mints a
JWT via the OIDC flow; the SSH server validates it against the
management-published JWKS. This is the single largest cost-reducer —
the validation infrastructure already exists.

### D3 — Additive coexistence, not a silent cutover

The legacy machine-key model (per-peer `SSHEnabled` + ACL) keeps
working. `ssh`-protocol policies are **opt-in and additive**. A hard
cutover (the upstream approach, per its public docs, was explicitly
"not backward compatible") is rejected for v1: it would break every
existing openZro SSH user on a management upgrade. A future deprecation
of the legacy path, if desired, is a separate decision (Phase 6) gated
by its own ADR amendment.

### D4 — Authorized-Groups model: Full vs Limited, fail-closed

An `ssh`-protocol policy carries, per source group, either **Full
Access** (any local OS user on the destination) or **Limited Access**
(an explicit allow-list of local OS users). No mapping ⇒ **deny**.
Authorization is decided on the destination peer from the validated
JWT identity; all attempts are audit-logged with the resolved user.

### D5 — Keep port 44338 for v1; defer the 22↔22022 interception

v1 runs identity SSH over the existing embedded-server port mechanics.
The transparent OpenSSH integration (intercepting standard port 22,
22022 redirect, drop-in `99-openzro.conf`, auto-ACL for 22022) is the
**riskiest and most platform-divergent** piece and is **deferred to
Phase 5 under its own ADR**. v1 connects via the `openzro ssh` client.

### D6 — Privilege model

User-switching to the mapped OS user requires the SSH server to run
with root/administrator privileges. Rootless installs run sessions as
the openZro process account only (documented limitation, fail-closed —
never silently escalate). Windows server stays out of scope (the
legacy server already does not run there).

### Phase plan (Phase 0 is this ADR)

- **Phase 1** — policy model: `ssh` protocol value + Authorized-Groups
  structure + DB migration + OpenAPI + validation. *AGPL clean-room.*
- **Phase 2** — distribution: extend the Sync proto / firewall-rule so
  the destination client learns the per-policy user mapping.
  *AGPL clean-room.*
- **Phase 3** — client auth (BSD): SSH server validates the JWT vs
  JWKS, resolves identity → allowed OS user, privilege-drops
  fail-closed; `openzro ssh` mints/caches the JWT via OIDC.
- **Phase 4** — dashboard: `ssh` in the protocol selector + the
  Authorized-Groups editor (Full/Limited, per-group OS-user list).
- **Phase 5** *(deferred, own ADR)* — port 22↔22022 interception +
  transparent native-OpenSSH integration.
- **Phase 6** *(optional, own ADR amendment)* — legacy machine-key
  deprecation.

### Out of scope

Windows SSH **server**; the Phase-5 transparent-OpenSSH/port-22 work;
non-OIDC identity sources; SFTP/port-forwarding policy semantics beyond
what the legacy server already offers.

## Rationale

- **Why identity-based:** machine-key SSH cannot answer "which *user*
  reached this host" — no per-user authz, no audit trail, no
  least-privilege OS-user mapping. Identity SSH is the actual value of
  the documented feature.
- **Why reuse Dex/JWKS (D2):** openZro already ships the OIDC + JWKS
  infrastructure; greenfielding an IdP path would dominate the cost for
  no benefit.
- **Why additive (D3):** the legacy model is in active use; a silent
  cutover trades a documented gap for a production outage. Additive
  opt-in lets operators migrate deliberately.
- **Why defer port-22 interception (D5):** it is platform-specific
  (Linux/macOS/Windows diverge; Windows server unsupported), touches
  connection interception, and is independently complex — bundling it
  into v1 would gate the whole feature on its riskiest part.
- **Why ADR-first:** spans AGPL + BSD + dashboard + migration; the
  license boundary and the fail-closed auth contract must be settled
  before code, per the engineering rules.

## Trade-offs

### What we accept

- A multi-week, multi-phase initiative (comparable to Control Center
  v2 / ADR-0017), not a slip-in.
- Clean-room overhead: reconstruct from public docs/specs + our own
  legacy code, with per-commit provenance attestation.
- Security-sensitive client code (JWT validation, OS-user
  privilege-drop) carrying a hard fail-closed requirement and tests.
- Two SSH models coexisting until (if ever) Phase 6 deprecates legacy.

### What we don't accept (rejected alternatives)

- **Consulting the upstream AGPL implementation** — contaminates the
  BSD-3 tree (ADR-0001 §3.1). Non-negotiable.
- **Silent hard cutover** — breaks existing SSH users on upgrade.
- **Frontend/client re-derivation of authorization** — the destination
  peer is the only correct enforcement point; the dashboard only
  *edits* the mapping.
- **Bundling Phase 5 (port-22 interception) into v1** — gates the
  feature on its riskiest, most divergent component.
- **A new IdP path** — duplicates existing Dex/OIDC/JWKS.

## Open questions

- JWT cache TTL: default value and whether caching is opt-in
  (client-side, per the documented `--ssh-jwt-cache-ttl` shape).
- Should machine-key auth remain available as an explicit fallback
  mode (a `--disable-ssh-auth` analog) for automation/service accounts?
- Interaction semantics when both a legacy ACL (TCP/22) and an
  `ssh`-protocol policy select the same source→destination pair.
- Rootless UX: hard-deny user-switching, or run-as-process-account with
  a loud warning? (Leaning hard-deny + clear error — fail-closed.)
- Group→OS-user mapping storage shape and its migration/rollback.

## References

- [openzro/docs#7](https://github.com/openzro/docs/issues/7) — the
  forked-doc drift that surfaced this (legacy-vs-rewrite framing).
- Public NetBird docs (the openZro page's `Source:` line) — used for
  the *feature concept only*, not implementation.
- openZro code studied (legacy model): `client/ssh/server.go`,
  `client/internal/engine.go`, `management/proto/management.proto`
  (`SSHConfig`, `PeerKeys`), `management/server/grpcserver.go` /
  `peer.go`, `dashboard/src/interfaces/Policy.ts` & `Peer.ts`.
- OIDC Core, RFC 7519 (JWT), RFC 7517 (JWK/JWKS), RFC 4252 (SSH auth).
- Precedents: [ADR-0001](0001-openzro-foundation.md) §3.1 (license
  posture), [ADR-0010](0010-unidirectional-all-policies.md) (a prior
  policy-protocol-shape decision), [ADR-0017](0017-control-center-access-graph.md)
  (clean-room, ADR-first, phased-roadmap precedent).

## Amendments

_None yet._
