# ADR-0003: Device admission gate (Phase 1 + 2 + 3)

- **Status**: Accepted
- **Date**: 2026-04-26
- **Decision-makers**: openZro maintainers
- **Supersedes**: —
- **Superseded by**: —

## Context

openZro inherits from NetBird a posture-check system where the
operator defines reusable rules (OS version, openZro version, geo
location, network range, process running, MDM/EDR compliance) and
attaches them to **policies** as `policy.SourcePostureChecks`.
Failing the check filters the peer out of the source group of that
policy — the peer keeps its session, keeps its IP, and is invisible
in the dashboard as "denied"; it just does not show up in its
intended peers' NetworkMap.

That model fits an environment where the network is the perimeter and
posture is one of several signals that shape ACLs. It does **not**
fit a regulated tenant (Bacen Resolução 4.893 / Circular 3.909 for
fintechs and banks; equivalents under DORA, NYDFS Part 500, PCI-DSS
v4.0 §8) where the controls require:

- *Provable* refusal of non-compliant endpoints at the control plane.
- An auditable trail with timestamps and reasons.
- The ability to **revoke** an active session when compliance
  changes.

A traffic-only filter does not produce evidence an auditor recognises
as "the device was denied access." The peer registered, took an IP,
and counted toward the head count — only its packets were quiet.

## Decision

Add an **account-wide admission gate** on top of the existing
per-policy posture machinery. Three phases land in sequence; each
is opt-in via a single account setting.

### Phase 1 — gate at the control plane

A new `Settings.AdmissionEnforcementEnabled` toggle plus
`Settings.AdmissionPostureChecks []string` list of posture-check IDs.
When the toggle is on, every `Login`, `AddPeer`, and `SyncPeer` call
on the gRPC ManagementService runs the listed checks against the
peer's freshly-reported `Meta`. The first failing check yields a
structured `AdmissionDenial{PostureCheckID, Name, CheckType, Reason}`
returned as `codes.PermissionDenied: "device admission denied: <type>: <reason>"`.

Implementation lives in [`management/server/types/account.go`](../../management/server/types/account.go)
(free function `EvaluateAdmission` so callers feed it whatever
scope they have — usually a transaction-scoped settings load plus
`GetPostureChecksByIDs` lookup). The per-call wrapper in
[`management/server/peer_admission.go`](../../management/server/peer_admission.go)
emits `activity.PeerAdmissionDenied` events (initiator, target,
posture-check ID + name, check type, reason, peer hostname) before
returning the gRPC error. Toggle flips and admission-list edits emit
their own audit events (`AdmissionEnforcementEnabled/Disabled`,
`AdmissionPostureChecksUpdated`) so changes to the policy itself
leave a paper trail.

### Phase 2 — active revalidation worker

The Phase 1 gate fires only when the client opens a fresh gRPC Sync
stream. After that the stream stays open indefinitely; a peer whose
Intune compliance flips post-connect would otherwise keep its
session forever.

A goroutine started from `BuildManager` iterates the locally
connected peers every `OPENZRO_ADMISSION_REVALIDATE_INTERVAL_SECONDS`
seconds (default 60s, 10s floor to protect vendor APIs from a
stampede, 0 disables) and re-runs the same evaluator. On denial the
peer's update channel is closed, which cleanly terminates the gRPC
stream; the client backs off and retries Login, the Phase 1 gate
refuses re-entry. End-to-end revocation latency:

```
worst_case = revalidate_interval (60s)
           + mdm_cache_ttl (5min)
           + client_backoff (~30s)
           ≈ ~6 min
```

HA-aware by construction: `peersUpdateManager.GetAllConnectedPeers`
is local-only, so each management instance handles its own connected
peers without coordination.

### Phase 3 — auditor CSV export

`GET /api/events/admission.csv?from=…&to=…` (RFC3339) streams the
admission slice of the activity log as CSV with stable columns:
timestamp, activity_code, activity, initiator_id/name/email,
target_id, posture_check_id/name, check_type, reason, peer_hostname.
Filters in-memory because `GetEvents` already caps at 10k events;
denials are rare events by design and inflating `activity.Store`
with another query method for one use case is not worth it.

The dashboard's Settings → Device Admission tab carries an "Audit
CSV" button that fetches the file with the user's bearer token and
triggers a date-stamped download — the artefact operators hand the
auditor for the quarterly evidence package.

## Why a separate concept from `policy.SourcePostureChecks`

We considered overloading the existing `policy.SourcePostureChecks`
with a "this policy gates admission" bit. Rejected:

- **Different lifecycle.** Per-policy posture is per-relationship
  ("peer A → peer B is allowed when A passes check X"). Admission
  posture is per-peer ("peer is allowed in the mesh at all when it
  passes check X"). They evolve on different cadences.
- **Different audit story.** A traffic-rule failure is a routine ACL
  drop and is not interesting to a compliance auditor. An admission
  failure is the auditor's whole point. Separating them keeps the
  audit log readable.
- **Different revocation model.** Per-policy posture revokes by
  silently filtering the NetworkMap. Admission posture revokes by
  closing the session. Putting both behind the same toggle would
  conflate two different operator intents.

The two layers compose: a peer that passes admission can still be
filtered out of specific policies based on the policy's own posture
checks. They are independent gates evaluated at different points.

## Consequences

### What this gives us

- **Provable refusal at the control plane.** A peer that fails
  admission never enters the mesh. The audit log carries the reason.
- **Bounded revocation latency.** ~6 min worst-case from vendor flip
  to session close. Tunable via the env var.
- **Compliance-grade artefacts.** CSV export is what the auditor
  asks for, ready to send.
- **Zero client changes.** Everything is server-side. Existing
  clients reconnect with backoff on `PermissionDenied` and surface
  the message in their tray UI.

### What we accept

- **Vendor lookup costs at every gate point.** With the in-process
  MDM cache (5 min TTL, errors NOT cached) the typical case is a
  cache hit; first lookup per peer per window pays the network round
  trip. The `Sync` gate runs on every metadata update, so for an
  active peer the cost is amortised across the cache window.
- **`FailOpen` is per posture-check, not global.** Operators with a
  strict posture leave it false (default). Those who care more about
  availability than fail-closed flip it on the EndpointSecurityCheck
  and accept the trade-off explicitly.
- **A misconfigured admission list locks everyone out.** If the
  operator ticks a check that no peer can pass and saves, every peer
  starts failing on the next Sync. Mitigation: the dashboard prompts
  before save, and the audit event records the policy change with
  initiator + timestamp so it's trivial to revert.
- **No `iat` reuse.** The gate runs on every Login/Sync regardless
  of how recently the peer last passed. We do not cache "this peer
  passed admission" because compliance state can change at the
  vendor without a posture-check structural change. That's a
  deliberate cost.

### What we did not do (and why)

- **No login-time webhook to a custom service.** Considered for
  parity with Okta IdP-side device assurance; rejected because the
  posture-check abstraction already handles "delegate the check to
  an external system" via `EndpointSecurityCheck`. A webhook hook
  would duplicate that surface.
- **No "soft" admission (warn but allow).** Considered for rollouts;
  rejected because it splits the audit semantics. The recommended
  rollout is to leave the toggle off, build the posture check,
  observe how many peers would fail via the existing per-policy
  posture, then flip the toggle once the failure set is empty.
- **No per-peer admission overrides.** Considered for the
  break-glass case ("this CEO laptop is non-compliant but must keep
  working"). Rejected: the operator can either fix the device or
  remove the relevant check from the admission list. A per-peer
  bypass would erode the audit story.
