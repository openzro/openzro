# ADR-0013 — Flow event policy correlation via ctmark on Linux

## Status

Accepted (2026-05-04).

## Context

`proto.FlowEvent` carries a `RuleId` field that the dashboard uses to
label each event with the policy that allowed (or denied) it. The
agent populates it on macOS / Windows / Linux-userspace deployments
because `client/firewall/uspfilter` keeps the management-issued
PolicyID on the matched rule and emits it inside the event.

On **Linux peers running the kernel firewall** (nftables / iptables —
the default for any non-router agent on Linux), the netlink-based
`client/internal/netflow/conntrack/conntrack.go` collector observes
flow lifecycle events via the kernel's conntrack subsystem. The
conntrack subsystem **does not expose** which firewall rule allowed
the packet — it only carries the `ct mark` (a 32-bit field per
conntrack entry).

The result: for the operator running the typical Linux deployment
(the operator's gateways, every Ubuntu home/server peer, every
Debian routing peer), the Network Traffic page shows IPs and ports
but no policy name. The "Allow Mesh Traffic" default policy never
correlates. NetBird OSS upstream has the same gap; it is the kind of
audit-grade feature their commercial Enterprise tier closes.

## Decision

Carry the policy identifier through the kernel via the conntrack
mark, in **bits 17-31** of the existing `ct mark` field. Bits 0-16
keep the current `nbnet` mark scheme (DataPlaneMarkIn/Out,
ControlPlaneMark, PreroutingFwmark*) **byte-for-byte unchanged** so
no existing call site needs to relearn the mark layout.

```
bit 31 ......... 17 16 ............ 0
[   rule_index   ] [   nbnet mark   ]
   15 bits             17 bits
   max 32 768 rules    existing scheme
```

The agent owns a per-process integer index per persisted policy
rule. The map `rule_index → PolicyID` is held in the ACL manager and
consulted by the conntrack collector when it builds an event.

## Mark layout

Add to `util/net/net.go`:

```go
const (
    RuleIndexShift uint32 = 17
    MarkValueMask  uint32 = 0x0001FFFF // bits 0-16: legacy mark space
    RuleIndexMask  uint32 = 0xFFFE0000 // bits 17-31: rule_index
    MaxRuleIndex   uint32 = RuleIndexMask >> RuleIndexShift // 32 767
)

func MarkValue(fwmark uint32) uint32     { return fwmark & MarkValueMask }
func MarkRuleIndex(fwmark uint32) uint32 { return (fwmark & RuleIndexMask) >> RuleIndexShift }
```

The existing `DataPlaneMarkIn`, `DataPlaneMarkOut`, `ControlPlaneMark`,
and the `PreroutingFwmark*` constants are unchanged. `IsDataPlaneMark`
is updated to mask off the rule_index before comparing to the legacy
range. The ratio fits: every legacy constant is ≤ `0x1BDFF`, well
within the 17-bit budget.

## Per-rule lifecycle

The ACL manager allocates the index when a `proto.FirewallRule` is
materialized into a kernel rule:

```go
type policyIndexer struct {
    mu       sync.RWMutex
    next     atomic.Uint32                  // monotonic, never reused
    byIndex  map[uint32][]byte              // index → PolicyID
    byPolicy map[string]uint32              // PolicyID → index, for dedup
}
```

The counter is monotonic and wraps at `MaxRuleIndex`. On wrap the
agent logs a warning and falls back to a synthetic "rule_index = 0"
that translates to an empty `RuleId` in the event — matching today's
behaviour and degrading gracefully rather than corrupting the
mapping. 32 767 simultaneous rules is far above any realistic
deployment (the test we ran has 6 rules).

When a `proto.FirewallRule` arrives whose PolicyID was already
indexed, the existing index is reused. When the rule is deleted the
index entry stays (we never recycle). The map is bounded by the
unique number of policies ever seen since process start; cleared on
restart.

## Mark application

### nftables (`client/firewall/nftables/acl_linux.go`)

When the rule is built, append two expressions before the existing
verdict:

```
ct mark set ct mark | (rule_index << 17)
```

`nftables` exposes this through `expr.Bitwise` (load ct mark into a
register, OR the constant, store back). Adding the assignment costs
~tens of cycles in the slow path of rule installation; the per-packet
hot path is identical to today (the kernel applies marks at line
rate).

If `rule_index == 0` (counter exhausted) the expression is skipped.

### iptables (`client/firewall/iptables/acl_linux.go`)

Equivalent via `-j CONNMARK --set-mark <value>/<mask>` with the same
shift, scoped to bits 17-31.

### uspfilter

No change. The userspace filter already records `mgmtId` on the
matched `PeerRule` and emits it directly — the kernel-mark detour is
unnecessary in userspace.

## Reader side

### `client/internal/netflow/conntrack/conntrack.go`

The collector currently emits an `EventFields` with a zero `RuleID`
on Linux. After this change, when the flow's `Mark` carries a
non-zero rule_index, look the PolicyID up via the ACL manager
interface:

```go
type PolicyResolver interface {
    LookupPolicyID(ruleIndex uint32) ([]byte, bool)
}
```

The collector takes a `PolicyResolver` in its constructor (defaults
to a no-op resolver when the manager is nil — keeps unit tests and
relay-only configurations working). On lookup miss the event is
emitted with an empty `RuleID` exactly as today.

`IsDataPlaneMark` — and the direction inference at lines 285-291 —
mask the mark with `MarkValueMask` before comparing, so the legacy
range checks keep working unchanged.

## What this is NOT

- **Not a kernel ABI change.** The conntrack mark is already 32 bits.
- **Not a wire-format change.** `proto.FlowEvent.RuleId` is unchanged.
  Older agents sending an empty RuleId continue to be accepted.
- **Not a server-side resolver.** Management trusts whatever RuleId
  the agent reports. If the agent sends empty (older versions, or
  this fix not yet applied), the event is stored as today and the
  dashboard column reads "—". Server-side correlation could land
  later as a *fallback* that fills empty RuleIds at insert time —
  out of scope here.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| Stale rule_index after policy update / agent restart | Agent restart clears the in-memory map. Conntrack entries flushed by kernel TTL or restart-induced firewall reinstall. New mappings rebuilt on next rule install. |
| Index exhaustion (>32 767 unique policies) | Agent logs warning, emits events with empty `RuleId` — graceful degradation, never corruption. Realistic ceiling is ~hundreds. |
| Agent ↔ collector race on lookup during rule install | Indexer uses RWMutex; lookups are non-blocking after install. Worst case: brief miss returning empty PolicyID, which the dashboard already tolerates. |
| Mark collision with kernel masquerade / connmark of an unrelated rule | The existing `nbnet` mark space is reserved by openzro; non-openzro nftables rules historically wouldn't write into it. Adding bits 17-31 narrows the chance further. |
| iptables backend | Implemented in parallel via `--set-mark <value>/<mask>` so iptables peers benefit too. Tracked as part of this ADR. |

## Out of scope

- Server-side fallback resolver in management.
- Marking at the routing peer / forwarder paths beyond peer ACLs
  (those rules still emit empty RuleId in flow events even after
  this change — a separate ADR if demand emerges).
- Backporting the mark to older agents — they will simply continue
  to emit empty RuleId, matching pre-fix behaviour.

## References

- `client/firewall/uspfilter/filter.go` — where the userspace path
  already wires PolicyID to flow events; the BSD-3 reference design
  for what we're matching on Linux.
- NetBird upstream commit `c02e2361` (Apr 2025) introduced the
  netflow collector; it never bridged the gap to `conntrack mark`,
  and the upstream OSS still ships with empty RuleIds on Linux
  kernel paths.
