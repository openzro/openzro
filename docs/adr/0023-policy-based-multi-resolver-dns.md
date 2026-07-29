# ADR-0023 — Policy-based DNS response selection for multi-resolver zones

## Status

**Proposed**. Design gate for the feature proposed and welcomed in
[openzro/openzro#140](https://github.com/openzro/openzro/issues/140)
(maintainer `klinux`). **No code lands with this ADR.** Naturally extends
[ADR-0022](./0022-custom-dns-zones.md) (Custom DNS Zones): 0022 makes the
agent authoritative for its *own* zones; 0023 chooses between several
*upstream* answers for a shared zone. The two tiers are orthogonal.

Scope confirmed on #140: **client-only, opt-in, zero-regression**; per-domain
policy and any management/proto/dashboard surface are explicitly v2.

## Context

The full problem statement, evidence, and cross-cloud generalization live in
#140 and are not repeated here. In short: when a zone is served by more than
one resolver and each returns a *valid but different* answer (split-horizon
Private Link across independent Azure tenants / AWS accounts / GCP projects),
openZro cannot select the right one, and no static config can (names aren't
enumerable, front-rewriting breaks TLS/`Host`, the apex zone legitimately has
multiple owners). The correct answer is **per-name, not per-zone**.

**Verified current behavior** (openZro tree at time of writing):

- `upstream.go:126-159` (`ServeDNS`) tries a group's servers **sequentially
  and returns on the first success**; on total failure writes `SERVFAIL`
  (not the chain continue-signal).
- `server.go:648` `groupNSGroupsByDomain` already groups NS-groups per domain;
  `server.go:666-671` `createHandlersForDomainGroup` then emits **one handler
  per group at `priority = basePriority - i`**. The chain
  (`handler_chain.go:13-19`, `PriorityUpstream=50`) dispatches highest-first,
  so a second same-zone group is **never consulted**.
- There is **no operator-facing priority/weight field** on a nameserver group
  (`dns/nameserver.go`: `ID, Name, NameServers, Groups, Primary, Domains,
  Enabled, SearchDomainsEnabled`). The handler "priority" is derived from the
  incidental list order management delivers — deterministic per config
  snapshot, but not a designed control. This constrains D4.

**Prior art & license.** Upstream NetBird's BSD-3 client (HEAD `8a43f4f`)
already merges same-zone groups (`buildMergedDomainHandler`) and races them
(`raceAll`), taking the **first valid** answer. That is the *mechanical*
substrate we need but the **opposite** of the selection we need ("chosen, not
raced"). `client/internal/dns/` is BSD-3 in both trees, so per the maintainer
and our engineering rules we **may adapt NetBird's code with attribution** —
full clean-room is not required here. See D7 for exactly how much we adapt.

## Decision

### D1 — Pipeline architecture and vocabulary (klinux #2)

Three components with a strict boundary — the resolver stays dumb, all
intelligence is in the selector:

- **`ResolverPool`** — holds the candidate resolvers for one zone, fans a
  query out, collects answers. **No** selection logic.
- **`CandidateResponse`** — one resolver/group's answer, in a struct
  deliberately shaped to be extended without breaking the interface (D11):

  ```go
  type CandidateResponse struct {
      Resp    *dns.Msg
      Source  ResolverID     // group/upstream identity — identifier, not secret
      Latency time.Duration
      // reserved for v2 signals (D11): ZoneTag, RouteCtx
  }
  ```
- **`ResponseSelector`** applies the configured `SelectionPolicy`, a **pure
  function** of the collected candidates (no I/O → table-testable):

  ```go
  type SelectionPolicy interface {
      Name() string
      Select(q dns.Question, cands []CandidateResponse) (*dns.Msg, bool)
  }
  ```

The pool never learns about policies; the selector never issues queries. This
invariant is what makes later strategies additive (D11).

### D2 — v1 ships exactly two policies

- **`first_success`** — default; behavior- and cost-identical to today (D5).
- **`prefer_private`** — opt-in; the split-horizon case (D3).

Everything else (per-domain, `prefer_routed`, explicit tag, other policies) is
v2 (D11), enabled by the interface but not built now.

### D3 — `prefer_private` signal and selection rule (klinux #1)

"Private" is **best-effort** and answer-based. The predicate on an A/AAAA
record is `addr.IsPrivate() || isInCGNATRange(addr)` — the CGNAT `100.64.0.0/10`
term is required because openZro peers live there and `IsPrivate()` excludes
it. Reuse the existing helper (klinux points to
`client/anonymize/anonymize.go:239-242`; verify the canonical location at
implementation time). Documented as best-effort in the config help.

Selection is **lexicographic by tier**, over candidates gathered per D5:

1. **Private** — responses whose answer holds a private A/AAAA. If any, pick
   among them by the D4 tiebreak. Done.
2. **Public** — responses with a public A/AAAA. If any (and no private), pick
   by D4 tiebreak. **This is the safe fallback: never worse than today.**
3. **Negative** — only `NXDOMAIN`/`NODATA` responses → return one
   deterministically. (A positive answer always beats a negative one, so a
   tenant that `NXDOMAIN`s a name never masks the tenant that owns it.)
4. **None** — all resolvers failed (`SERVFAIL`/timeout) → `SERVFAIL`, as today.

Only A/AAAA queries engage this (D5). `NXDOMAIN` is never *preferred*; it only
wins when it is the only thing on offer.

### D4 — Tiebreak and determinism (klinux #1)

The outcome must be a function of the *set* of answers, not of network arrival
order. Within a tier, tiebreak by a **stable key** (the source resolver's
address). This is **explicitly a reproducibility mechanism, not an operator
preference** — the incidental list-order "priority" (Context) is *not* used as
a preference signal, because the operator cannot control it today. A
controllable preference needs an explicit weight/tag field → v2 (D11).

Multiple private answers for one name is an **ambiguous topology** (blob
account names are globally unique, so it is rare and usually a
misconfiguration). We pick deterministically by the stable key **and log a
warning** (`ambiguous: N private answers for <name> from X,Y — picked X`),
mirroring ADR-0022's search-domain-conflict warning. The *meaningful*
disambiguator is reachability — see `prefer_routed` in D11.

### D5 — Gather semantics (klinux #4)

Two paths, chosen by policy:

- **`first_success`**: the exact `upstream.go:126-159` sequential-return-on-
  first path. No fan-out, no extra latency. **Zero regression.**
- **Selection policies**: fan out **only when all of** — opt-in on (D8),
  the zone has **>1 resolver**, and qtype is **A or AAAA** — hold; otherwise
  fall back to the `first_success` path. When engaged: query the same-zone
  resolvers concurrently (one worker per group; within a group, sequential as
  today), collect one `CandidateResponse` per group.

Bounding and correctness:

- **Grace window** — a **ceiling** on how long the pool waits for slower
  answers before deciding, a different and much smaller time scale than the
  per-exchange `UpstreamTimeout` (15 s, unchanged; `upstream.go:29`). Tying
  the two would let one laggard stall a query for seconds. v1 uses a
  **hardcoded named constant (~500 ms)**; because the pool decides as soon as
  all outstanding resolvers have responded (below), this ceiling is only hit
  when a resolver is genuinely slow — the common path returns in the tens of
  ms the answers actually take. Configurability is deferred (cheap, no
  interface change). Implementation note: prefer anchoring it as a *settle
  window after the first usable answer* rather than absolute-from-query-start,
  so the **added** latency is bounded regardless of how slow the first answer
  is.
- **Deterministic gather → decide:** the selector runs once the pool has
  either collected all outstanding answers **or** the window elapsed. An
  early return is permitted **only when the outstanding resolvers cannot
  change the tier-1 outcome** (a private answer is in hand *and* every other
  resolver has already responded). In the common single-private case the
  outcome is timing-independent, so this stays fast; strict determinism costs
  latency only in the multi-resolver-slow case, bounded by the window.
- **Partial failure:** a slow/failing resolver must not block the decision
  (that is the window's job); the selector decides on whatever valid answers
  arrived. All-fail → `SERVFAIL`.

### D6 — Keep openZro's health model (zero-regression guardrail)

openZro's per-group `deactivate`/`reactivate` + `failsCount`/
`checkUpstreamFails` (`upstream.go:175`, `server.go:715`) is **retained**,
tracked **per source** inside the pool. We do **not** adopt NetBird's replaced
health model (it dropped these for a `markUpstreamFail` + status-projection
rework — see D7). A pool with all sources deactivated behaves exactly as the
group being absent today. This is the primary implementation-correctness risk
and is covered by regression tests.

### D7 — Implementation approach: adapt the pattern, not the subsystem (klinux #2/#4)

NetBird's `raceAll` is a **connected cluster** (`upstreamRace`, `raceResult`,
`upstreamFailure`, `tryRace`, `queryUpstream`, `markUpstreamFail`, `resutil`,
EDNS0 and RFC 8914 extended-error handling, a changed `newUpstreamResolver`
signature, **and a
replaced health model**). Taking it wholesale would be a large,
behavior-changing rework that breaks the agreed minimal/zero-regression scope.

Therefore: **reimplement the fan-out skeleton** (goroutine per group, buffered
result channel, per-attempt `r.Copy()`, context cancellation of losers) **on
openZro's existing types, keeping openZro's health model (D6)**, and **replace
"first valid wins" with gather-then-select (D5)**. This is an *adaptation of
the BSD pattern, attributed in the commit* — not a port of the subsystem. The
`ResponseSelector`/`SelectionPolicy` layer and the opt-in config (D8) are
original (NetBird has no equivalent).

### D8 — Opt-in surface (klinux #3)

A single **client-side, opt-in** config field selects the policy, default
`first_success`. It is **local agent config** (CLI flag / config file), **not**
distributed from management — no proto/dashboard. Threaded
`config → DefaultServer → ResolverPool`; **not** a package-level global
(openZro forbids new global mutable state). Absent/empty ⇒ today's behavior
exactly.

### D9 — Other invariants

- **Cache** unchanged; selection runs only at gather time, no per-policy cache
  keys in v1.
- **Trust:** candidates are admin-configured nameserver groups. A malicious
  resolver could return a spoofed private address, but it can already win
  outright today — no new attack surface; documented.

### D10 — Merge point in code

`createHandlersForDomainGroup` produces **one `ResolverPool` handler per zone**
(replacing the N descending-priority handlers) at `PriorityUpstream`. The
handler chain above is unchanged; the gather→select stages live inside the
pooled handler. Custom-zone (`PriorityLocal=100`, ADR-0022) still terminates
first — untouched.

### D11 — Forward-compat (klinux #5)

The `CandidateResponse` struct and the `SelectionPolicy` seam are shaped so v2
is additive, no redesign:

- **per-domain policy** — a selector picker keyed by `q.Name`.
- **explicit "internal zone" tag** — a more authoritative signal than
  best-effort private-IP; the pool tags candidates from tagged resolvers
  (`ZoneTag`), a policy reads it. Needs management/proto/dashboard → v2.
- **`prefer_routed`** — prefer the answer whose IP is covered by a live mesh
  route (reachability, the meaningful disambiguator for D4's ambiguous case).
  Reads `RouteCtx`. Feasibility is already evidenced upstream: NetBird's merged
  handler carries `selectedRoutes` (`server.go:926` in `8a43f4f`).

### D12 — License posture

`client/internal/dns/*.go`, **BSD-3**. Fan-out skeleton adapted from NetBird's
BSD client **with attribution in the commit**; selection layer + config
original. No AGPL paths involved.

## Non-goals / v2 (separate ADR if pursued)

Per-domain / per-resolver policy; explicit "internal zone" tag; `prefer_routed`
and other policies; controllable preference/weight field; policy-aware caching;
any management/proto/dashboard change; adopting NetBird's reworked health and
extended-error subsystem.

## Risks accepted

- **Fan-out latency** up to the grace window, bounded by D5's gates; opt-in
  only; `first_success` pays nothing.
- **Grace-window value** (~500 ms) may need tuning → a later config knob, no
  interface change.
- **Health-model preservation** (D6) through the merge — the main correctness
  risk; regression-tested.
- **best-effort private predicate** (D3) — spoofable private answer (D9,
  accepted) and cases where the private-link IP is not RFC1918/CGNAT (rare);
  `prefer_routed` (D11) is the robust long-term answer.

## Verification

TDD, table-driven, co-located, written before the implementation. The selector
is a pure function → exhaustive tables with no network:

```bash
# prefer_private picks the private answer even when slower/lower in list order;
# falls back to public when none private; negative never beats positive;
# multiple-private → deterministic by stable key + warning logged;
# non-A/AAAA and single-resolver zones bypass fan-out (unchanged);
# first_success is byte-for-byte the pre-change path;
# per-source health deactivation still fires under parallel gather.
go test -race -timeout 5m -count=1 ./client/internal/dns/... \
  -run "Selector|Policy|PreferPrivate|FirstSuccess|ResolverPool|Upstream|Health"
golangci-lint run --timeout=12m ./client/internal/dns/...
make fmt.check
```

Plus a regression test that **fails on `main`** (two same-zone groups, private
answer from the slower group; assert the private answer is returned) and stays
in the tree. Pre-PR: working-tree test image (`Dockerfile.testbuild` →
`hri4711/openzrotest`), smoke-tested on a peer with two NS-groups for one zone.

## Implementation notes

Append-only. Added while implementing the decisions above; the sections before
this one are left as they were accepted.

### Two components, not three (D1)

D1 draws the pipeline as resolver pool → `ResponseSelector` → `SelectionPolicy`.
The code has no `ResponseSelector` type: the pool hands the gathered candidates
straight to the policy. A separate selector would have been a shell doing nothing
but forwarding, while the part that is genuinely shared between policies — the
quality ladder and the ranking spine with its deterministic tiebreak — lives in
the selection package as internal helpers every policy reuses.

The boundary D1 exists for is unchanged and is what the seam is worth: the
resolver never learns about policies, and a policy never does I/O and never logs.
Both are enforced by construction — the policies are pure functions of the
candidate list, which is why they are exhaustively table-tested without a network.

`CandidateResponse.Latency`, which D1 prints as part of the struct, is not
carried: no policy uses it, and adding a field later is source-compatible for
callers that construct candidates by field name. The struct's documented purpose
of taking further signals (D11) is unaffected.

### The response ladder (D3)

D3 orders four classes: private > public > negative > none. The implementation
splits that into a **policy-independent ladder** plus a **policy refinement**,
because "negative" turned out to be several different things whose order the ADR
does not decide:

    no reply < error reply < NXDOMAIN < NODATA < off-topic < referral < answer

- **No reply < error reply.** A SERVFAIL or REFUSED *reply* is content — it can
  carry an RFC 8914 extended DNS error. Forwarding the upstream's own failure
  beats synthesizing a bare SERVFAIL, so a real error reply outranks a resolver
  that never answered.
- **Error reply < NXDOMAIN.** A definitive "this name does not exist" beats a
  server failure.
- **NXDOMAIN < NODATA.** The name exists but carries no record of the queried
  type — often it has the *other* address family. Answering NXDOMAIN when another
  resolver said the name exists would be wrong.
- **NODATA < off-topic.** Every other rung grades a response's *content*; this one
  grades its *relevance*. A reply whose answer section carries records but never
  the queried name says nothing about that name, so nothing in it may decide what
  that name resolves to — a private address belonging to some unrelated name must
  not make the reply the internal answer. It still outranks the negatives, because
  it is a reply that was received and is served when nothing better exists: ranking
  it below them would mean a false positive of this very check suppresses a
  legitimate answer in favour of another resolver's NXDOMAIN. That it therefore
  outranks another resolver's NXDOMAIN is deliberate and costs availability only,
  never a redirect. An **empty** answer section is not off-topic — that is the
  ordinary NODATA reply, and there is nothing in it that could be about a name.
  Owner names are compared in canonical form, because DNS 0x20 case randomization
  makes a resolver echo the name in mixed case. A reply that does mention the
  queried name but gives no address for it — a TXT, say — lands on NODATA, which is
  what it is: the name exists and has no address here.
- **Off-topic < referral.** A NOERROR CNAME/DNAME with no address resolved is a
  pointer to follow, which is more than a negative answer, and it is about the
  queried name. An NXDOMAIN that happens to carry a CNAME stays NXDOMAIN — the
  alias resolves to nothing.
- **Referral < answer**, and a ranking policy refines only that top tier:
  `prefer_private` splits it into public < private.

Two details worth recording. EDNS extended rcodes (≥ 16, RFC 6891) live in the
OPT record, not in the 4-bit header field, so they are read from OPT before
anything else — otherwise BADVERS (16, header nibble 0) would look like NOERROR
and BADMODE (19, nibble 3) like NXDOMAIN. And because the ladder is shared, the
`prefer_routed` idea of D11 becomes a different refinement of the top tier alone,
with the ordering of everything below it decided once, here.

### An answer is internal only if all of it is (D3)

D3's private/public split reads naturally as "the answer holds a private address".
The implementation requires **every** address of the queried type to be private
instead. A message that also hands out a public address does not resolve the name
into the internal network, and unlike "holds one" — which is decided by whichever
record was appended last — a property of the whole set cannot be won by adding
records to the message.

Which addresses *are* the answer is decided by membership, not by resolving the
chain. The set of names this reply presents as aliases of the question is the
question's own name plus every CNAME target reachable from it, and an address
counts when its owner is in that set, sits in the question's class, and is not
itself aliased. Nothing else in the answer section belongs to this question.

That is deliberately not a chain walk, and it needs none of the rulings one would
require. A cycle is harmless because adding to a set is idempotent; a name aliased
twice contributes both targets; there is no hop limit to pick, since every round
either adds a name or ends the loop and the only candidates are CNAME targets of
this answer, bounding it at the number of records. Order does not matter because
each round rescans the section. The motivating shape works unchanged: a two-step
Azure chain ending in a private address is a set of one private address.

Three rules that look like details and are not:

- **Only CNAME extends the set.** A DNAME aliases a subtree, not a name — its
  owner is a proper ancestor of the queried name, and it reaches an answer through
  the synthesized CNAME RFC 6672 requires alongside it. Letting a DNAME extend the
  set, or count as a referral, would let an unrelated one decide this question.
  Without the synthesized CNAME the set does not grow and the ladder settles lower,
  which is the safe direction.
- **Only CNAME makes a name non-terminal.** An address at a name that carries a
  CNAME of its own is not this reply's answer: RFC 1034 §3.6.2 forbids a CNAME
  owner from holding other data, so a reply doing both is malformed and a client
  following the alias ends up elsewhere than the address suggests. The test asks
  about CNAMEs only — a DNAME owner may legitimately carry an address, and
  excluding those would silently drop a legitimate preference.
- **Class is part of a record's identity.** A record in a class other than the
  question's answers a different question; the client matches on (name, class,
  type) and discards it, so counting it would let a discarded record decide.

One accepted consequence: a reply that pads its answer section with unrelated
**public** records loses its preference. Only a careless upstream can trigger that,
and only for its own answer — nobody can enrich another resolver's reply. Note that
"loses its preference" can mean the *other* group's public view is served, if that
group wins the tiebreak: for a dual-homed service published with both an internal
and a public address in one answer, `prefer_private` then has no effect for that
name. The selected source is logged at debug level, which is where that shows up.

Because "internal" is a property of the whole answer, the ambiguity warning below
compares only candidates that are internal answers: a mixed reply is not one, so it
cannot disagree about an internal address. The same restricted address set feeds the
ranking, the signature comparison and the warning, so they cannot drift apart.

### Which zones are pooled (D5)

The pool is built per zone from the groups serving it, and a nameserver group
marked *primary* serves the root zone. Two primary groups therefore pool `.`, so
every A/AAAA query fans out and pays the settle window — not only the split-horizon
zones D5 has in mind. That is the correct reading of "a zone served by more than one
resolver", and with classification tied to the queried name such a zone is no less
safe than any other, but the cost is worth knowing before enabling the policy on a
peer whose default resolvers are two primary groups.

### Where the implementation refines D4

D4 says a name with two or more private answers is logged as ambiguous. The
implementation narrows and throttles that:

- The flag (`selection.Result.Ambiguous`) is raised only when the private
  answers **differ** — two resolvers naming *different* private endpoints for
  one name. Identical private answers from redundant resolvers are normal HA,
  not a topology problem, and warning about them would be noise.
- The selector stays a pure function and does not log; it sets the flag and the
  pool warns, **at most once per five minutes**. A misconfigured zone is a
  standing condition an operator has to fix, not a per-query event, and the
  warning sits on the query path.

The choice itself is deterministic either way, so this only changes what gets
logged, never what gets answered.

### Deferred: a determinism-preserving early exit

D5 permits an early return only when the outstanding resolvers cannot change the
outcome, and defines that as "a private answer is in hand *and* every other
resolver has already responded" — which is the all-answers-in condition. The
implementation therefore always waits for every resolver or the window, so a
single slow resolver costs up to the window on every pooled A/AAAA query.

There is a strictly stronger exit condition that still cannot change the result,
and it is worth recording for a later increment:

> Return as soon as the best candidate holds the **maximum possible score** and
> every resolver still outstanding sorts to a **higher stable key** than that
> candidate.

It is exact, not heuristic: the maximum score cannot be beaten, and the tiebreak
picks the lowest key, so an outstanding resolver with a higher key can at best
tie and lose. One with a lower key can still win and must be waited for.

Cost: the pool cannot evaluate this itself — it knows neither the policy's
scoring nor what its maximum is — so `SelectionPolicy` would grow a second
method along the lines of `Decided(q, candidates, pending) bool`, which every
policy then has to implement. The only thing lost is the ambiguity flag above,
since a second private answer may go unseen; that is a diagnostic, not an answer
path. The gain is bounded by how much slower the slowest resolver is than the
fastest, so it pays off exactly in the case the window exists for.

### What the grace window does and does not bound (D5)

D5 calls the window "a ceiling on how long the pool waits". It is a ceiling on the
**added** wait: the clock starts with the first reply a policy could rank, as the
implementation note in D5 recommends, so the pool is at most one window slower
than its fastest resolver.

It is deliberately **not** armed by a resolver that failed outright. A zone whose
first resolver fails fast and whose second is slow therefore waits for the second
one, bounded only by that resolver's own `UpstreamTimeout` — where today, with
only the first resolver ever consulted, the client would get an immediate
SERVFAIL. Arming on a failure instead would cap that wait, but at the price of
sometimes returning SERVFAIL while discarding the answer the pool exists to
fetch. Getting the answer wins; the wait stays bounded per resolver, and a
resolver that keeps failing is deactivated out of the fan-out by the health model
after `failsTillDeact` attempts.

### Health scope of a pooled zone (D6)

D6 keeps the per-source `deactivate`/`reactivate` model. Two details only became
visible once the pool existed, both covered by regression tests:

- The zone-level hooks must be derived **once per pool**, not per source. They
  close over the index `reactivate` needs to undo what `deactivate` did, so
  per-source pairs let whichever source returns first reactivate through an
  empty index — leaving the zone deregistered until the next configuration push,
  depending on which backoff won.
- They must be scoped to the **pooled zone alone**. A nameserver group may serve
  further domains, each pooled separately with its own health, so deactivating
  one zone must not deregister the others. The zone-level hook therefore runs
  against a synthetic group covering just that zone; per-source status keeps
  coming from the sources themselves.

## References

- Proposal & maintainer greenlight: [openzro/openzro#140](https://github.com/openzro/openzro/issues/140).
- Sibling ADRs: [ADR-0022](./0022-custom-dns-zones.md) (adjacent chain tier),
  [ADR-0020](./0020-openzro-ssh-identity-protocol.md), [ADR-0021](./0021-policy-propagation-consistency-model.md).
- openZro code: [`upstream.go:126-159`](../../client/internal/dns/upstream.go#L126-L159),
  [`server.go:648`](../../client/internal/dns/server.go#L648),
  [`server.go:666`](../../client/internal/dns/server.go#L666),
  [`handler_chain.go:13-19`](../../client/internal/dns/handler_chain.go#L13-L19).
- NetBird prior art (BSD, adapted-with-attribution per D7/D12; conceptual for
  the selection layer): `client/internal/dns` `raceAll` / `buildMergedDomainHandler` at HEAD `8a43f4f`.
