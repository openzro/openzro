# ADR-0017 — Time-based posture check (`ScheduleCheck`)

**Status:** Proposed (2026-05-11)

**Relates to:**
[ADR-0003](0003-device-admission-gate.md) (posture-check framework),
[ADR-0014](0014-coordinated-multi-pod-relay.md) (cluster
coordinator + leader election),
[ADR-0008](0008-kubernetes-helm-operator.md) (HA management
deployments).

**Supersedes:** none.

## Context

openZro's posture-check framework (inherited from NetBird) is a set
of typed predicates that gate a policy's source peers. Today we
ship six check types — NB version, OS version, geo-location, peer
network range, process running, MDM/EDR compliance — all defined in
[`management/server/posture/`](../../management/server/posture/)
behind the `posture.Check` interface
([checks.go:32](../../management/server/posture/checks.go#L32)):

```go
type Check interface {
    Name() string
    Check(ctx context.Context, peer nbpeer.Peer) (bool, error)
    Validate() error
}
```

Each implementation is **stateless** — the same `peer` snapshot
always produces the same verdict. Evaluation happens during network
map computation (see
[`types/account.go::validatePostureChecksOnPeer`](../../management/server/types/account.go#L1119)),
which itself runs in response to peer sync, policy changes, peer
adds/removes, etc.

A Acme operator asked for a check that says **"group X can only
reach internal-system between 09:00 and 17:00 on weekdays"** — i.e.
a posture predicate that depends on wall-clock time. The use case
is small but recurrent (compliance-driven schedule restrictions,
restricting after-hours admin access, time-boxing contractor
groups). It does not exist in any open-source ZTNA today; Tailscale,
Twingate, and Cloudflare Access all gate on time only through their
proprietary policy engines.

This ADR proposes a new check type, `ScheduleCheck`, and the
plumbing required to make a *time-dependent* predicate work under
the stateless framework above.

## What makes this non-trivial

Three things separate `ScheduleCheck` from the existing six:

1. **Time is a moving input.** A network map computed at 16:59 is
   stale at 17:01 — the peer's verdict flipped without anything
   else changing. We need a re-evaluation trigger that fires at
   window boundaries.
2. **Time zones and DST are real.** Brazil dropped DST in 2019;
   the EU's debate keeps it on the table; Australia, the US, and
   Chile all observe it on different dates. We must pick an
   authoritative time-zone model and design against the two known
   pathologies — **spring forward** (a window can contain
   non-existent wall times) and **fall back** (a window can
   contain the same wall time twice).
3. **HA deployments** ([ADR-0008](0008-kubernetes-helm-operator.md))
   run multiple management replicas. A naïve "tick every minute
   and refresh peers" loop running on every replica would fan out
   N× duplicate updates at every boundary.

## Decision

We add `posture.ScheduleCheck` as the seventh first-class check
type, with the following choices:

### 1. Timezone model: IANA names, required

`ScheduleCheck.Timezone` is a string IANA name (e.g.
`"America/Sao_Paulo"`, `"Europe/Berlin"`, `"UTC"`). It is
**required** — there is no implicit default.

- IANA names cover DST automatically through the embedded `tzdata`
  package. Go's `time.LoadLocation` consults the IANA tz database;
  the management binary already imports `_ "time/tzdata"` via
  upstream so the tz database ships in the binary and works even
  in scratch containers.
- We deliberately **reject** fixed offsets (`UTC-03:00`) because
  operators who type "UTC-3" typically mean São Paulo *with* DST,
  and shifting to fixed offset silently breaks twice a year.
- We deliberately **reject** an account-level default time zone.
  Operators can manage devices across regions; the timezone
  belongs to the **rule**, not the operator. The dashboard pre-
  fills with the browser's resolved `Intl.DateTimeFormat().resolvedOptions().timeZone`
  as a convenience, but the operator must confirm.

### 2. Window shape: weekday + `HH:MM` start/end

```go
type ScheduleCheck struct {
    Timezone string           // IANA, required
    Action   string           // "allow" | "deny"
    Windows  []ScheduleWindow // OR-combined, non-empty
}

type ScheduleWindow struct {
    Days  []string // ["mon","tue","wed","thu","fri","sat","sun"], non-empty
    Start string   // "HH:MM" 24h, in Timezone
    End   string   // "HH:MM" 24h, in Timezone
}
```

Semantics:

- **Action** mirrors `GeoLocationCheck`
  ([geo_location.go:29](../../management/server/posture/geo_location.go#L29)):
  - `allow` → peer passes the check **inside** any window,
    fails outside.
  - `deny` → peer fails the check **inside** any window,
    passes outside.
- **Days** is the set of weekdays the window applies to. The
  weekday is determined by the **start instant** in the
  configured time zone — i.e. an overnight window `22:00-06:00`
  on `["mon"]` covers *Monday 22:00 → Tuesday 06:00*. This
  matches operator intent ("Monday evening shift").
- **Start / End** are wall-clock 24h `HH:MM`. The interval is
  **half-open**: `[Start, End)`. A window `09:00-17:00` is
  *in* at 09:00:00 and *out* at 17:00:00.
- **End < Start** is legal and means the window crosses
  midnight (`22:00-06:00`). `End == Start` is rejected (we
  treat that as ambiguous; operator who wants 24-hour coverage
  uses `00:00-23:59` or a single window per day with `End=23:59`).
- **Multiple windows OR**: the peer is "inside" if any window
  matches. This is how operators express split schedules
  ("09:00-12:00 and 14:00-17:00") and per-day variation
  ("Mon-Fri 09:00-17:00 + Sat 09:00-13:00" = two windows).

Concrete examples the dashboard ships as quick-presets:

| Preset | Windows |
|---|---|
| Business hours (Mon-Fri 09-17) | `[{days: mon-fri, 09:00-17:00}]`, action=allow |
| No after-hours admin | `[{days: mon-sun, 22:00-06:00}]`, action=deny |
| Weekday only | `[{days: mon-fri, 00:00-23:59}]`, action=allow |
| Contractor shift | `[{days: mon-fri, 09:00-12:00}, {days: mon-fri, 14:00-17:00}]`, action=allow |

### 3. DST handling: trust the IANA tzdata, document the corners

We use `time.LoadLocation(tz)` once at check time, then
`time.Now().In(loc)` to get the wall clock in the configured
zone. The window comparison is a pure wall-clock test against
the resulting `time.Time`. This means:

- **Spring forward** — e.g. America/New_York on `2026-03-08`,
  02:00 advances to 03:00. A window of `02:30-03:30` on Sundays
  loses its first 30 minutes that day (02:30 doesn't exist).
  The window is still honored 51 weeks of the year; only that
  one Sunday has 30 fewer minutes of coverage.
- **Fall back** — same zone on `2026-11-01`, 02:00 falls back
  to 01:00. A window of `01:30-02:30` covers 1h30m of wall-time
  on Saturday → Sunday, but two hours of real time. Operators
  who care must split the window or pick a different zone.
- We **do not** try to detect or warn about DST overlap at
  validation time. The wall-clock model is intuitive; mis-aligned
  windows are operator error, not framework bug. The
  documentation calls this out explicitly so it's not surprising.

Test coverage for DST cases is non-optional — see "Rollout" below.

### 4. Re-evaluation trigger: per-account scheduler with cluster
   leader election

Re-evaluation needs to happen exactly once per window-boundary per
account. The design:

1. **In-process scheduler** runs on every management replica that
   passes the cluster leader-election lock for an account. The
   lock primitive is `cluster.Coordinator.Lock`
   ([cluster/coordinator.go](../../cluster/coordinator.go)) with
   key `posture-scheduler:{accountID}` and a 90-second TTL,
   refreshed every 30 seconds.
2. **Wakeup time** is computed as the **earliest upcoming
   boundary** across all `ScheduleCheck`s attached to active
   policies in the account. The scheduler sleeps with a
   per-account timer; on fire, it calls
   `am.updateAccountPeers(ctx, accountID)`
   ([account.go](../../management/server/account.go)) which is
   the same path used by policy/peer change events today. The
   peer update fans out through `PeersUpdateManager.SendUpdate`
   and the existing `peerUpdateTopicPrefix` cluster pub/sub
   ([updatechannel.go:34](../../management/server/updatechannel.go#L34)).
3. **Lock holder change** (e.g. replica restart, replica crash)
   is handled by the lock TTL — the new holder reads current
   account state and recomputes the next wakeup. Boundaries
   missed during the gap (< 90 s) are lost; we accept this
   because the next sync from any affected peer will re-evaluate
   posture in line.
4. **Schedule edits** (operator saves a `ScheduleCheck` or attaches
   it to a policy) publish a per-account invalidation message on
   the existing
   [`updatechannel`](../../management/server/updatechannel.go)
   bus. The scheduler holding the lock picks it up and
   recomputes the next wakeup. (For single-replica deployments,
   this is just an in-process channel.)

Alternatives considered and rejected:

- **Global 60-second tick on every replica.** Trivial to write,
  but every replica recomputes and pushes an update, so HA
  triples (or quintuples) the per-boundary update fan-out. Also
  wakes every minute even when no boundary is imminent.
- **Per-policy goroutine.** O(policies × replicas) goroutines.
  Easy to leak. Rejected.
- **Push the timer into the client/agent.** The client gets to
  decide if it's inside the window. Rejected: operators must
  control the wall clock authoritatively, otherwise a peer that
  has rolled its clock backward bypasses the policy. Server-side
  clock is canonical.
- **Run the scheduler outside management.** A sidecar or
  separate "policy clock" service. Rejected as overkill for
  v1 — the scheduler is ~200 LOC and shares all its inputs
  with management already.

### 5. Storage and API shape

The `ChecksDefinition` struct
([checks.go:56](../../management/server/posture/checks.go#L56))
gets a new optional pointer field:

```go
type ChecksDefinition struct {
    NBVersionCheck        *NBVersionCheck        `json:",omitempty"`
    OSVersionCheck        *OSVersionCheck        `json:",omitempty"`
    GeoLocationCheck      *GeoLocationCheck      `json:",omitempty"`
    PeerNetworkRangeCheck *PeerNetworkRangeCheck `json:",omitempty"`
    ProcessCheck          *ProcessCheck          `json:",omitempty"`
    EndpointSecurityCheck *EndpointSecurityCheck `json:",omitempty"`
    ScheduleCheck         *ScheduleCheck         `json:",omitempty"` // NEW
}
```

Because the entire `ChecksDefinition` is GORM-serialized as a
single JSON column on `posture_checks`
([checks.go:52](../../management/server/posture/checks.go#L52)),
this is a **zero-DDL** change. Old rows with no `ScheduleCheck`
unmarshal cleanly into the new struct.

The OpenAPI spec (`management/server/http/api/`) gets a parallel
`ScheduleCheck` type. The generated Terraform provider follows
suit on next regen.

### 6. Dashboard UX

A new card in the posture-check editor, matching the existing
violet-on-paper styling. Components from the v2 design system
(`oz2-*` tokens, OzTabs, OzCard):

- **Timezone picker** — searchable IANA dropdown. Defaults to
  `Intl.DateTimeFormat().resolvedOptions().timeZone`. Common
  zones pinned to the top.
- **Action toggle** — `Allow during these windows` / `Deny
  during these windows`, mirroring the geo-location editor.
- **Window editor** — a `Add window` button below a list of
  rows. Each row: weekday multi-select chips (Mon–Sun) +
  start/end time inputs (`HH:MM`).
- **Visual grid** — a 7×24 heatmap that shows the resulting
  in-window cells in `oz2-acc-soft`, with the current
  hour-of-week pulsing if it falls inside.
- **Now indicator** — small text under the timezone picker:
  "It is currently **inside** the schedule" /
  "It is currently **outside** — peers in this group would
  be denied". Pure client-side, informational only.

The card slots into the existing posture-check tab in
`PolicyEditorBody`
([dashboard/src/modules/access-control/v2/PolicyEditorBody.tsx](../../dashboard/src/modules/access-control/v2/PolicyEditorBody.tsx)).

### 7. Activity log

Failed admissions and policy-time gating record an
`activity.PolicyPostureFailed` event with metadata:

```json
{
  "check_type": "ScheduleCheck",
  "check_name": "Business hours only",
  "reason": "outside permitted hours",
  "timezone": "America/Sao_Paulo",
  "evaluated_at": "2026-05-11T22:14:03-03:00"
}
```

Existing log surface
([management/server/peer.go::processPeerPostureChecks](../../management/server/peer.go#L1058))
already records check-name failures; we extend the metadata.

## Edge cases enumerated

| Case | Behavior |
|---|---|
| `End == Start` | Validation error. "End must differ from Start." |
| `Days` empty | Validation error. "Select at least one weekday." |
| Invalid IANA tz | Validation error at save (server-side `time.LoadLocation` probe). |
| `Start`/`End` not `HH:MM` | Validation error. |
| Spring forward window | Window simply has 60 fewer minutes on that one Sunday. Documented; no warning. |
| Fall back window | Window covers two real-time hours. Documented; no warning. |
| Peer clock skew | Irrelevant — server clock is authoritative. |
| Management replica clock skew | Operators must run NTP. We do not compensate. The leader-election lock indirectly mitigates: only one replica computes per account, so divergent replicas don't fan out. |
| Posture-check evaluation race at boundary | The next `validatePostureChecksOnPeer` call evaluates against `time.Now()` at that moment. If a sync arrives at 16:59:59.999, the peer passes; at 17:00:00.001 it fails. We accept the millisecond race — there is no practical way to be "atomic at the boundary" without coordinating clients. |
| Empty `Windows` after deserialization | Validation error. ScheduleCheck must have at least one window. |
| 24-hour coverage | Operator uses `00:00-23:59` (we document the 1-minute gap, or recommend `End=00:00` with overnight semantics on `mon-sun`). The validator accepts `00:00-23:59`. |
| Holidays / one-off exceptions | **Out of scope for v1.** A future v2 can add an `Exceptions []Date` list; the schema field is reserved. |
| Cron / iCal RRULE | **Out of scope.** Operator-hostile in the UI; we'll revisit only if customers ask. |

## Test plan

TDD-first per project rule. Tests live alongside
`management/server/posture/schedule.go`:

**Unit (pure)** — table-driven against a `nowFn` package var the
test overrides:

1. Window inclusivity at exact boundary (`16:59:59.999` in,
   `17:00:00.000` out).
2. Overnight window correctness (`22:00-06:00` on Mon means
   in at Mon 22:30 + Tue 05:30, out at Tue 06:00).
3. Multi-window OR.
4. Action=allow + outside-all-windows → fail.
5. Action=deny + outside-all-windows → pass.
6. Spring-forward DST: America/New_York 2026-03-08; window
   `02:30-03:30` on Sundays; at 03:00:00 real time the peer
   is "in window" (wall clock 03:00 in zone).
7. Fall-back DST: America/New_York 2026-11-01; window
   `01:30-02:30` on Sundays; both 01:30 EDT and 01:30 EST
   are "in window".
8. Brazil 2026 (no DST): America/Sao_Paulo on 2026-10-15
   stays at UTC-3 across the day (regression for
   pre-2019 expectations).
9. Invalid timezone string at `Validate()`.
10. End < Start across midnight is valid; End == Start is not.

**Integration** — uses the existing testcontainers Postgres
([`store_test.go`](../../management/server/store/sql_store_test.go)):

1. Save → load → re-save round-trips ScheduleCheck fields.
2. Attach ScheduleCheck to a policy, advance the package
   clock past a boundary, assert `updateAccountPeers` runs.
3. HA: with two `cluster.Coordinator` instances, only one
   acquires the lock and only one triggers the refresh.

**Property-style** — for any valid (Timezone, Windows) pair,
the set of in-window instants in a 7-day span is closed under
the `WithinWindow(t)` predicate (no off-by-one gaps).

## Rollout

Implementation order, each step its own commit:

1. **Backend, posture package** — `posture/schedule.go` with the
   struct, `Check` interface implementation, `Validate()`.
   Failing tests first; minimum that compiles after.
2. **Backend, plumbing** — wire into `ChecksDefinition.Copy`,
   `GetChecks`, the API translator
   ([checks.go:186](../../management/server/posture/checks.go#L186)),
   the API response builder.
3. **OpenAPI** — add `ScheduleCheck` to the spec; regenerate
   `api/types.gen.go`. Manual review of the diff.
4. **Backend, scheduler** — new package
   `management/server/posture/scheduler/` with leader-elect,
   per-account timer, recompute on save. Integration test
   for the boundary trigger.
5. **Activity log metadata** — extend
   `processPeerPostureChecks` failure record.
6. **Dashboard, v2** — new editor card, timezone picker
   component, heatmap visual, now-indicator.
7. **Docs** — operator guide section under
   [`docs/operator/posture-checks.md`](../../docs/operator/posture-checks.md)
   with DST corner-case examples.
8. **Helm chart appVersion bump** + release notes call-out.

Estimated total: ~1.5 days backend (steps 1-5), ~1 day dashboard
(step 6), ~half day docs + chart (steps 7-8). The scheduler is
the only non-trivial piece; everything else is pattern-matching
against the existing five check types.

## Why this is worth doing

- Differentiates openZro from upstream NetBird (which has no
  time-based gating).
- Closes a Acme-asked compliance feature that maps directly to
  Bacen Res. BCB nº 4.893's "least-privilege at the right time"
  ([docs/compliance/bacen-4893-mapping.md](../../docs/compliance/bacen-4893-mapping.md)).
- Reuses 100% of the posture-check authoring + audit surface,
  so the marginal UX cost is one card.
- Sets up the scheduler primitive that future
  state-changes-over-time features (rotating credentials, scheduled
  policy version rollovers) can ride on.

## Open questions for the reviewer

1. **Default action.** Should the dashboard pre-fill `allow`
   ("allow during these hours") or `deny` ("deny during these
   hours")? The former is the more common ask; the latter is
   safer-by-default. Leaning `allow`.
2. **Per-window timezone.** Should `Timezone` be on the window
   (one zone per window) or on the check (one zone for all
   windows)? Proposal: check-level, simpler. If an operator
   needs cross-zone, they create two checks.
3. **Calendar import.** Should the v2 editor support importing
   a `.ics` calendar to seed windows? Probably v2.
4. **gRPC backpressure.** At a boundary that affects 10 000
   peers in one account, we'll enqueue 10 000 `SendUpdate`
   calls in a burst. The per-peer channel buffer is 1 000
   ([updatechannel.go:25](../../management/server/updatechannel.go#L25)).
   We should measure but the existing `updateAccountPeers`
   already does this for policy edits — proven path.

## Decision drivers ranking

1. Operator can express the rule in <30 s through the dashboard.
2. Server is canonical on time; client cannot bypass.
3. Zero-DDL; clean rollback (delete the field, drop the goroutine).
4. HA-safe by construction, not by convention.
5. Test coverage proves DST corners actually work.
