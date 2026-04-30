# ADR-0010 — Unidirectional `Protocol=ALL` access policies

**Status:** Accepted (2026-04-30)

**Supersedes:** none

**Relates to:** none

## Context

Access policies in openZro carry a `Bidirectional` flag on each rule:

- `Bidirectional=true` — peers in `Sources` can initiate to peers in
  `Destinations`, AND peers in `Destinations` can initiate to peers in
  `Sources`. Two flows allowed.
- `Bidirectional=false` — only `Sources → Destinations` may initiate.
  Reply traffic for that flow rides the firewall's stateful conntrack
  the same way protocol-specific rules already do. The reverse
  direction (`Destinations → Sources`) is denied.

For protocol-specific rules (TCP/UDP/ICMP), unidirectional has always
been straightforward and well-tested.

For `Protocol=ALL`, NetBird historically forced `Bidirectional=true`.
The relevant code lived in three places:

1. `management/server/types/policy.go::Policy.UpgradeAndFix` — auto-coerced
   on filestore→SQLite migration (PR
   [netbirdio/netbird#3047](https://github.com/netbirdio/netbird/pull/3047)).
2. UI (NetBird dashboard, originally) — disabled the direction toggle
   when the operator picked `Protocol=ALL`.
3. Default policy created for new accounts (`types/account.go`) — hardcoded
   `Bidirectional=true`. (This is fine and unchanged; operators can
   edit the default.)

Users have asked for the inverse for at least a year — see
[netbirdio/netbird#3547 "One-way direction to a peer"](https://github.com/netbirdio/netbird/issues/3547),
open since 2025-03-19, with the rationale:

> If a server with one of the peers is taken over by an attacker, then
> the attacker will automatically gain access to the entire network
> accessible to the peer. This would close this attack surface.

In other words: forced bidirectional is a real lateral-movement risk on
ALL-protocol rules. Operators who want to allow `peer A → peer B` for
"everything" specifically don't want a compromised B to pivot back to A.

## Decision

openZro supports unidirectional `Protocol=ALL` policies as a
first-class feature. No opt-in setting; no experimental flag. The
firewall-rule compiler ([`types/account.go::GetPeerConnectionResources`](../../management/server/types/account.go))
and the userspace data-plane (`client/firewall/uspfilter`) already
respect `Bidirectional=false` per-direction; the only thing standing
between operators and the feature was the upstream auto-coerce.

We removed the coerce.

Concretely:

- `Policy.UpgradeAndFix` no longer flips `Bidirectional=true` on
  Protocol=ALL rules. The function still migrates Protocol="" → ALL
  for pre-v0.20 filestore imports, which is uncontroversial.
- The HTTP handler ([`policies_handler.go`](../../management/server/http/handlers/policies/policies_handler.go))
  has never coerced; it forwards `rule.Bidirectional` from the request
  body as-is.
- The dashboard's `PolicyDirection` toggle does not gate on protocol;
  ALL with "in" or "out" is a regular UI choice.
- A regression test (`TestSavePolicy_UnidirectionalAllRoundtrip` in
  [`policy_test.go`](../../management/server/policy_test.go)) creates
  a Protocol=ALL rule with Bidirectional=false, saves through the
  manager, reloads from the store, and asserts the flag survives the
  round-trip on both SQLite and Postgres engines. Future contributors
  who restore the upstream coerce will see this test go red.

## Before / after

**Before** (matched upstream NetBird, still the case in
`netbirdio/netbird` as of 2026-04-30):

```
operator picks Protocol=ALL + Bidirectional=false in the API
      ↓
UpgradeAndFix flips it on next migration / filestore import
      ↓
firewall-rule compiler emits BOTH directions
      ↓
peer B (compromised) can initiate to peer A; lateral movement intact
```

**After** (openZro ≥ v0.53.1-alpha.16):

```
operator picks Protocol=ALL + Bidirectional=false
      ↓
flag persists through save and reload
      ↓
firewall-rule compiler emits ONLY the forward direction
      ↓
reply traffic on established flows passes via stateful conntrack
      ↓
peer B → peer A unsolicited initiation is denied
```

## Consequences

**Wins**

- Closes the lateral-movement gap that `#3547` describes. Operators
  running mesh-wide ALL policies for convenience can now make the
  egress half asymmetric without dropping to per-protocol rules.
- No new schema, no new flag, no new permission scope. The data plane
  was already direction-aware; the user-facing behavior just stops
  being silently overridden.
- Default-policy semantics unchanged: every new account still gets a
  bidirectional ALL-to-ALL policy. The operator opts into asymmetry.

**Costs / risks**

- **Asymmetric flows that aren't conntrack-friendly.** ALL includes
  ICMP. ICMP echo request/reply has identifier sessions that conntrack
  handles, but operationally-asymmetric ICMP (e.g. an unsolicited
  destination-unreachable from B to A) will be dropped under
  unidirectional. Operators who depend on those should keep their
  rules bidirectional.
- **Unsolicited reply protocols.** Apps on the destination peer that
  proactively push to the source (SNMP traps, syslog UDP outbound,
  some agent telemetry) will fail silently under a unidirectional
  rule. The operator needs to know.
- **Audit confusion.** Existing audit-log dashboards assume symmetric
  flows. Asymmetric drops will appear in flow exports as
  `dropped: rule unidirectional, no reverse path` (existing client log
  format, not new). Worth calling out in operator docs.
- **Lab validation gap.** The change is exercised by unit tests on
  SQLite + Postgres at the manager layer, and the firewall-rule
  compiler is covered by `TestAccount_getPeersByPolicyDirect`. End-to-
  end behavior on real iptables / uspfilter / native-firewall backends
  needs a manual lab pass before any release marks unidirectional ALL
  as "stable" in marketing copy. Until then, the docs label it
  EXPERIMENTAL.

**Decisions deliberately not taken**

- We did NOT add a `Settings.AllowUnidirectionalAll` toggle. An earlier
  draft of this ADR proposed one (default false → preserve upstream
  behavior, default true → enable feature). We dropped it once the
  audit showed the only blocker was the migration coerce — there's no
  second-system tradeoff to mediate, just a behavior to stop forcing.
- We did NOT add per-rule "experimental" labels in the dashboard. A
  callout is cleaner; see operator docs.
- We did NOT change the default policy for new accounts. ALL-to-ALL
  bidirectional is a sensible default; operators who want asymmetry
  edit the policy after creation.

## Validation

- `TestSavePolicy_UnidirectionalAllRoundtrip` (manager + store
  roundtrip, both SQLite and Postgres).
- Existing `TestAccount_getPeersByPolicyDirect` covers the firewall-
  rule compiler with `Bidirectional=false` (line 601 onwards).
- Lab plan (manual, not CI-gated):
  1. Three peers (peerA, peerB, peerC). Policy: groupA → groupB,
     Protocol=ALL, Bidirectional=false.
  2. From peerA, `nc -zv peerB 80`: should succeed.
  3. From peerB, `nc -zv peerA 80`: should fail (timeout).
  4. From peerA, `curl http://peerB/`: should succeed end-to-end
     (forward + conntrack-tracked reply).
  5. Repeat with iptables backend (Linux) and uspfilter backend
     (cross-platform).

## References

- NetBird issue [#3547](https://github.com/netbirdio/netbird/issues/3547) — feature request, still open as of 2026-04-30.
- NetBird PR [#3047](https://github.com/netbirdio/netbird/pull/3047) — original migration code that introduced the coerce.
- [`management/server/types/policy.go`](../../management/server/types/policy.go) — `UpgradeAndFix`.
- [`management/server/types/account.go`](../../management/server/types/account.go) — firewall-rule compiler (`GetPeerConnectionResources`).
- [`management/server/policy_test.go`](../../management/server/policy_test.go) — regression test.
