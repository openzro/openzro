# ADR-0016 — Dashboard redesign v2 (Notion/Arc-flavored, incremental)

## Status

**Accepted (planning)**. Visual validation done on the
`feat/dashboard-redesign-v2` branch — tokens, primitives, shell, and
a Peers screen prototype are live at `/v2-preview` and `/v2-preview/peers`.
Full migration to production is a multi-month per-screen rollout,
tracked under a follow-up issue once this ADR lands.

## Context

The current openZro dashboard ([`dashboard/`](../../dashboard/)) is a
fork of NetBird's web UI: Next.js App Router, Tailwind, shadcn-ish
primitives, with brand-mark + violet palette swapped in. It works,
but a year of incremental fixes (light theme audit in #11, theme
tokens that flip via `nb-gray-*`, the 11-variant
[`Button.tsx`](../../dashboard/src/components/Button.tsx)) have
left enough rough edges to consider a redesign rather than another
audit pass:

1. **Hierarchy is flat.** Sidebar lists 12+ items at the same level
   — Peers next to Setup Keys next to Network Routes next to DNS
   next to Settings. Operators report "I never remember where X is."
2. **Brand violet underutilized.** The `--oz-violet-*` scale ships
   the right colors, but most surfaces stay neutral-gray. The brand
   identity reads on the wordmark + a few CTAs and disappears
   everywhere else.
3. **Tooling-driven shapes.** The tables / forms inherited from the
   upstream codebase optimise for max information density (small
   row height, dense filter strips). Operators who manage <100
   peers / <20 networks would benefit from a calmer layout that
   makes the state legible at a glance.
4. **Settings is monolithic.** Eight unrelated concerns
   (general / authentication / DNS / posture / JWT / networks /
   billing / audit) sit on a single scrolling page with no tabs.
5. **Error surfaces are bare.** 404 / 403 / 500 fall through to
   browser default or a half-styled blank — no guidance, no retry
   CTA, no request-id for support.

Claude Design delivered a redesign handoff in
`design_handoff_openzro_dashboard/` covering: a token system
(warm-paper light + violet-anchored dark), 17 screens including
new ones (Overview, Network Routes dedicated, Posture, Network
Detail, Error system), 4-section sidebar IA, 8-tab Settings
split, and reusable primitives (Card / Button / Pill / StatusDot
/ Toggle / KPICard / SegmentedTabs / Avatar).

We had two options to act on it:

- **Big-bang:** ship the redesign as a feature-flagged v2 on a
  branch, port every screen, flip default once done. Low review
  burden per PR but a multi-month branch divergence with high
  conflict risk against `main`.
- **Incremental, additive:** ship tokens + primitives + shell as
  parallel artefacts (`oz2-*` Tailwind classes, `src/components/v2/`
  primitives, `/v2-preview/*` routes) on a feature branch. Each
  production screen migrates as a separate PR once the v2 sandbox
  proves the pattern.

We chose the incremental path. Reviewability per PR + ability to
ship visual upgrades to operators without waiting for the whole
redesign was the deciding factor.

## Decision

Adopt the Claude Design v2 redesign as the destination for the
openZro dashboard, migrated incrementally over a 5-7-week focused
window (3-4 months calendar with parallel work). Preserve every
behavioural feature of the current dashboard during migration —
this is **a visual + IA refactor, not a feature deprecation**.

The dashboard `CLAUDE.md` rule about preferring **semantic CSS
variables** (`--oz-bg`, `--oz-primary`) stays. The new tokens
extend the same pattern under the `--ozv2-*` namespace
([`globals.css`](../../dashboard/src/app/globals.css)) with matching
Tailwind class aliases (`bg-oz2-surface`, `text-oz2-text-muted`,
`border-oz2-border-strong`, `shadow-oz2-md`, `rounded-oz2-card`).
Legacy `--oz-*` and `nb-gray-*` stay until the screen using them
migrates.

### Out of scope for the migration

The handoff includes screens / features that don't exist in the
current openZro dashboard. We will **not introduce them as part of
the migration** — they ship as separate features once the v2 base
is in place:

- **Overview** screen with throughput chart + onboarding checklist
  (new — needs new endpoints).
- **Network Routes** as a dedicated top-level screen (new — currently
  routes are nested under Networks).
- **Posture Checks** visualisation with coverage bars (new — needs
  posture-coverage API).
- **Network Detail** drill-down (new).
- **Error system pages** (new — useful to add but not blocking).
- KPI bands on screens that don't currently expose those KPIs
  (e.g. throughput on Peers list — would require new aggregation
  endpoint).

The migration sticks to: change visual + IA on the screens that
**already exist**, preserve every data-fetching path, action,
permission gate, and edge case. Once main reflects the v2 chrome,
new-screen work runs as separate ADR-tracked features.

### Migration phases

| Phase | Scope | Effort | Risk |
|---|---|---|---|
| **1. Tokens + primitives** | `--ozv2-*` CSS vars in `globals.css`, `oz2-*` classes in `tailwind.config.ts`, `src/components/v2/` for OzButton / OzCard / OzPill / OzStatusDot. | ~1 week | Low — additive, no existing screen touched. **Done on branch (commits 25ff83a2 + 7c5c18a0).** |
| **2. Shell** | OzShell / OzSidebar / OzTopbar / OzThemeToggle. Sidebar items kept identical to current routing — only the visual + 4-section grouping change. | ~1 week | Medium — touches the `(dashboard)/layout.tsx` when we flip. **Components done on branch (commit 29a2006f); flipping the layout is part of phase 4.** |
| **3. Screen prototyping** | One real screen rebuilt in v2 visual at `/v2-preview/<screen>` to validate the pattern. **Peers prototype done on branch (commits a7610a3c + 168ec626 + 714e9417 + 3a09e6a4).** | ~3-5 days per screen | Low — preview routes only, no production impact. |
| **4. Functional parity (per-screen)** | For each existing screen, port the v2 visual into the production route preserving data-fetching, actions, permission gates, bulk select, refresh, status badges. **Peers needs bulk select + refresh + Expiration/Login required badges before it can replace `/peers`.** | ~2-4 days per screen × 9 screens | Medium-high — touches real data paths; per-PR review. |
| **5. Settings split + cleanup** | Break monolithic `/settings` into 8 sub-pages. Delete legacy `DashboardLayout.tsx` / `Header.tsx` / `Navigation.tsx`, deprecated `Button.tsx` variants, `nb-gray-*` Tailwind palette, `oz-*` legacy tokens once nothing references them. | ~1 week | Low (after phase 4 lands) — pure deletion. |

Total: 5-7 weeks of focused work, 3-4 months calendar with one
front-end dev in parallel to other priorities.

### Per-screen migration order (phase 4)

Driven by **traffic + dependency**: most-visited screens first;
screens that share components migrate adjacently to avoid double-work.

1. Peers (most-visited; all primitives exercised; sets the pattern)
2. Networks (reuses Peers patterns; plus expand-row interaction)
3. Setup Keys (table + key creation modal; small but operationally critical)
4. Access Control (form-heavy, exercises the live-preview pane)
5. Users & Groups (tabs, two distinct table shapes)
6. Activity (timeline-style feed; new layout pattern)
7. Flow Traffic (chart-heavy; latest visual)
8. DNS (simple toggles + forms)
9. Integrations (tile grid)

Settings is phase 5 because the split changes routing.

### Functional gaps the v2 prototype must close

The v2 Peers prototype on the branch establishes the visual but
intentionally drops these production features pending phase 4. They
are **non-negotiable** for the production flip:

- **Bulk select** (header + per-row checkboxes) — operators need
  batch group-update / batch delete / batch admit flows.
- **Refresh button** — affordance to force a re-fetch without a
  page reload (operationally critical when peers transition state).
- **Status badges** — `Expiration disabled`, `Login required`,
  `Approval pending` are security-relevant signals; cannot be
  hidden behind the kebab.
- **`AdmissionBypassModal`** flow + `PeerActionCell` actions —
  these wire into real auth + permission gates.

Equivalent gaps will surface for the other screens during their
prototype phases. Each screen's PR explicitly lists the surface
parity check before merge.

## Rationale

### Why a redesign, not another audit pass

Light-theme issue tracking (#11) made it clear the existing token
system has hierarchical conflicts (the `nb-gray-*` scale was tuned
for dark; light mode is the inverse + tinted). The handoff's
`--ozv2-*` system is purpose-built for both modes from the start.
A full rewrite of the token system + per-screen audit pass would
take roughly the same effort as the redesign — but the redesign
gets us a sharper visual identity at the same cost.

### Why incremental and not big-bang

A 4-month branch diverging from `main` against an actively-developed
fork would conflict-hell every merge. Per-screen PRs ship review
work in 1-day chunks, get visual feedback fast, and don't block
unrelated work (geolocation mirror, certbot pipeline, relay
re-pick, etc) from continuing on `main`.

### Why preserve the existing data layer

The current `PeersProvider`, `UsersProvider`, `GroupsProvider`,
`ApplicationProvider`, etc. work. They wire SWR caches, auth, real-
time updates, permissions. Rebuilding that during a visual refactor
would multiply the risk and the timeline. The v2 components are
visually opinionated but data-shape-agnostic — they accept whatever
the existing hooks return.

### Why keep two token namespaces during migration

`--oz-*` legacy + `--ozv2-*` v2 coexisting means:

- Half-migrated dashboards always look coherent (each screen is
  fully one or the other, not a mix).
- Rollback per screen is `git revert` of one PR, no token-system
  surgery.
- The `nb-gray-*` Tailwind palette stays valid for the 166 components
  that hardcode it — no global find/replace pressure during phases.

When phase 5 deletes the legacy, the cleanup is mechanical (the
search shows zero references and the tokens drop out cleanly).

## Trade-offs

### What we accept

- **3-4 months of dual-namespace dev experience.** Two token systems
  in `globals.css`, two component directories (`components/` legacy +
  `components/v2/` new), two table styles in production until phase
  4 finishes. Mitigated by a clear per-screen "what's migrated"
  checklist in each phase-4 PR.

- **The handoff includes screens we'll skip.** Overview, Network
  Routes, Posture, Network Detail, Error system are out of scope.
  Operators who navigate to `/overview` after the visual flip will
  get a 404 — same behaviour as today (the route doesn't exist).
  Adding them is a separate post-migration roadmap item.

- **Brand identity shifts subtly.** Warm-paper `#fbfaf7` instead of
  pure white for light mode page bg; warmer borders (`#e9e5db`)
  instead of violet-tinted. Closer to the design handoff's
  Notion/Arc reference; further from the previous "violet
  everything" approach. Validated visually on the branch — feels
  like a brand maturation, not a brand change.

### What we don't accept (rejected alternatives)

- **Big-bang feature-flagged v2.** Rejected for the conflict-hell
  reason above. Not impossible, just expensive.
- **Token migration only (no IA / sidebar reorg).** Considered as
  a "lighter" path. Rejected because the IA gap (flat 12-item
  sidebar) is the bigger UX win. Token-only would be 30% of the
  benefit at 50% of the work.
- **Adopt the handoff's new screens during migration.** Tempting
  but expands scope dramatically; we'd be re-implementing data
  pipelines (KPIs, charts) AND visuals at once, doubling the risk.

## Validation done so far

The decision was made after building the prototype on
`feat/dashboard-redesign-v2`. Branch state at decision time:

- **Phase 1** complete: tokens in `globals.css`, oz2-* classes in
  Tailwind, OzButton / OzCard / OzPill / OzStatusDot primitives.
  Demo at `/v2-preview` shows all four with light + dark variants.
- **Phase 2** complete: OzShell + OzSidebar + OzTopbar +
  OzThemeToggle. Demo at `/v2-preview` renders the full chrome.
- **Phase 3** prototype done for Peers: `/v2-preview/peers`
  reproduces the production page's information density with mock
  data, exercises every cell type (status, OS, country flag,
  group cluster, P2P/Relay pill, kebab menu), uses real toolbar
  controls (search, group multi-select dropdown, page-size
  combobox, segmented status tabs, prev/next pager).

Visual + IA validated against the production `/peers` screenshot
side-by-side. The v2 reads better on first parse; production has
more functional features (bulk select, refresh, status badges)
that phase 4 must absorb.

Branch lives in this repo (not merged to main). Commits:

- [`25ff83a2`](https://github.com/openzro/openzro/commit/25ff83a2) — v2 design tokens
- [`7c5c18a0`](https://github.com/openzro/openzro/commit/7c5c18a0) — v2 primitives + preview
- [`29a2006f`](https://github.com/openzro/openzro/commit/29a2006f) — v2 shell
- [`a7610a3c`](https://github.com/openzro/openzro/commit/a7610a3c) — v2 Peers preview
- [`168ec626`](https://github.com/openzro/openzro/commit/168ec626) — country, group filter, tooltips
- [`714e9417`](https://github.com/openzro/openzro/commit/714e9417) — User column, Connection pill, paginação, kebab, tabs
- [`3a09e6a4`](https://github.com/openzro/openzro/commit/3a09e6a4) — toolbar tweaks (page-size next to groups, Add peer in topbar)

## Open questions

- **Feature flag for the production flip?** When phase 4 PRs land,
  we have two options: each PR replaces the screen unconditionally
  (operators see the v2 immediately on the next chart bump), or we
  ship under a `?dashboard=v2` localStorage flag and flip default
  when phase 5 cleanup completes. Decide before the first phase-4
  PR — flag is cheap to add but adds a 2-month dual-render burden.
- **Light-theme audit (#11) merge timing.** That issue tracks
  fixes to the legacy `nb-gray-*` scale. Worth merging it (one
  more PR for users on the legacy paint) or rolling its fixes
  into phase 4 (one PR, less work)? Suggest the latter — saves a
  pass.
- **Existing `Button.tsx` deprecation.** The 11-variant legacy
  Button stays until phase 5. Worth marking it `@deprecated`
  in JSDoc during phase 4 to discourage new uses, or wait until
  the migration is complete? Suggest deprecating early.

## References

- Design handoff: `design_handoff_openzro_dashboard/` (delivered by
  Claude Design — README at the root of that bundle is the
  authoritative spec)
- Branch: `feat/dashboard-redesign-v2`
- Predecessor brand work: [`brand/openzro-icon.svg`](../../brand/openzro-icon.svg),
  the root [`CLAUDE.md`](../../CLAUDE.md), and the dashboard
  [`CLAUDE.md`](../../dashboard/CLAUDE.md) — the violet scale and
  wordmark rules from those docs are preserved verbatim by the v2
  tokens.
- Related issues: #11 (light theme audit, will be subsumed by
  phase 4), #12 (Groups page polish, schedule for phase 4 with the
  Users & Groups screen).
