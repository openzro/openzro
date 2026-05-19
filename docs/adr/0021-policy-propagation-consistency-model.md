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

### D1.1 — Mutation under the lock; propagation after the lock is released

Today `SavePolicy`/`DeletePolicy` hold the account write lock
(`AcquireWriteLockByUID` + `defer unlock()`) and call
`UpdateAccountPeers` **before returning** — i.e. the recompute+push
runs **while the write lock is still held** (`policy.go`). The
mutation and audit event happen under the lock; **propagation
scheduling (buffered) — and the sync-default fallback path too — must
happen AFTER the write lock is released**. Otherwise the async mode
only fixes perceived latency while the sync default keeps holding the
account write lock across the full fanout (the contention the issue's
follow-up called out). This applies to *both* feature-flag modes.

### D2 — Reuse and evolve `BufferUpdateAccountPeers` as the single sanctioned primitive

Of the two existing async precedents, the debounced buffer
(`BufferUpdateAccountPeers`, `peer.go:1441`) is chosen over
`go UpdateAccountPeers` (`account.go:469`): policy edits arrive in
bursts (operator iterating; multi-rule saves); the buffer
**coalesces** them into one recompute and is **bounded**, where a
bare goroutine stampedes under load. Do **not** build a fourth model
or a new subsystem.

But the current implementation is **not** "already sufficient" — it
must be **evolved** to satisfy D3/D4/D5. Known gaps it must close:

- it captures the **caller's `ctx`** in the deferred `time.AfterFunc`
  (`peer.go:1441`) — a request `ctx` that is canceled once the API
  returns must not silently kill the buffered propagation; the
  scheduled work needs a lifecycle ctx of its own;
- no defined **retry contract**, and `UpdateAccountPeers` returns no
  error — failure is currently invisible;
- no propagation **lag/failure metric** (see D4);
- **undefined behavior on process death before flush** (in-memory
  timer only — see D3 durability boundary).

"Reuse/evolve as the single sanctioned propagation primitive" — not
"it already works".

### D3 — Consistency model: bounded eventual, explicitly stated

After the API confirms a policy change, connected peers receive the
new enforcement a **short, bounded** time later (the buffer flush
window), not strictly-before-response. Acceptable for policy changes
and barely a weakening: `SendUpdate` is already drop-on-full/
best-effort.

Explicit semantics this ADR fixes:

- **`200 OK` means: persisted + propagation scheduled/buffered** — it
  does **not** mean delivered/applied by peers, nor (per D7)
  resolver-refreshed.
- **Durability boundary:** the buffer is an in-memory timer; pending
  propagation **can be lost on process death before flush**. Accepted
  for Phase 1. If durable revocation ever becomes a hard requirement
  it is a separate mechanism/ADR — explicitly out of scope here.
- The flush-window **bound is an SLO, not an open number to defer**:
  the ADR does not pick the final value, but **Phase 1 cannot ship
  without an explicit, documented SLO** and the D4 metric proving it.

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

### D7 — The ADR-0018 flow-policy resolver rides the same trigger and SLO

`UpdateAccountPeers` also calls `am.flowPolicyIndex.Rebuild(accountID,
account)` (`peer.go:1328`) — the ADR-0018 server-side flow-policy
resolver is refreshed on the *same trigger* as peer propagation
(ADR-0018 explicitly "hooks the same triggers"). Therefore deferring
propagation **also makes the resolver eventual on policy changes**:
from Phase 1, `POST /policies` may return `200` **before the resolver
reflects the change**, bounded by the same D3 SLO.

This is a **deliberate, recorded** consequence that touches ADR-0018's
freshness semantics — it must not be changed implicitly by the
implementation. The resolver's staleness window == the propagation
SLO; the Phase-1 test (below) asserts they are the same bound. If any
consumer ever needs **strict** resolver freshness (synchronous rebuild
regardless of the propagation flag), that is a **separate
mechanism/ADR** and is *not* solved here.

### Phase plan

- **Phase 0** — this ADR.
- **Phase 1** — `SavePolicy`/`DeletePolicy`: route the affects-peers
  branch through the evolved `BufferUpdateAccountPeers` (D2), behind
  the D5 flag. **Hard ship gates (Phase 1 must NOT merge without all
  of these):**
  1. an explicit, **documented SLO** for the flush-window bound (D3);
  2. the D4 **lag + failure metrics** wired and proving the SLO;
  3. propagation scheduling / sync fallback occurs **after write-lock
     release** (D1.1);
  4. the buffered work runs on a **lifecycle ctx**, not the request
     ctx that dies at the `200` (D2);
  5. tests proving: (a) in async mode the response does **not** wait
     on fanout; (b) propagation still happens within the SLO;
     (c) burst **coalescing** works; (d) the `flowPolicyIndex`
     resolver refresh follows the **same SLO bound** as peer
     propagation (D7) — asserted explicitly, not assumed.
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
  interval, or a policy-specific bound? The *number* is open; shipping
  Phase 1 **without a documented SLO is not** (decided — D3 + Phase-1
  gate 1).
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
