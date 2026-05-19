# ADR-0021 — Policy-change peer propagation: consistency model (async off the request path)

## Status

**Proposed**. Decision gate for [openzro/openzro#68](https://github.com/openzro/openzro/issues/68).
Diagnosis is complete (in the issue); this ADR settles the consistency
model so implementation can land in a separate PR. **No code lands
with this ADR.**

**Clean-room mandate.** `management/` is AGPL. Upstream NetBird does
this propagation synchronously on purpose; this ADR and any
implementation are reasoned from openZro's own code and general
backend patterns — no upstream AGPL diff is consulted or ported
(ADR-0001 §3.1). Each implementation commit cites its public sources
and confirms this.

## Context

Operators perceive latency when adding/editing an access-control
policy. Diagnosis (issue #68, evidence-backed): **not the frontend**
(the dashboard issues a single `POST /policies` in the common case)
and **not redundant work** — it is inherent propagation work done
**synchronously inside the request**.

Verified in code at `management/server/policy.go`:

- `SavePolicy` (`:34`) → `:83 am.UpdateAccountPeers(ctx, accountID)`
- `DeletePolicy` (`:90`) → `:129` same
- both gated by `arePolicyChangesAffectPeers` (`:149`)

When the change affects peers, `UpdateAccountPeers` runs **before the
HTTP response**: it recomputes the account network map and pushes to
every connected peer. On large accounts that recompute+push dominates
POST latency — the perceived "slow submit".

**Three propagation models already coexist in-tree** (this ADR's main
constraint — the codebase is not consistent today):

- **Synchronous** `am.UpdateAccountPeers(ctx, accountID)` — `policy.go`
  and 8 other files (`dns.go`, `group.go`, `nameserver.go`,
  `route.go`, `peer.go`, `posture_checks.go`, `user.go`, `account.go`).
- **Fire-and-forget goroutine** `go am.UpdateAccountPeers(...)` —
  `account.go:469` (unbounded; a burst stampedes).
- **Debounced buffer** `am.BufferUpdateAccountPeers(...)` —
  `account.go:1690`, `:1936` (coalesces bursts; bounded).

`SendUpdate` to a peer already **drops on a full channel**
(non-blocking, best-effort) — so propagation is *already* not a
strict, guaranteed-before-response delivery; only the
*recompute+enqueue* is on the request path today.

## Decision

### D1 — Move policy-change propagation OFF the request path

`SavePolicy`/`DeletePolicy` persist in the transaction, return the API
response, and propagate **asynchronously**. The recompute+push is
inherent work but does not need to block the `200`.

### D2 — Reuse `BufferUpdateAccountPeers`, not a bare goroutine

Of the two existing async precedents, the debounced buffer
(`BufferUpdateAccountPeers`) is chosen over `go UpdateAccountPeers`.
Policy edits arrive in bursts (operator iterating; multi-rule saves);
the buffer **coalesces** them into one recompute and is **bounded**,
where a bare goroutine stampedes the recompute under load. Reusing an
existing mechanism also adds no new infrastructure and converges the
codebase toward one async model instead of adding a fourth.

### D3 — Consistency model: bounded eventual, explicitly stated

After the API confirms a policy change, connected peers receive the
new enforcement a **short, bounded** time later (the buffer flush
window), not strictly-before-response. This is acceptable for policy
changes and is barely a weakening: `SendUpdate` is already
drop-on-full/best-effort. The buffer-flush bound is the SLO and must
be documented + metric-backed (D4).

### D4 — Observability is mandatory

Ship with a propagation **lag** metric (enqueue→flush) and a
**failure** metric. An async path with no visibility is a regression
in operability; the metric is part of the implementation, not a
follow-up.

### D5 — Feature-gated rollout

Runtime flag, **sync default → async opt-in → flip the default** once
the metrics validate it in a real account. De-risks a
consistency-model change to the hot policy path.

### D6 — Scope: policy first, model is general

This ADR establishes the **consistency model for `UpdateAccountPeers`
propagation**. Phase 1 applies it to the policy path (the reported
pain). The other 8 synchronous callers are a **follow-up** under this
same model — explicitly *not* boiled into one change, but new code
must not add a 4th model; it conforms to D2.

### Phase plan

- **Phase 0** — this ADR.
- **Phase 1** — `SavePolicy`/`DeletePolicy`: route the affects-peers
  branch through `BufferUpdateAccountPeers`, behind the D5 flag, with
  the D4 metrics. Regression test: API returns before propagation;
  propagation still happens within the bound.
- **Phase 2** *(separate issue)* — converge the other 8 sync callers
  onto the same model, one area per PR.

### Out of scope

Changing `SendUpdate`'s drop-on-full behavior; the network-map
recompute algorithm itself; a distributed/broker propagation rework.

## Rationale

- **Why async:** the recompute+push is real work but the operator's
  `200` does not depend on every peer having been enqueued; blocking
  on it only makes large accounts feel broken.
- **Why the buffer (not `go`):** burst-coalescing + boundedness;
  reuses an audited in-tree mechanism; reduces (not grows) the number
  of propagation models.
- **Why feature-gated:** it changes the consistency model of the hot
  policy path; a flag lets us validate via D4 metrics before flipping.
- **Why ADR-first:** AGPL clean-room boundary + a cross-cutting
  consistency-model decision that future callers must follow.

## Trade-offs

### What we accept

- Policy changes are visible to peers a bounded short time *after* the
  API confirms (vs. before today) — documented, metric-bounded.
- A temporary increase in code-path divergence (Phase 1 policy-only)
  until Phase 2 converges the rest.
- Clean-room overhead (reason from our own 3 precedents, not upstream).

### What we don't accept (rejected alternatives)

- **Stay synchronous + micro-optimize the recompute** — does not
  remove the request-path coupling; large accounts still feel slow.
- **Bare `go UpdateAccountPeers`** — unbounded stampede under bursts;
  adds a 4th uncoordinated model.
- **A new bespoke async/queue subsystem** — `BufferUpdateAccountPeers`
  already exists and is sufficient; new infra is unjustified.
- **Boiling all 9 callers in one PR** — unreviewable, high blast
  radius; the ADR sets the model, Phase 2 converges incrementally.
- **Consulting the upstream AGPL implementation** — license posture
  (ADR-0001 §3.1), non-negotiable.

## Open questions

- Buffer flush window: reuse the existing `BufferUpdateAccountPeers`
  interval, or a policy-specific bound? What number is the documented
  SLO?
- Failure semantics after a `200`: the buffer's existing retry/coalesce
  behavior — is it sufficient, or does policy need explicit
  surfacing/idempotency beyond it?
- Flag shape: per-account vs global; default-flip criteria from the D4
  metrics.
- Does any caller rely on propagation having happened *before* the API
  returns (an implicit ordering assumption to audit before flipping)?

## References

- [openzro/openzro#68](https://github.com/openzro/openzro/issues/68) —
  diagnosis, decision points, Codex review (consolidated in-thread).
- openZro code: `management/server/policy.go`
  (`SavePolicy`/`DeletePolicy`/`arePolicyChangesAffectPeers`),
  `management/server/account.go:469` (`go` precedent), `:1690`/`:1936`
  (`BufferUpdateAccountPeers` precedent); the 9 synchronous
  `UpdateAccountPeers(ctx,…)` call sites.
- Precedents: [ADR-0001](0001-openzro-foundation.md) §3.1 (license
  posture), [ADR-0018](0018-server-side-flow-policy-resolver.md)
  (server-side propagation/consistency precedent),
  [ADR-0020](0020-openzro-ssh-identity-protocol.md) (clean-room,
  ADR-first, phased precedent).

## Amendments

_None yet._
