# ADR-0004: Admission bypass + group-scope exemption

- **Status**: Accepted
- **Date**: 2026-04-27
- **Decision-makers**: openZro maintainers
- **Supersedes**: §Consequences "no per-peer admission overrides" of [ADR-0003](./0003-device-admission-gate.md)

## Context

[ADR-0003](./0003-device-admission-gate.md) introduced the
account-wide Device Admission gate. Its §Consequences section
explicitly rejected per-peer overrides:

> No per-peer admission overrides. Considered for the break-glass
> case ("this CEO laptop is non-compliant but must keep working").
> Rejected: the operator can either fix the device or remove the
> relevant check from the admission list. A per-peer bypass would
> erode the audit story.

Two facts surfaced after that decision shipped:

1. **Routing / gateway peers**. The gate refuses every peer that
   cannot pass the configured posture checks. Server-side peers
   (cloud VMs, Kubernetes pods, on-prem servers running the
   client as a daemon) are not enrolled in MDM/EDR — their host
   OS has no Intune/SentinelOne/Huntress agent to report from.
   With the gate on and an `EndpointSecurityCheck` in the
   admission list, those peers cannot enter the mesh, which
   means the operator cannot bring up their own infrastructure.
2. **Break-glass legitimacy**. The original "bypass would erode
   the audit story" reasoning conflates two different design
   questions. A bypass that is undocumented, unscoped, and
   permanent erodes the audit story. A bypass with **mandatory
   audit metadata** (initiator, reason, expiry) plus an
   automatic expiry plus an audit event for every grant /
   revoke / expiration **strengthens** the audit story — the
   auditor sees more, not less.

The original framing was therefore too restrictive. The gate
needs two complementary escape hatches.

## Decision

Add two declarative axes on top of the existing gate. The
account-wide enable toggle and the posture-check list from
ADR-0003 stay unchanged.

### Axis 1 — Group-scope exemption (declarative, default for infra)

A new `Settings.AdmissionExemptGroups []string` lists Group IDs
whose member peers skip the admission gate entirely. The
evaluation is OR-semantic: a peer in **any** listed group is
exempt.

Operator workflow:

1. Create a Group `infrastructure-peers`.
2. Issue setup keys with `AutoGroups: ["infrastructure-peers"]`
   for every routing / gateway peer.
3. Add the group ID to `Settings.AdmissionExemptGroups`.
4. Bring up infrastructure peers normally. They enter the mesh
   without ever being checked against the posture list.

Audit: changes to the list emit
`account.setting.admission.exempt_groups.update` with the new
list of group IDs as event meta.

This is the default path for infra peers. The bypass axis below
is for one-off cases on user endpoints.

### Axis 2 — Per-peer bypass (manual, break-glass)

A new `peer_admission_bypasses` table stores time-bounded
overrides per `(account, peer)`:

```text
id            uint64           primary key
account_id    string           non-null, indexed (with peer_id)
peer_id       string           non-null
initiator_id  string           non-null  -- WHO authorised
reason        text             non-null  -- WHY (free text)
granted_at    timestamp        non-null
expires_at    timestamp        non-null  -- WHEN it stops
```

`expires_at` is required; the API rejects no-expiry grants.
Maximum permitted duration is 30 days — long enough for
"device replacement next month", short enough that a forgotten
bypass cannot become permanent.

Audit:

| Event                          | When                         | Initiator      |
|--------------------------------|------------------------------|----------------|
| `peer.admission.bypass.granted`| Operator grants the bypass   | the operator   |
| `peer.admission.bypass.revoked`| Operator pulls it back early | the operator   |
| `peer.admission.bypass.expired`| Worker sweeps an expired row | system         |

A background sweeper (`admission.RunExpiryWorker`) runs hourly
by default (configurable via
`OPENZRO_ADMISSION_BYPASS_SWEEP_INTERVAL_SECONDS`, 60s minimum).
It deletes expired rows and emits the `expired` event so the
auditor sees the full lifecycle from grant to expiration.

### Order of evaluation

`evaluateAdmission` runs the two short-circuits BEFORE the
posture checks:

```
if peer.groups ∩ AdmissionExemptGroups ≠ ∅:
    skip (declarative exemption)
if active bypass exists for (account, peer):
    skip (break-glass override)
otherwise:
    run posture checks
```

This keeps the cost-ordered: the cheapest checks run first. An
exempt routing peer never queries Microsoft Graph; a bypassed
peer never queries Microsoft Graph. Vendor API budget is
preserved for the peers the operator actually wants to gate.

## Consequences

### What this gives us

- **Routing / gateway peers** are no longer blocked by the
  admission gate. Operators can declare "infrastructure" once
  and bring up new gateway peers without thinking about
  admission again.
- **Break-glass** is supported with an audit trail that is
  **stronger** than the original ADR-0003 reasoning feared:
  every override is named, reasoned, and time-bounded.
- **Auditor walkthrough** still works end-to-end. The CSV
  export at `/api/events/admission.csv` now includes the four
  new event codes alongside the denials, so the timeline shows
  "denied at T0, bypassed at T1 by user U with reason R until
  T2, expired at T2".

### What we accept

- **Operator error**: forgetting to add a routing peer's group
  to `AdmissionExemptGroups` keeps the gate hard for that peer.
  Recovery is a one-line settings edit; documented in the
  operator guide.
- **30-day cap on bypass**: longer windows require a re-grant.
  Deliberate friction; the auditor expects a fresh entry every
  month rather than a stale "bypass valid until 2027" row.
- **Worker is best-effort, not real-time**: a row that expired
  at 14:00 may keep its database presence until 15:00 when the
  sweep runs. `IsActive` rejects expired rows on read so the
  gate behavior is instant; only the audit event for expiry is
  delayed by up to one sweep interval. Acceptable.

### What we did NOT do (and why)

- **No allow-list scope** (`AppliesToGroups` whitelist instead
  of `ExemptGroups` blacklist). Considered: more permissive by
  default. Rejected because the typical configuration is "gate
  everyone except infra" — the blacklist form fits the mental
  model and prevents the "I forgot to whitelist new endpoints"
  failure mode.
- **No bypass without expiry**. Considered for "this peer is
  legacy, will never be compliant, must keep working forever".
  Rejected: that case should be modelled as a group exemption
  (axis 1), which is reviewable as a settings change. The
  bypass pathway is for time-bounded overrides only; the API
  enforces this.
- **No auto-revoke when the vendor reports compliance recovered**.
  Considered: nice ergonomically, the bypass becomes a no-op
  the moment the device is fixed. Rejected for v1: the vendor
  call cost would land on every Sync to check, and the worker's
  hourly sweep already catches the expiry case. We may revisit
  if operators ask.

## Implementation references

- Domain types: [`management/server/types/settings.go`](../../management/server/types/settings.go)
  `Settings.AdmissionExemptGroups`
- Bypass model + store: [`management/server/admission/`](../../management/server/admission/)
- Evaluation order: [`management/server/peer_admission.go`](../../management/server/peer_admission.go)
- Expiry worker: [`management/server/admission/expiry_worker.go`](../../management/server/admission/expiry_worker.go)
- HTTP CRUD: [`management/server/http/handlers/admission_bypass/handler.go`](../../management/server/http/handlers/admission_bypass/handler.go)
- Activity codes 91–94: [`management/server/activity/codes.go`](../../management/server/activity/codes.go)
