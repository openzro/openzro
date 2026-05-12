# ADR-0018 — Server-side flow event policy resolver

**Status:** Proposed (2026-05-12)

**Relates to:** [ADR-0002](0002-flow-events-storage.md) (flow events
storage), [ADR-0013](0013-flow-policy-correlation.md) (agent-side
policy correlation via ctmark).

**Supersedes:** none.

## Context

[ADR-0013](0013-flow-policy-correlation.md) added agent-side
correlation between flow events and the originating PolicyID on
Linux peers, by stamping a `rule_index` on bits 17-31 of the
conntrack mark when the ACL chain matches a packet. The conntrack
collector reads the mark, asks the in-process `policymark.Indexer`
to map it back to the management-issued PolicyID, and ships the
result inside `FlowEvent.RuleId`.

That works for three of the four paths flow events come from:

| Path | Stamping site | Status |
|---|---|---|
| Linux kernel firewall, **inbound** | [`nftables/acl_linux.go`](../../client/firewall/nftables/acl_linux.go) / [`iptables/acl_linux.go`](../../client/firewall/iptables/acl_linux.go) | ✓ stamped |
| Linux kernel firewall, **routing forward** | [`nftables/router_linux.go`](../../client/firewall/nftables/router_linux.go#L389) / [`iptables/router_linux.go`](../../client/firewall/iptables/router_linux.go) | ✓ stamped |
| Userspace filter (macOS / Windows / userspace-bind) | [`uspfilter/filter.go`](../../client/firewall/uspfilter/filter.go) — direct PolicyID on rule | ✓ in-band |
| Linux kernel firewall, **outbound from initiator** | nowhere — packets only hit OUTPUT + POSTROUTING mangle | ✗ empty |

The fourth path is the one a Cora operator surfaced. They open the
Network Traffic page and see the Policy column blank for every
flow their machine reported (events where their peer is the
**initiator** of an outbound connection to a Network Resource via a
routing peer). Sample API response:

```json
{
  "peer_id": "d7tlq3jr42ds73fq66m0",
  "rule_id": null,
  "type": "start",
  "occurred_at": "2026-05-12T04:13:08.267667Z"
}
```

Their conntrack inspection shows the entries carry only the legacy
data-plane mark (`mark=0x1BD11 = DataPlaneMarkOut`); bits 17-31 are
zero because:

1. The agent's OUTPUT chain does not install per-policy ACL rules
   (upstream commit `5a82477d`, "Remove outbound chains" —
   stateful conntrack matching covers reply traffic on INPUT, so
   the OUTPUT path is unstamped by design).
2. The POSTROUTING mangle rule in
   [`router_linux.go::setupDataPlaneMark`](../../client/firewall/nftables/router_linux.go#L294)
   writes `DataPlaneMarkOut` directly to the ct mark register
   instead of OR-ing into it — it would overwrite any high-bit
   rule_index even if a previous chain had set one.

The gap is architectural, not a bug. It exists in NetBird upstream
OSS too (they recently removed the agent-side `PolicyResolver`
entirely; their cloud product fills `rule_id` server-side as the
inferred Enterprise differentiator). ADR-0013 explicitly reserved
this slot:

> **Not a server-side resolver.** Management trusts whatever
> RuleId the agent reports. […] Server-side correlation could land
> later as a fallback that fills empty RuleIds at insert time —
> out of scope here.

This ADR makes the slot concrete.

## Decision

Add a **server-side resolver in the flow ingest pipeline** that
fills `RuleId` when the agent left it empty. The resolver is a
fallback: events that arrive with a non-empty `RuleId` (the
common case — inbound, routing, uspfilter) are passed through
untouched. Only outbound-initiator events from Linux kernel peers
hit the resolver in practice.

### Where it sits in the pipeline

```text
peer ──gRPC FlowService.Events──> management/server/flow_service.go
                                     │  handler ACKs peer immediately
                                     ▼
                                  resolver (this ADR)
                                     ├── event.RuleId != "" → pass through
                                     └── event.RuleId == "" → enrich
                                     ▼
                                  ┌─ HOT store
                                  ├─ SIEM stream
                                  └─ COLD archive
```

The resolver runs **after** the peer ack has shipped, so peer-side
latency is untouched. It runs **before** the fan-out so the same
enriched event reaches the hot store, the SIEM exporter, and the
cold archive — operators querying any of those tiers see
consistent attribution.

### Algorithm

For each event missing `RuleId`, build a query tuple
`(peer_id, src_ip, dst_ip, dst_port, protocol)` and match it
against the cached account policy graph. The matching rule is the
same the dataplane already follows when computing the network map
that produces `FirewallRule.PolicyID` for each peer — we just run
it backward.

```text
1. peer_id → groups membership (Account.Peers[peer_id].GroupID)
2. groups → policies where any source rule includes a group the peer is in
3. for each candidate policy:
     a. does dst_ip resolve to a resource / peer in any destination group?
     b. does dst_port + protocol fall in the policy's port set?
   first candidate that satisfies (a) ∧ (b) wins
4. return policy.ID
```

Ambiguity is settled the same way the dataplane settles it:
**first match by policy creation order**. This is deterministic
and matches what the firewall already enforces on the wire — the
dashboard now shows the same policy the kernel actually saw.

### Index shape

A reverse index built per-account, lazily on first need and
refreshed when the account's policy/group/peer graph changes
(`Account.Manager` already emits these events for the existing
`updateAccountPeers` path; we hook the same triggers).

```go
type FlowPolicyResolver struct {
    mu         sync.RWMutex
    byAccount  map[string]*accountIndex
}

type accountIndex struct {
    // For each peer, the list of policies where the peer is on the
    // source side. Per-peer lookup is O(1); the candidate list per
    // peer is typically 5-50 entries in realistic accounts.
    byPeer map[string][]policyCandidate
}

type policyCandidate struct {
    policyID    string
    dstResources []string        // resource IDs to expand at match time
    dstCIDRs     []netip.Prefix  // pre-expanded destination CIDRs
    ports        []portRange     // empty = match any port
    protocol     proto           // 0 = any
    createdAt    time.Time       // tiebreaker
}

func (r *FlowPolicyResolver) Resolve(accountID, peerID string,
    dstIP netip.Addr, dstPort uint16, p proto) (string, bool) { … }
```

Lookup cost per event:
- O(1) hash to fetch the peer's candidate list.
- O(candidates) linear scan with a couple of inline predicate checks.
- Realistic worst-case in our largest Cora-style account today
  (~200 peers, ~30 policies, ~10 candidates per peer) is one
  cache line + a handful of comparisons ≈ **1µs per resolution**.

### Performance budget

Set against the existing ingest cost (DB write dominates at
~100-500µs per event):

| Tier | Events/s | Resolver CPU/s | % of one core |
|---|---|---|---|
| Tiny | 50-200 | <0.1ms | <0.1% |
| Small team | 500-2k | ~1-2ms | <0.5% |
| Medium (Cora today) | 5k-20k | ~10-30ms | ~1-3% |
| Large | 50k+ | ~100-200ms | ~10-20% |

Memory budget at Medium tier: ~5-15MB of index per account.
Larger deployments revisit — see Open questions §3.

The resolver **only runs when `RuleId` is empty**. Best estimate
from the four-path table above: in mixed-OS deployments roughly
25-40% of events will hit the resolver; in Linux-kernel-heavy
deployments it climbs toward 75% but those accounts already pay
the rebuild cost on policy updates anyway.

### HA behavior

Each management replica holds the account graph in memory already
(`Account.Manager` cache) and processes flow events from its own
connected peers. The resolver is **stateless per replica** — no
distributed coordination, no leader election. Two replicas that
ingest events from different peers of the same account each run
their own resolver; both see the same answer because they read the
same authoritative state. Behavior under partition: the resolver
falls back to the last-known policy graph until the cache refresh
catches up.

### What does NOT happen

- **No retroactive enrichment of pre-resolver events.** Rows already
  in the hot store / archive with `RuleId = empty` stay empty.
  Operators who need historical attribution run their own SQL
  against the hot store; we ship a documented query in the
  operator runbook (see Implementation §5). Online enrichment of
  events older than a few minutes is a fairness problem (policy
  graph from 6 months ago may have been replaced) and out of
  scope for v1.
- **No new env var.** The resolver is on by default; an internal
  knob can disable it for benchmarking but is not surfaced.
- **No client-side change.** The agent contract stays the same;
  we keep the agent-side stamping path as the primary correlator
  for the three paths where it works.
- **No new dependencies.** Everything lives inside
  `management/server/flow_policy_resolver/`.

### Why this is a fallback, not a primary

A discussion point worth recording. We deliberately keep both
paths:

1. **Agent-side stamping** stays the primary for the inbound /
   routing-forward / uspfilter cases. It's free at ingest time
   (the kernel already filled the field), survives policy graph
   transitions (the rule_index → PolicyID map is whatever the
   agent had when it stamped the conn, not what the management
   has now), and matches what actually happened on the wire.
2. **Server-side resolution** is the fallback for the outbound-
   initiator gap. It's slightly slower (~1µs per event) and reads
   the current policy graph, not the historical one — but for
   the outbound case there's no historical graph to read.

Choosing one over the other would either lose audit fidelity
(server-only, no historical accuracy) or leave the gap open
(agent-only, outbound stays blank). Defense in depth fits openZro's
self-hosted positioning where dashboards matter more than minimal
agent footprint.

## Test plan

TDD-first per project rules. Tests live under
`management/server/flow_policy_resolver/`.

### Unit (pure)

Table-driven against a fixture account graph:

1. **Resolves a single matching policy** — peer A in group G,
   policy P maps G → resource R on TCP/443, event from A to R:443
   returns P.ID.
2. **Disambiguates by creation order** — two policies match the
   same tuple; older wins.
3. **No match returns empty** — event from a peer with no
   relevant policies.
4. **Pass-through on non-empty RuleId** — agent-stamped event is
   not re-resolved.
5. **Port range matches** — `8000-8999` matches `8443`, fails
   `9000`.
6. **Protocol ALL matches every protocol** including ICMP.
7. **Destination resource — IP literal** matches.
8. **Destination resource — CIDR** matches (the `10.0.0.0/24`
   path).
9. **Destination resource — hostname** — uses the precomputed
   resolved-IPs from the management's DNS path (ADR-0015).
10. **Empty peer_id / unknown peer** returns empty, no panic.
11. **Cross-account isolation** — peer in account X cannot
    resolve to a policy from account Y.

### Integration

Postgres testcontainer + `FlowService.Events` round-trip:

1. Send a flow event with empty RuleId → assert hot store row has
    the expected policy_id.
2. Modify the policy graph (delete the matching policy) →
    subsequent events return empty as expected, the index
    invalidated cleanly.
3. Concurrent resolution under policy save — start a goroutine
    that loops on Resolve(), modify the policy graph 1000 times
    from another goroutine, assert no panic and final state is
    self-consistent (no half-built index reads).

### Property-style

For every (peer, dst_ip, dst_port, protocol) tuple where the
dataplane would have accepted the connection, the resolver MUST
return the same PolicyID the dataplane attributed to the firewall
rule. Property checked with `quick.Check` against a generated
account graph.

## Implementation sequence

Each step its own commit.

1. **Resolver package skeleton** — `management/server/flow_policy_resolver/`
   with `Resolver` struct, `Resolve()`, `accountIndex` shape,
   `rebuildAccount(accountID, *Account)` builder. Tests for the
   pure matching logic (unit cases 1-11) added first, then the
   builder.

2. **Account-change hooks** — register the resolver with
   `Account.Manager` so `updateAccountPeers` and the policy
   save/delete paths invalidate the affected account's index.
   Reuses the existing event surface; no new public API on the
   account manager.

3. **Wire into flow_service.go** — slot the resolver between the
   gRPC handler's enqueue and the per-destination fan-out. Skip
   the resolver when `event.FlowFields.RuleId != ""`.
   Integration test for the round-trip lands here.

4. **Metrics** — three Prometheus counters on the existing flow
   metrics surface: `flow_resolver_hits`, `flow_resolver_misses`,
   `flow_resolver_skipped` (agent-stamped pass-through). Histogram
   for `flow_resolver_duration_seconds`. Operator gets a clear
   read on the cost.

5. **Operator runbook section** — add a section to
   [`docs/operator/`](../../docs/operator/) documenting the
   resolver and the manual SQL operators run if they want to
   backfill historical events.

6. **Helm chart appVersion bump** + release notes.

Estimated total: ~2 days backend (steps 1-4), ~half day docs +
chart (steps 5-6). Step 1 dominates; steps 2-4 are wiring.

## Consequences

### Positive

- **Closes the last visible attribution gap** in the Flow Traffic
  page. Operators see a populated Policy column on every event
  the management considers policy-driven, regardless of which
  side of the conn the peer was on.
- **Parity with NetBird Cloud's enterprise audit feature**,
  without leaving our self-hosted scope. Cora-class operators
  get the same dashboard utility paying nobody.
- **Agent-side stamping stays untouched** — the ~75% of paths
  where it already works skip the resolver entirely.
- **SIEM stream and cold archive both inherit attribution for
  free**, since the resolver runs before fan-out.

### Negative / risks

- **Memory cost grows with account size.** Capped by the index
  shape choice (peer-keyed, candidate list bounded), but a
  hypothetical 10k-peer + 1k-policy account would carry a
  ~50-150MB index. Mitigated by sharing the candidate slices
  across peers in the same source group (a future optimization
  not yet in v1).
- **Ambiguity is silently picked by creation order.** This
  matches dataplane behavior but if an operator changes their
  intent without realizing it the dashboard will follow without
  warning. Documented in the runbook; metrics expose the
  ambiguity count via `flow_resolver_ambiguous`.
- **Resolver runs against the live policy graph**, so events that
  ingest right after a policy is deleted are attributed to the
  *next* matching policy (or none). The window is bounded by the
  graph rebuild cadence (~milliseconds); not ideal but expected.

### Neutral

- The resolver is **per-replica**; HA deployments do not
  coordinate. Each replica resolves what its own peers report.
  No cross-pod cache.
- The resolver does NOT extend to admission events, route flows,
  or other event sources. Those have separate correlation paths
  already.

## Alternatives considered

1. **Add OUTPUT chain to the agent ACL.** Re-introduces what
   upstream commit `5a82477d` removed; complicates the agent and
   doesn't help macOS/Windows/userspace deployments that are
   already fine. Rejected.

2. **CONNMARK rules in postrouting mangle that mirror ACLs.**
   Stamps rule_index on outbound conns at packet time, no server
   coupling. Reasonable but doubles the number of installed
   nftables rules; adds policymark coupling to a mangle-time
   path that was deliberately kept simple. Deferred — could be
   the v2 enhancement if resolver CPU cost becomes an issue.

3. **Read-side resolution** (only fill rule_id when the dashboard
   queries the event). Cheaper in storage but doesn't help SIEM
   stream / archive consumers, and the query cost would be
   per-page-load. Rejected.

4. **Out-of-band async enrichment** (separate goroutine reads
   recent events, fills rule_id, writes back). More moving parts,
   no win over the inline resolver since the inline cost is
   already <1% at typical tiers. Rejected.

## Open questions

1. **Should we expose `flow_resolver_disabled` as an env var?**
   Default off (resolver always runs). Use case: operators who
   ship every event to an external resolver via SIEM and don't
   want the management to do the work. Probably yes, low cost
   to add.

2. **Ambiguity warning UX.** When the resolver picks a policy
   among multiple candidates, should the dashboard show a small
   indicator on the row? Useful for audit, slightly noisy. Vote
   needed.

3. **Memory ceiling for very-large accounts.** Above ~10k peers
   the index footprint becomes meaningful. We probably need a
   benchmark + an alternative shape (interval tree on dst
   CIDRs + bitmap on source groups) — but that's a follow-up
   PR, not a blocker for v1.

4. **Should `Resolve` accept a context with a deadline?** Today
   the loop is sub-microsecond so timeouts are theatrical. If we
   ever swap in a CIDR tree with a worst-case scan, a deadline
   becomes useful.

## References

- [ADR-0002](0002-flow-events-storage.md) — flow events storage
- [ADR-0013](0013-flow-policy-correlation.md) — agent-side
  correlation via ctmark, explicitly reserves this slot
- [`management/server/types/account.go::GetPeerConnectionResources`](../../management/server/types/account.go)
  — the forward path that produces `FirewallRule.PolicyID` per
  peer (the resolver is its inverse)
- [`client/firewall/nftables/router_linux.go::setupDataPlaneMark`](../../client/firewall/nftables/router_linux.go#L294)
  — the postrouting mangle that clobbers any high-bit rule_index
  before the conntrack entry is finalized
