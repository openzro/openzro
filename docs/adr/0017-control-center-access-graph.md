# ADR-0017 — Control Center: topological access-graph view

## Status

**Proposed**. Phase 0 of [openzro/openzro#39](https://github.com/openzro/openzro/issues/39).
This ADR settles the three gating decisions (D1 graph computation,
D2 rendering dependency, D3 v1 scope) so Phase 1 (backend resolver +
API) and Phase 2 (dashboard view) can start. No code lands with this
ADR — it is the prerequisite gate the issue mandates.

## Context

`docs.openzro.io/manage/control-center` documents a **Control Center**:
a graph view that, for a focus node (Peer / User / Group / Network),
shows the access-control policies that apply and the resources it can
reach, with port/protocol edge labels and clickable policy chips that
open the existing policy editor (graph revalidates on save).

The doc is a NetBird-forked page (`Source: https://docs.netbird.io/manage/control-center`,
openZro string-substituted). **The feature is not implemented** — a
dashboard-wide grep finds zero `control-center` / `topology` /
`access-graph` references and nothing in the sidebar. The docs
currently promise a feature the product does not have. (Tracked as a
recurring forked-doc-vs-reality drift; see
[`docs/security`](../security/) discipline and the GmbH/Berlin scrub
and `resolved_addresses` revert precedents.)

This is roadmap-scale: a backend resolver + a new frontend graph
dependency + a new route. The issue requires a dedicated window and
forbids mixing it into unrelated release work. It must not be
re-derived in TypeScript — re-computing access in the frontend
produces a visualization that *misrepresents enforced access*, the
exact failure class that got `resolved_addresses` reverted
([ADR rationale carried from #39 D1]).

Three decisions had to be made before any code:

- **D1** — where the graph is computed.
- **D2** — the graph rendering dependency (the dashboard ships none;
  `dashboard/CLAUDE.md` flatly forbids new charting libraries, so an
  ADR is the only sanctioned way to introduce one).
- **D3** — v1 scope (four views in one shot is too much).

## Decision

**North star.** The Control Center is a **read-only audit surface**
that answers, for a focus node: *what does it reach right now, through
which policy, on which protocols/ports, and what is policy-permitted
but blocked by posture* — computed from the same engine that enforces
access. It is not a topology editor and not a network diagram; every
edge it draws must correspond to enforced (or explicitly
posture-blocked) reality.

### D1 — Compute the graph server-side, from the enforcement engine

The access graph is derived **server-side, from the same code that
decides what a peer actually reaches** — never re-derived in the
dashboard. The issue's original citation
(`management/server/flow_policy_resolver/{resolver,match,index}.go`)
**drifted and no longer exists** — the logic was consolidated onto the
`Account` receiver; the principle is unchanged, the paths are
corrected here.

**D1.1 — Reach producers.** The adapter composes the *same producers
`GetPeerNetworkMap`
([`account.go:244`](../../management/server/types/account.go)) uses to
build a peer's network map*:

- **peer ↔ peer:** `GetPeerConnectionResources`
  ([`account.go:982`](../../management/server/types/account.go)) →
  reachable peers + `[]*FirewallRule` (group expansion + posture
  gating already applied). `FirewallRule` already carries `PolicyID`
  ([`account.go:1050`](../../management/server/types/account.go)) and
  protocol/direction — the policy-chip identity is not a data gap.
- **peer → network/route:** for a route edge the audit truth is the
  **composition of two facts, not either alone**:
  1. **distribution (peer-centric):** does the route reach this peer?
     `GetRoutesToSync`
     ([`account.go:124`](../../management/server/types/account.go)) +
     `GetNetworkResourcesRoutesToSync`
     ([`account.go:1405`](../../management/server/types/account.go)),
     keyed on `route.Groups`/ACL peers — what
     `GetPeerNetworkMap` concatenates into `NetworkMap.Routes`
     ([`account.go:281,283,308`](../../management/server/types/account.go)).
  2. **permission (enforcement-centric, at the router):**
     `GetPeerRoutesFirewallRules`
     ([`account.go:1208`](../../management/server/types/account.go)) /
     `GetPeerNetworkResourceFirewallRules`, keyed on
     `route.AccessControlGroups`, which materialise the actually
     permitted traffic (the accepted `SourceRanges`, or
     `getDefaultPermit` when `AccessControlGroups` is empty).

  **A route synced to a peer is NOT proof its traffic is permitted.**
  `route_test.go:1934-1936` pins this: `peerC` is in `route1`'s
  distribution groups yet `GetPeerRoutesFirewallRules` returns **zero**
  rules for it. A graph that draws a reachable edge from "route is in
  `NetworkMap.Routes`" alone would lie. v1 route edges must be backed
  by *distribution ∩ a permitting route firewall rule*; the firewall
  rule also supplies the protocol/port/source-range labels. (This
  corrects an over-rotation in the previous D1.1 draft, which had
  swung from "router-centric only" to "distribution only" — neither
  pole is the audit answer.)

**D1.2 — Posture-blocked reach needs an explanatory pass (same engine,
richer output).** The current enforcement output is *already
posture-filtered*: `getAllPeersFromGroups`
([`account.go:1099`](../../management/server/types/account.go))
`continue`s a peer that fails `validatePostureChecksOnPeer` before it
is accumulated, so the resolver result cannot distinguish "no policy
connects these" from "a policy connects them but posture blocked it".
The north-star "what is policy-permitted but posture-blocked" question
is therefore **not** answerable by consuming the existing output. v1
adds a read-only **explanatory pass** that walks the *same* policies
and reuses the *same posture check implementations* to record the
posture-dropped candidates. Precision on the helper: the bool-only
`validatePostureChecksOnPeer`
([`account.go:1119`](../../management/server/types/account.go)) is
sufficient for the **binary** "is this edge `posture_blocked`"
decision, but it does **not** carry *which* check failed. To name the
failing check the pass must use the **structured** posture evaluation
— the `check.Check` results surfaced as an `AdmissionDenial`-shaped
value (`{PostureCheckID, PostureCheckName, CheckType, Reason}`,
[`account.go:1156`](../../management/server/types/account.go), the
form `EvaluateAdmission` already produces) — extracted/shared, not
re-implemented. Either way this is a richer output of the same
engine, **not a parallel policy walker**: it makes no access
decisions, it annotates the ones the engine already made.

Phase 1 extracts a read-only adapter `(account, focusType, focusID) ->
GraphDTO`, built **by composing the producers above**, not by
re-deciding access. A new handler `GET /api/control-center/{view}/{id}`
returns the DTO, gated to Admin / Network-Admin (matches the doc's
permission note; RBAC reuses the existing middleware). This surface is
AGPL clean-room with respect to upstream `management/` — the DTO shape
and handler are designed here, not ported.

**Minimum DTO envelope (non-binding — exact schema is Phase 1).** The
sketch is intentionally not the final contract, but it must be able to
carry what the requirements above force, so it is pinned to at least:

```
GraphDTO {
  nodes: [{ id, kind: focus|policy|group|peer|route|network_resource, label, meta }]
  edges: [{
    from, to,
    permitSource: policy | route_default_permit | router_local, // D1.1; see amendment 2026-05-17
    policyId?, policyName?,          // present iff permitSource == policy (chip identity)
    protocol, ports, sourceRanges?,  // non-lossy: ranges preserved, not flattened
    direction,                       // in|out|bidirectional
    state: enforced | posture_blocked, // D1.2; never collapsed into "no edge"
    meta                              // e.g. group-focus "k of n members", failing posture check
  }]
}
```

### D2 — `@xyflow/react` + `@dagrejs/dagre` (both MIT)

Adopt **`@xyflow/react`** (react-flow v12, **MIT**) for node/edge
rendering and custom node/edge components, with **`@dagrejs/dagre`**
(**MIT**) for directed-graph auto-layout. This is a deliberate,
ADR-sanctioned exception to the `dashboard/CLAUDE.md` "No new
charting / table / dropdown libraries" rule, scoped to the Control
Center route only. Both are OSI/MIT, satisfying the "no non-OSI
licenses (SSPL/BUSL/Commons Clause)" rule.

Why this pair: it is **what NetBird's own Control Center uses**
(verified against `netbirdio/dashboard` — `@xyflow/react ^12`,
`@dagrejs/dagre ^1` in `package.json`; `Handle/Node/Position` imported
in `src/modules/control-center/nodes/*` and the route `page.tsx`). The
`elkjs` / `d3` deps also present in NetBird's bundle are **not** used
by its Control Center and are **not** adopted here. Matching the
upstream choice gives us a proven fit for this exact UI (custom
node/edge component model, clickable nodes for the policy-chip→editor
flow) and parity with the documented behavior.

**License & provenance:** the openZro dashboard is already a fork of
NetBird's web UI (established in [ADR-0016](./0016-dashboard-redesign-v2.md)
§Context) — adopting the same MIT library and, where useful,
mirroring NetBird's node/edge component decomposition is consistent
with that existing posture. The **graph data contract is ours**: the
GraphDTO comes out of openZro's BSD enforcement engine (the D1
producer set), not from any upstream `management/` code. Versions are
pinned; a
bundle-size delta is measured in the Phase 2 PR and recorded back
here before the dependency merges.

### D3 — v1 ships Peers + Groups focus, including peer→policy→network(route) reach

The first shippable Control Center covers **Peers** and **Groups**
*focus types*. **Users** and **Networks as a standalone focus view**
are a tracked v2 follow-up.

**Network/route reachability is in v1, not deferred.** From a Peer (or
Group) focus the graph must render, as first-class targets:

1. **peer → policy → peer** reachable peers and the permitting
   policy, with protocol/port on the edge; and
2. **peer → network/route** the network resources / routes that peer
   reaches **and whose traffic is actually permitted**, with the
   permit source (a policy, or a route default-permit) on the edge —
   the headline audit question ("how does this peer connect, through
   what, to which networks?").

This is engine-backed via the D1.1 producers: `GetPeerConnectionResources`
for peer↔peer, and for route edges the **composition** of distribution
(`GetRoutesToSync` + `GetNetworkResourcesRoutesToSync`) **with**
router-side permission (`GetPeerRoutesFirewallRules` /
`GetPeerNetworkResourceFirewallRules`) — synced ≠ reachable, per D1.1.
The Phase 1 adapter consumes that composition plus the D1.2
explanatory pass, so network/route reach **and** posture-blocked reach
are v1 acceptance requirements, not a v2 widening. What stays v2 is the
*inverse* navigation (pick a Network as the focus node and fan out to
who reaches it) and the Users focus type — those add focus-type
surface without changing the engine contract v1 already exercises.

**Group focus semantics (pinned, not left to the implementer).** A
group focus renders the **union** of its members' per-member reach —
*not* the intersection. Intersection would hide access that genuinely
exists for some members, which is precisely the wrong answer for an
audit tool. Each aggregated edge carries `meta` "k of n members"
(how many group members actually have that reach) and posture is
evaluated **per member** (consistent with the enforcement model, where
`validatePostureChecksOnPeer` is per-peer) — a member blocked by
posture contributes a `posture_blocked` edge, it is not silently
dropped from the union.

Rationale: Peers + Groups focus with full peer/route reach carries the
highest audit value and exercises the engine's reach producers +
explanatory pass end-to-end, proving the engine→graph→policy-editor
round trip before adding inverse-focus navigation.

### Functional requirements v1 must close

Mirroring the ADR-0016 discipline of pinning the must-haves so a
prototype cannot quietly drop them, v1 is not shippable until:

- Peer focus renders reachable **peers** and reachable
  **networks/routes**, the latter as *distribution ∩ permission* per
  D1.1 (a synced-but-unpermitted route is **not** drawn as reachable),
  each edge labelled with its permit source and protocol/port/range.
- Group focus renders the **union** of per-member reach with "k of n
  members" edge metadata and per-member posture (D3 — pinned, not the
  implementer's call).
- **Edges whose `permitSource == policy`** carry a clickable chip that
  opens the existing policy editor; on save the graph revalidates (SWR
  `mutate`). Edges backed by a **route default-permit** (no
  `AccessControlGroups`) render the permit-source plainly with **no**
  fake policy chip (D1.1 / Point 3).
- **`posture_blocked` is a v1 edge state** (D1.2): policy-permitted
  but posture-blocked reach renders as a distinct edge — never
  collapsed into "no edge"/"no access". The open design question is
  *how to render* it, not *whether* (resolved by the owner; see
  Status answer).
- Cross-tenant scoping and empty-state (no policies / isolated peer)
  render correctly.

### Phase plan (Phase 0 is this ADR)

| Phase | Scope | Gate |
|---|---|---|
| **0. ADR** | This document — D1/D2/D3. | Owner sign-off. |
| **1. Backend** | Read-only `GraphDTO` adapter (D1.1): `GetPeerConnectionResources` for peer↔peer; route edges = **distribution** (`GetRoutesToSync` + `GetNetworkResourcesRoutesToSync`) **∩ permission** (`GetPeerRoutesFirewallRules` / `GetPeerNetworkResourceFirewallRules`, incl. `getDefaultPermit` → `permitSource`); **plus the D1.2 explanatory pass** (bool helper for the binary `posture_blocked` state, structured `AdmissionDenial`-shaped eval to name the failing check). `GET /api/control-center/{view}/{id}`; Admin/Network-Admin gate. Tests: peer↔peer reach; **route synced-but-unpermitted is NOT reachable** (`route_test.go:1934` shape); `route_default_permit` edge has no policy chip; `posture_blocked` distinct from enforced and from no-policy; group-focus union + "k of n"; cross-tenant; empty cases. | Codex review; no frontend dep yet. |
| **2. Dashboard** | Add `@xyflow/react` + `@dagrejs/dagre` (pinned; bundle delta recorded back into this ADR); `Control Center` sidebar entry; route with Peers/Groups tabs; `useFetchApi` the DTO; policy chip → existing `AccessControlModal`, SWR-`mutate` on save. | Bundle review; per-PR. |
| **3. Polish** | Port/protocol edge labels, click-to-switch-focus, Cypress E2E for the user-visible flow. | — |
| **v2 (separate)** | Users + Networks focus types. | New tracking issue. |

### Out of scope

- **Users** focus type, and **Networks as a standalone focus view**
  (the inverse: pick a network → who reaches it) — v2 (D3).
  *Network/route reachability **from** a Peer/Group focus is v1.*
- Topology editing. The graph is **read-only**; group/policy
  create/delete stays in their existing sections (matches the doc).
- Any frontend re-derivation of access (D1 — explicitly forbidden).
- Publishing the `manage/control-center` doc page: it stays
  unpublished/removed until Phase 2 ships, so docs do not describe a
  non-existent feature.

## Rationale

### Why server-side, from the enforcement engine

A graph that re-walks policies in TypeScript will, sooner or later,
diverge from what the data plane actually enforces (group expansion,
posture gating, unidirectional ACL coercion, network-resource rules)
and show access that does not exist — a security-relevant lie in a
zero-trust audit tool. Deriving the DTO from the *same producers
`GetPeerNetworkMap` uses* (D1.1) — plus an annotator that reuses the
*same* posture check (D1.2) — makes the visualization correct *by
construction*. This is the same lesson the `resolved_addresses`
revert encoded.

### Why match NetBird's library choice

The dependency rule exists to stop bundle bloat and look
fragmentation, not to forbid a graph view we are explicitly building.
Given a graph view is required, the lowest-risk pick is the library
already proven against this exact feature upstream, MIT-licensed, with
a custom-component model that fits the clickable-policy-chip
interaction. Inventing a hand-rolled SVG graph engine to avoid one
MIT dep would be more code, more maintenance, and worse interaction
fidelity for no license or bundle win that survives scrutiny.

### Why Peers + Groups first

Four views multiply the resolver adapter surface and the frontend
node-type matrix before the core round trip is validated. Peers +
Groups is the smallest scope that still delivers the headline audit
value and de-risks the resolver contract for the v2 widening.

## Trade-offs

### What we accept

- **One new MIT dependency pair** on the Control Center route,
  against the standing "no new charting libraries" rule — accepted
  via this ADR, scoped, pinned, bundle-measured.
- **Backend engine coupling.** The DTO adapter is tied to the D1.1
  producer set + the D1.2 annotator; a refactor of any of them must
  keep the adapter green (Phase 1 tests pin this).
- **v1 is partial** — no Users focus and no inverse Networks-focus
  view; but peer/group→policy→peer **and** →network/route reach are
  in. Docs stay unpublished until v1 ships, so we never over-promise.

### What we don't accept (rejected alternatives)

- **Frontend re-derivation** of the access graph — rejected (D1):
  misrepresents enforced access; the `resolved_addresses` failure
  class.
- **A parallel server-side policy walker that re-decides access** —
  rejected: two access-deciding paths drift; single-source-of-truth
  is the whole point. Note this is *distinct from* the D1.2
  explanatory pass, which makes **no** access decisions — it reuses
  `validatePostureChecksOnPeer` to annotate decisions the engine
  already made. Annotating ≠ re-deciding.
- **`elkjs` / `d3` / `cytoscape`** — rejected for v1: not what the
  upstream feature uses, larger or lower-fidelity for this exact
  interaction, no offsetting license/bundle benefit.
- **All four views in v1** — rejected (D3): largest surface, slowest
  to first ship, delays validating the resolver contract.

## Open questions

- Exact `GraphDTO` node/edge schema (Phase 1 design — must satisfy
  the D1 minimum envelope without lossy flattening of port ranges).
- *How* to render the `posture_blocked` edge state (resolved *that*
  it is in v1 — D1.2 + owner decision; the remaining question is
  visual treatment, e.g. dashed edge + "would reach if compliant"
  affordance + which failing check to surface).
- Exact shape of the D1.2 explanatory pass (a thin annotator reusing
  `validatePostureChecksOnPeer`) vs. threading an out-parameter
  through the existing producers — Phase 1 design, constrained to
  "no second access-deciding path".
- Bundle-size delta of the `@xyflow/react` + `@dagrejs/dagre` pair —
  measured in the Phase 2 PR and recorded back into D2 before merge.
- The README ADR index is stale (stops at 0009); reconciling 0010–0017
  is orthogonal pre-existing debt, not in this ADR's scope.

## References

- Issue: [openzro/openzro#39](https://github.com/openzro/openzro/issues/39) — Control Center topological access-graph view.
- Engine producers (D1.1) — distribution: `GetPeerConnectionResources` ([`account.go:982`](../../management/server/types/account.go)), `GetRoutesToSync` ([`account.go:124`](../../management/server/types/account.go)), `GetNetworkResourcesRoutesToSync` ([`account.go:1405`](../../management/server/types/account.go)), composed by `GetPeerNetworkMap` ([`account.go:244`](../../management/server/types/account.go), routes concat `:308`). Route permission: `GetPeerRoutesFirewallRules` ([`account.go:1208`](../../management/server/types/account.go)), `getDefaultPermit` (no `PolicyID`) ([`account.go:1298`](../../management/server/types/account.go)); synced≠permitted pinned by `route_test.go:1934-1936`.
- Posture (D1.2): filter `getAllPeersFromGroups` `continue` [`account.go:1099`](../../management/server/types/account.go); binary helper `validatePostureChecksOnPeer` (bool) [`:1119`](../../management/server/types/account.go); structured denial `AdmissionDenial` [`account.go:1156`](../../management/server/types/account.go).
- Dependency rule: [`dashboard/CLAUDE.md` §"What not to introduce"](../../dashboard/CLAUDE.md).
- Dashboard fork posture: [ADR-0016 §Context](./0016-dashboard-redesign-v2.md).
- Upstream reference (library choice only, not ported): `netbirdio/dashboard` `package.json` + `src/modules/control-center/*`.
- Drift precedent: `resolved_addresses` scope-creep revert (frontend-vs-enforced divergence).

## Amendments

- **2026-05-17 — `permitSource` gains `router_local` (Phase 1, owner-decided).**
  The minimum DTO envelope originally listed `permitSource: policy |
  route_default_permit`. Phase-1 review surfaced that when the focus
  *is* the router serving a route, labelling that reach
  `route_default_permit` is wrong if the route carries
  `AccessControlGroups` (those gate other clients, not the router).
  A third value, **`router_local`**, was added: honest
  infrastructure-local reach, no policy chip. This is part of the
  wire contract — the Phase 2 dashboard must handle all three
  `permitSource` values. Pinned by the C8 contract test and the
  enum wire-value test. Not a reversal of D1.1; a faithful refinement
  of it.

- **2026-05-17 — D2 bundle-size delta measured (Phase 2, P6).** ADR
  D2 required the `@xyflow/react` + `@dagrejs/dagre` bundle delta to
  be measured and recorded before the dep merges. `next build` on the
  Phase-2 branch: the deps are **code-split into the `/control-center`
  route only** — "First Load JS shared by all" is unchanged at
  **103 kB** (no regression to any other route). `/control-center`
  First Load = **318 kB**, *below* `/access-control` (349 kB) and
  `/peers` (430 kB), i.e. within the existing per-route envelope.
  Route-specific JS = 73.3 kB (graph stack + components) vs ~17.5 kB
  for a comparable graph-less data screen → ≈55 kB route-scoped,
  route-only cost. Verdict: acceptable; the standing
  no-new-charting-lib concern (shared-bundle bloat) does not
  materialise because the libs never enter the shared baseline.

- **2026-05-18 — Topology v2 redesign (owner-decided; supersedes the
  v1 generic graph).** A Claude-Design hifi handoff
  (`design_handoff_topology/`) reframes Control Center from the v1
  peer-centric reach graph into a **4-column access map**:
  `User (+email) → Peers → Policies → Resources/Networks`, with
  fan-in/fan-out cubic-Bézier edges, an animated "flow", hover
  isolation (2-hop), tabs (Peer/User/Group/Networks), footer legend.
  The owner judged this the target. Scope/decision deltas:

  - **D1 — projection changes (not just a reskin).** The current
    `GraphDTO` is peer-centric: focus ∈ {peer,group}; it emits
    `focus/peer/route/network_resource` nodes and carries policy only
    as edge metadata — there is **no User entity, no email, no Policy
    as a column node, no User→Peer relation** (verified in
    `controlcenter/*.go` on main). The handoff needs a NEW
    server-side, enforcement-faithful **user-centric columnar
    projection** (`User{peerIds}` → `Policy{peerIds,resourceIds}` →
    `Resource`). D1's "derive from the enforcement engine, never
    re-derive in TS" principle is UNCHANGED; the *shape* is new. This
    is a substantial backend phase, not a frontend tweak — the single
    biggest cost in this redesign.
  - **D2 — xyflow KEPT, dagre DROPPED (no reversal to hand-built
    SVG).** Verified: xyflow can render the handoff (explicit-position
    custom nodes, custom Bézier edge type with gradient stroke +
    `stroke-dashoffset` flow keyframe, `<Background variant=dots>`,
    hover state). dagre's layered auto-layout actively fights a fixed
    4-column design, so it is replaced by the handoff's deterministic
    layout (fixed X per column-kind + `distributeY(count)`). The
    `@xyflow/react` dependency and its ADR-sanctioned exception stand;
    `@dagrejs/dagre` is removed once unused. #51/#53/#55 are reused,
    not discarded (lighter than a ground-up SVG rebuild — owner-chosen
    over the SVG option).
  - **D3 — v1 scope extended.** Users + Networks (deferred to "v2" in
    the original D3) are now in scope as root-column tabs alongside
    Peer/Group.
  - **Brand exception (sanctioned).** Edges are coloured by status —
    **green = allowed, red = posture_blocked** — a deliberate owner
    override of `dashboard/CLAUDE.md`'s violet-only / "no green
    checkmarks; success-error use violet variants" rule AND of the
    handoff's own violet-flow palette. Recorded here as a sanctioned
    exception (same mechanism as the xyflow charting-lib exception),
    scoped to the topology edges only. Rationale: the operator
    anchored on NetBird's green semaphore; the audit signal
    (reachable vs blocked) is judged worth the palette break.

  This is **Control Center v2**, tracked under #39 (kept open as the
  umbrella). Phasing: T0 = this amendment (gate); T1 = backend
  user-centric projection + API; T2 = xyflow columnar layout (dagre
  out); T3 = the 4 kind cards; T4 = Bézier-flow edges (green/red);
  T5 = hover-isolation + footer/legend/tabs + dot-grid chrome.
  Cypress still deferred (#52). Each phase: own PR, Codex review,
  owner-authorised merge — same cadence as v1.

- **2026-05-18b — Topology v2 model CORRECTION (supersedes the
  2026-05-18 description).** The prior entry under-specified the
  projection — it described only `User → Peers → Policies → Resources`
  as if that were THE model. Reviewing the NetBird Control Center
  screens with the owner: it is **one topology view with FOUR focus
  tabs**, **Policy is always the middle pivot column**, and the column
  set differs per focus:

  | Focus tab | Columns (left → right) |
  |---|---|
  | **Peer** | Peer → **Policies** → Resources/Networks (no Peers column) |
  | **User** | User(+email) → **Peers** (the user's machines) → **Policies** → Resources (only User has the Peers column) |
  | **Group** | Group → **Policies** → Resources |
  | **Networks** | **INVERSE fan-in**: Groups → **Policies** → the selected network's Resources → resource detail. Answers "*who* (which groups, via which policies) can reach THIS network" |

  - **v2 REPLACES v1 (owner-decided).** `buildPeerFocus` /
    `buildGroupFocus` are reshaped to the Policy-columnar form above;
    the v1 peer↔peer *effective-reach* graph (the
    `GetPeerConnectionResources`-based `addPeerReach` /
    `addPostureBlocked` / `addRouteReach` peer-graph) is **retired**,
    and its v1 reach-graph tests are replaced by columnar tests. The
    v1 "what does this peer effectively reach" answer is dropped in
    favour of the policy-wiring map.
  - **D1 unchanged.** Structure = the *configured* policy wiring;
    each edge's `State` is the *effective* enforcement (`enforced`
    vs `posture_blocked`) from the same posture engine — never
    re-derived in TS. The green/red *rendering* of that State is the
    sanctioned brand exception (still as recorded).
  - **Re-scope.** "T1 = user-centric projection" was wrong. **T1 =
    the full backend projection set**: `Focus{peer|user|group}` all
    emit the Policy-columnar shape (only `user` adds the Peers
    column) + `Focus=network` the inverse fan-in. `FocusUser`/
    `NodeUser` enums + `buildUserFocus` (User tab) are reusable;
    `buildPeerFocus`/`buildGroupFocus` are rewritten; a network
    focus + `NodeNetwork` is added. T2–T5 (frontend) unchanged in
    intent. The work-in-progress on `feat/cc-v2-t1-user-projection`
    (T1.1 enums + `buildUserFocus`) is kept and folded into this
    wider T1; no PR was opened for it under the wrong framing.

  Same cadence: this corrective amendment is the re-gate (doc-only,
  owner-authorised) before the backend reshape lands. #39 stays the
  umbrella; Cypress still #52.
