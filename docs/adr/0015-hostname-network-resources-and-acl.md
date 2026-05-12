# ADR-0015 — Hostname Network Resources + zero-trust ACL onboarding

## Status

**Accepted** as a diagnostic note + onboarding requirement. Proper
fix is product-side (better onboarding signal in the dashboard +
docs); a parallel architectural improvement (fakeIP-on-Linux for the
DnsInterceptor handler) is captured as a follow-up at the bottom of
this ADR.

## Context

An operator reported a hard freeze ("trava tudo") on the openZro
client whenever they accessed a host their newly-created Network
Resource was supposed to route. The freeze was reproducible: open
`https://ci.example.com` in the browser → the dashboard
(`https://dash.example.com`) and every other `*.example.com` HTTPS
endpoint stopped responding within a couple of seconds; only a
manual `wt0` cycle restored connectivity, until the next access to
`ci.example.com` triggered the loop again.

an operator's self-hosted deployment puts every infra hostname under a single GKE
nginx-ingress LoadBalancer at `198.51.100.42`:

- `mgmt.example.com`, `access.example.com`, `dash.example.com`,
  `signal.example.com`, `relay.example.com`, `example.com` (apex)
- The routed apps `ci.example.com`, `cd.example.com` (sharing the
  same LB; path-routing inside the ingress dispatches per Host
  header).

`mgmt-grpc.example.com` is the lone exception, on its own LB —
that's a side-effect of [ADR-0014](0014-coordinated-multi-pod-relay.md)'s
standalone gRPC proxy, and incidentally the only reason flow ingest
kept working during the incident.

## What I diagnosed first (and got wrong)

Initial reading of the route handler code led to a plausible
theory: the `dnsinterceptor` handler relies on
`internalDnatFw()` ([handler.go:480](../../client/internal/routemanager/dnsinterceptor/handler.go#L480))
which gates fakeIP/DNAT support on `runtime.GOOS == "android"`.
On Linux/macOS/Windows the handler falls back to installing a
/32 prefix for the **real** resolved IP via the WireGuard
interface ([handler.go:117](../../client/internal/routemanager/dnsinterceptor/handler.go#L117)).
With every `*.example.com` host resolving to the same
`198.51.100.42`, that /32 catches the management/dashboard/signal
traffic too. I theorised that this created a circular dependency
on the WG control plane and caused the freeze.

The Android-only gate is real (see "Architectural follow-up"
below). The circular-dependency story turned out to be the wrong
diagnosis.

## The actual root cause

Disabling **DNS Wildcard Routing**
(`routing_peer_dns_resolution_enabled = false`,
[NetworkSettingsTab.tsx:285](../../dashboard/src/modules/settings/NetworkSettingsTab.tsx#L285))
switches the client to the legacy `dynamic` handler. That handler
also installs a /32 for the resolved real IP
([dynamic/route.go:294-298](../../client/internal/routemanager/dynamic/route.go#L294))
and on Linux resolves via plain `net.LookupIP`
([route_generic.go:11-13](../../client/internal/routemanager/dynamic/route_generic.go#L11)) —
i.e. it has the **same** "real-IP /32 captures shared infra IP"
property the new handler has. If the circular-dependency theory
were correct, both handlers should freeze identically. The
operator confirmed the freeze persisted after the toggle flip.

What actually fixed it was creating an Access Control policy
covering `users` → `All` for TCP/80,443. Once that policy was
active, the freeze went away — with the toggle in **either**
state, with the Network Resource still in place, with the same
shared LB IP, with the same /32 PBR captures.

The mechanic is the openZro/NetBird zero-trust default. ACL
policies gate **every** peer-to-peer flow inside the mesh. Direct
internet traffic to `198.51.100.42` (which the client has been
using all along to reach the dashboard) is not policed — it
leaves on the host's default route, hits the public LB, returns.
The moment a Network Resource installs the /32 PBR, that same IP
flips to the "via WireGuard" path: now traffic crosses the mesh
to the routing peer, and the routing peer pair is a peer-to-peer
hop subject to ACL. With no policy matching, the management
firewall layer drops every TCP/443 from the client to the routing
peer. Connections to dashboard, mgmt, signal, relay all stall on
the same blackhole; applications hang, DNS queries pile up, the
client *appears* deadlocked.

The freeze isn't a routing or DNS bug. It's the zero-trust mesh
behaving exactly as designed, hitting an operator who created a
new traffic class without realising they had also implicitly
re-routed a much wider set of connections through the mesh.

## Why the same operator's NetBird deployment worked

Same shared-LB pattern, same hostname route. Two differences
combine to produce the divergent outcome:

1. The NetBird account had a permissive ACL policy ("Default" or
   similar allow-all TCP/443) inherited from older onboarding
   defaults. openZro accounts ship without one — fresh accounts
   have to opt into permissive policies explicitly. That choice is
   defensible (zero-trust by default is the whole point of the
   product), but it produces this exact failure mode the first time
   a new operator wires up a Network Resource without a covering
   policy.

2. The NetBird account ran with
   `routing_peer_dns_resolution_enabled = false` (legacy `dynamic`
   handler). On the routing-peer side that handler installs a
   permissive `0.0.0.0/0` NAT rule for the route's traffic
   ([`routeToRouterPair`](../../client/internal/routemanager/server/server.go#L142)),
   so the peer NATs **anything** that arrives via the route's PBR.
   With the new `dnsinterceptor` handler the NAT rule is scoped to
   a dynamic firewall set populated by the routing peer's local
   resolver. If the client's `/32` PBR captures an IP the
   routing-peer's set didn't observe — for instance a sibling
   hostname on the same shared LB IP — the packet arrives at the
   routing peer, finds no matching NAT rule, has no return path,
   and the connection silently stalls.

The user observed exactly this asymmetry: with the toggle ON and
the default policy in place, `dash.example.com` (no app-layer
allowlist) succeeded while `ci.example.com` (firewalled to
internal source IPs only) failed. Same mesh path, same routing
peer, same /32 PBR — but the backend application's allowlist
rejects connections from the routing peer's NAT egress IP, while
the dashboard backend has no such restriction.

## Decision

1. **Document this failure mode** (this ADR) so operators hitting
   the freeze land here on the first search and recognise it as
   the combined ACL + NAT-scope behaviour, not DNS or routing.

2. **Recommended config for shared-LB deployments**: keep
   `routing_peer_dns_resolution_enabled = false` until the
   architectural improvements below land. With the toggle off, the
   routing peer applies the permissive NAT rule and the route
   behaves as a generic egress for whatever the client's /32 PBR
   captures. The user's working NetBird deployment runs this way.

3. **Update onboarding docs** to call out: *Network Resources
   require an ACL policy covering the client group → routing peer
   group (or → All) for the protocols/ports the resource serves.*
   No silent default; explicit policy per use case.

4. **Do not change the zero-trust default**. A blanket
   `users → All TCP/*` policy would have hidden the symptom but
   undermines the security posture. Instead, tighten the
   recommendation: scope policies to the routing peer group, not
   `All`, so the operator's intent is auditable.

5. **Architectural follow-up** (separate work item): land
   fakeIP/DNAT support on Linux/macOS/Windows so DnsInterceptor
   actually delivers the synthetic-IP isolation it was designed to
   provide. Without that, the handler degrades to "just install a
   /32 PBR for the real IP", which has the property of capturing
   any traffic that happens to share the LB. That's not the
   freeze cause — the ACL is — but it forces operators into the
   sequence we just walked: install a Resource, accidentally
   re-route control-plane traffic through the mesh, hit the ACL
   wall, and then hit the NAT-scope wall on the routing-peer side.
   Synthetic IPs would isolate the routed app's traffic class from
   the rest of the LB without any operator effort.

## Workaround and recommended ACL pattern

Minimum required policy when adding a Network Resource for an
HTTPS host served by a routing peer:

| Sources | Destinations | Protocol | Ports | Why |
|---|---|---|---|---|
| user group (e.g. `users`) | routing-peer group (e.g. `prod-admin-us`) | TCP | 80, 443 | The actual app traffic |
| user group | routing-peer group | TCP | 22 | SSH if relevant |
| user group | routing-peer group | ICMP | — | Connectivity probes |

Avoid the "All" destination. The routing peer group is the
narrowest correct destination; a `→ All` policy implicitly
permits client-to-client TCP/443 traffic that is rarely intended.

DNS already has its own permissive policies in most openZro
deployments (`dns-servers-udp-policy` / `dns-servers-tcp-policy`
on port 53) and does not need extension here.

## Architectural follow-up — fakeIP on non-Android

The `dnsinterceptor` handler ships fakeIP allocation
([handler.go:117](../../client/internal/routemanager/dnsinterceptor/handler.go#L117),
[fakeip/](../../client/internal/routemanager/fakeip/)) and an
in-kernel DNAT mapping mechanism. Both are gated to Android via:

```go
func (d *DnsInterceptor) internalDnatFw() (internalDNATer, bool) {
    if d.firewall == nil || runtime.GOOS != "android" {
        return nil, false
    }
    ...
}
```

[client/internal/routemanager/dnsinterceptor/handler.go:480](../../client/internal/routemanager/dnsinterceptor/handler.go#L480)

On Linux/macOS/Windows this returns `(nil, false)`,
`transformRealToFakePrefix` short-circuits to the real prefix,
and the handler installs a /32 for the public LB IP. Functionally
this isn't a regression vs. the legacy `dynamic` handler (both
end up with the same /32 collision), but it nullifies the whole
design intent of the new handler — synthetic-IP isolation per
resource. Worth landing for non-Android platforms in a follow-up.

Sketch:

- **Linux** — extend the iptables/nftables firewall managers
  ([client/firewall/iptables](../../client/firewall/iptables/),
  [client/firewall/nftables](../../client/firewall/nftables/))
  with `AddInternalDNATMapping(fakeIP, realIP)` /
  `RemoveInternalDNATMapping(fakeIP)` on the OUTPUT chain
  (`PREROUTING -d $FAKE -j DNAT --to-destination $REAL`).
  conntrack handles the reverse path.
- **macOS** — same firewall manager interface; no native pf
  manager today, deferred until kernel WG dominates Darwin.
- **Windows** — userspace filter manager; native WFP is out of
  scope.
- Drop the GOOS gate, replace with capability-check on the
  firewall manager (the same pattern
  `IsServerRouteSupported()` already uses).

This is independent of the present ADR's primary issue and lands
on its own merits.

## Consequences

- Operators creating their first Network Resource hit the freeze
  unless they also create a covering ACL policy. The dashboard
  has no in-product warning today.
- The zero-trust default is preserved.
- Operators migrating from upstream NetBird may carry over a
  permissive default policy from the older onboarding flow; if
  they don't, they hit this on the first openZro Resource.
- Diagnosis is unintuitive — the first-look symptom is "DNS
  resolves but nothing connects after I touch this hostname",
  which suggests routing/DNS, not ACL.

## References

- Initial misdiagnosis was rooted in
  [handler.go:480](../../client/internal/routemanager/dnsinterceptor/handler.go#L480)
  (the Android-only gate) being a real bug, just not the one
  causing the freeze.
- Actual failing path: ACL evaluation between client peer and
  routing-peer group, dropped at the management firewall.
- Reproduction artefacts: shared GKE LB IP `198.51.100.42`
  serving every `*.example.com` host except `mgmt-grpc.example.com`
  (own LB by [ADR-0014](0014-coordinated-multi-pod-relay.md)).
