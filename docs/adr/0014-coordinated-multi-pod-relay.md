# ADR-0014 — Coordinated multi-pod relay for K8s deployments

## Status

**Accepted**, implementation landed across `relay/server/cluster/`,
`relay/server/clusterboot.go`, `relay/cmd/root.go`, and the
[openzro/helms](https://github.com/openzro/helms) chart at
`2.1.0-alpha.8`. Lab chaos validation (pod kill mid-stream,
scale-up, partition) remains as a follow-up before the chart leaves
the alpha line.

Single-pod deployments are unaffected — `--cluster-headless` is
opt-in, and the chart only renders the new Headless Service +
downward-API env vars when `relay.replicaCount > 1` (or the
operator sets `relay.cluster.enabled: true` explicitly).

## Context

NetBird's relay binary keeps connected peers in a per-process,
in-memory store ([`relay/server/store/store.go`](../../relay/server/store/store.go)).
A relayed session between peers A and B requires both to land on
the same process — when peer A asks the relay to forward to B, the
relay simply does a `store.Peer(id)` lookup and pipes the bytes
through.

That single-process assumption is fine for the way upstream NetBird
operates — they ship one relay deployment per region, each with a
distinct public URL (`relay-fra`, `relay-iad`, …), and rely on the
client's existing **foreign-relay dialing** (`Manager.OpenConn`) to
let cross-region peers reach a non-home relay. The OSS open source
build is explicitly active-passive at the relay tier; per-region
multi-replica with a single shared LB is not a supported topology.
Upstream maintainer braginini confirmed the gating in
[netbirdio/netbird#5724](https://github.com/netbirdio/netbird/issues/5724):
"active-active setup … refer to the enterprise commercial license."

For openZro, the upstream pattern is enough for self-hosters who run
a region per location and accept that scaling vertically is the
ceiling. an operator's self-hosted setup fits that profile exactly — one relay VM
in `southamerica-east1`, no K8s on the data plane side. **No
implementation work is required for that case.**

The pattern stops being enough when:

1. **K8s operator wants `replicaCount: N` to "just work"** — the
   typical reaction when an SRE sees `replicaCount: 1` is to bump
   it for HA. With a single LB IP in front, the round-robin lands
   peers A and B on different pods and they fail to pair
   (`isForeignServer` compares URL strings, so the pods look like
   the same relay even though they're different processes). This
   is the exact incident reported in [issue #9](https://github.com/openzro/openzro/issues/9).
2. **An operator hits scale a single pod can't sustain in one
   region** — relay is a fallback path, but CGN-heavy NAT
   environments push real bandwidth through it. At some throughput
   the answer is either bigger node (cap visible) or more pods
   (locked behind the OSS gap).
3. **openZro decides to offer SaaS** — multi-tenant data plane on
   shared relay infra is the kind of feature that has to scale
   beyond one box.

This ADR captures the design that closes the gap, so the work is
ready to ship the moment one of those triggers fires. It is **not**
authorising the implementation today.

## Decision

When the work happens, openZro adds a coordinated multi-pod relay
mode that turns "deploy N replicas behind one Service" into a
supported topology. The design has three commitments:

1. **K8s-only**. The operator UX win is K8s-shaped (StatefulSet,
   Headless Service, automatic discovery, single LB IP). Bare-metal
   VM operators keep using the existing per-region pattern, which
   already works and is documented separately under [ADR-0009](0009-bare-metal-ansible-and-ha.md).
2. **No new broker dependency.** The peer registry that lets
   pod-1 find pod-2 lives entirely within the relay binary, using
   Headless Service DNS for pod discovery and a broadcast-on-miss
   protocol over TCP. **No NATS, no Redis** in the data plane *or*
   the control plane of the relay.
3. **Hot path is direct TCP between pods.** Routing decisions for a
   peer live in a small, locally-cached lookup table. A peer-to-peer
   relay session is forwarded over a long-lived, multiplexed,
   plain-TCP stream between exactly two pods. No broker-mediated
   data plane, no per-packet RPC overhead.

## Architecture

### Topology

```
                          ╭────────────╮
       relay clients ─────│ LoadBalancer│ (1 IP, round-robin)
                          ╰──────┬─────╯
                                 │
              ┌──────────────────┼──────────────────┐
              ▼                  ▼                  ▼
     ┌──────────────┐  ┌──────────────┐  ┌──────────────┐
     │  relay-pod-0 │  │  relay-pod-1 │  │  relay-pod-2 │
     │  store: {A}  │  │  store: {B}  │  │  store: {C}  │
     │  cache:      │  │  cache:      │  │  cache:      │
     │   B→pod-1    │  │   A→pod-0    │  │              │
     └──────┬───────┘  └──────┬───────┘  └──────┬───────┘
            │                 │                 │
            └─────────────────┼─────────────────┘
                              │
                Headless Service (relay-internal):
                relay-0.relay-internal.svc, …
                long-lived TCP between every pod-pair
                multiplexed per-stream
```

- **Single LB Service** facing the public, type `LoadBalancer`.
  Round-robin is fine — the design stops caring which pod a peer
  lands on.
- **Headless Service** (`clusterIP: None`) for in-cluster discovery.
  Each pod resolves the Service's DNS and learns the IPs of the
  other pods.
- **StatefulSet** for the relay (gives stable pod names like
  `relay-0`, `relay-1`, used in DNS).
- **Inter-pod traffic stays in the cluster network**. NetworkPolicy
  isolates it; TLS is unnecessary on a trusted backplane and would
  add measurable per-packet cost.

### Per-pod state

Each pod has:

```go
type Server struct {
    // existing single-pod state
    localStore  *store.Store          // peers connected directly to this pod

    // new for multi-pod
    cluster     ClusterDiscovery      // DNS-watched list of peer pods
    locator     *PeerLocator          // mark_hash → pod address (cache)
    interpod    *InterpodForwarder    // long-lived TCP per pod-pair
}
```

`localStore` is the existing `relay/server/store/store.go` —
unchanged. It owns the peers that registered with **this** pod.
`cluster` is a tiny helper that periodically resolves the Headless
Service DNS to learn `[]string` of other pod addresses (10s
interval, refreshed lazily on lookup miss). `locator` is a
short-TTL cache of `peer_id → pod_internal_addr`. `interpod` owns
the long-lived TCP connections to the other pods.

### Connection flow — same pod

Peer A registered at pod-0. Peer A asks pod-0 to open a session to
peer B. pod-0 checks `localStore`, finds B locally, pipes bytes via
the existing single-pod path. **Zero overhead vs the current
implementation.** The same-pod fast path matters because:

- A randomly-selected pair of peers in an N-pod cluster has a
  `1/N` chance of co-locating;
- During gradual rollout, all peers might still be on one pod for
  some window; we don't want to penalise that.

### Connection flow — cross pod

Peer A registered at pod-0. Peer A asks pod-0 for peer B, who is at
pod-1.

```
 pod-0                                                pod-1
   │
   │ A: OPEN_CONN(B)
   │
   ├── localStore.Peer(B) → miss
   ├── locator.Get(B)     → miss
   │
   │ broadcast WHO_HAS(B) on inter-pod stream
   │     ──────────────────────────────────────►
   │                                              localStore.Peer(B) → hit
   │                                              ◄─── I_HAVE(B, seqno=42)
   │
   ├── locator.Set(B, pod-1, ttl=5min)
   │
   │ open peer-conn frame on stream-to-pod-1
   │     ──────────────────────────────────────►
   │                                              accept, plumb to B
   │                                              ◄─── ACK
   │
   │ A: data ─►
   │     ──────────────────────────────────────►
   │                                              ──► B
   │                                              ◄── data
   │     ◄──────────────────────────────────────
   │ ◄── A: data
```

After the locator caches `B→pod-1`, subsequent connections from any
peer on pod-0 to B skip the broadcast.

### Inter-pod transport

Raw TCP. No TLS, no HTTP/2, no gRPC. The framing format is one
length-prefixed message:

```
┌──────────┬──────────┬──────────┬──────────────┬──────────┐
│ varint   │ uint8    │ uint8    │ payload      │ trailing │
│ frame_len│ msg_type │ pad/flag │ <peer ids,   │  bytes   │
│          │          │          │  payload>    │          │
└──────────┴──────────┴──────────┴──────────────┴──────────┘
```

Message types:

- `WHO_HAS` (peer_id) — sent on miss
- `I_HAVE` (peer_id, seqno) — answer from the pod that owns it
- `NACK` (peer_id) — answer from a pod that doesn't have it; can be
  silent in the broadcast case
- `OPEN` (src_peer, dst_peer) — open a forwarding channel
- `DATA` (channel_id, bytes) — relay data, the hot frame
- `CLOSE` (channel_id) — close the channel
- `PING` / `PONG` — health check, ~5s interval

`channel_id` is a small uint that pod-0 picks per `(peer_a, peer_b)`
pair so individual relayed sessions don't need their own TCP stream.

### Discovery

The relay binary takes two flags:

```
--cluster-discovery=k8s-headless
--cluster-headless-svc=relay-internal
```

When `cluster-discovery=k8s-headless` (the only mode this ADR
introduces), the pod periodically resolves the Headless Service via
its in-cluster DNS, builds a `[]netip.Addr` of the other pods, and
maintains a TCP stream to each. Self-IP is filtered out via the
downward API (`POD_IP` env var).

When `cluster-discovery` is unset (the default), the relay runs in
**single-pod mode** — exactly today's behaviour. **Existing
deployments are not affected by this ADR's code unless they opt
in.**

## Performance budget

| Hop | Latency | Notes |
|---|---|---|
| External LB → pod | 0.1 – 0.3 ms | Cluster LB pass-through |
| TLS handshake (peer ↔ relay) | 50 – 100 ms | Once per session |
| Local store lookup | < 0.05 ms | Map read under RWMutex |
| Locator cache hit | ~ 0.05 ms | Map read |
| Locator cache miss + broadcast | 1 – 3 ms | One round-trip across all peers |
| Inter-pod stream setup | 0.5 – 1 ms | First packet only; pre-warm makes this 0 |
| **Inter-pod forward, per packet** | **0.2 – 0.4 ms** | TCP between pods, no TLS, no HTTP/2 |

The numbers add up: **a relayed cross-pod session pays roughly
0.3 ms per packet on top of today's same-pod session**. The peer-to-
peer WAN distance, which is the realistic floor for any relayed
session, is at least 5 ms in the same region and 100 + ms across
regions. The 0.3 ms intra-cluster overhead is single-digit percent
of what the WAN already costs.

Pre-warming inter-pod streams at startup is recommended (one
connection per pair on the first DNS resolve) so cross-pod traffic
never pays the TCP / TLS handshake on the first user packet.

## Failure modes

| Failure | What happens | Recovery |
|---|---|---|
| Pod-1 dies mid-session | TCP stream from pod-0 to pod-1 reads EOF; channel state on pod-0 is torn down; the peer's session reports closed; client reconnects through the LB | Standard client reconnect behaviour. Same as today's "relay restarted" UX. |
| Pod-1 process restart | DNS resolves to the new pod; old TCP stream reads EOF; pod-0 dials new IP; locator entries pointing at the dead pod TTL-expire (5 min) | Brief connection failures during the window; auto-recovers without operator action. |
| DNS lag during pod scale-up | New pod joins the StatefulSet; existing pods see it on next DNS poll (≤ 10 s); locator cache works without it during the window | New pod's peers don't reach existing peers for up to 10 s. Acceptable for fallback path. |
| Network partition between two pods | TCP stream drops; locator entries pointing at the unreachable pod expire; future broadcasts fail to find the peer; forward path returns a "peer unreachable" error to the client | Client retries / falls back to direct WG hole-punching where applicable. |
| Locator cache poisoned (stale entry) | Forward attempt hits TCP-RST or NACK from the addressed pod; pod-0 invalidates and re-broadcasts | Self-healing in 1 RTT. |
| Race: two pods both think they own peer B (B reconnected) | Both reply `I_HAVE(B, seqno)` on the broadcast; the higher seqno wins | Tie-break by seqno; loser's local view fixes itself on next conntrack churn. |

There is **no scenario where coordination failure causes silent
data corruption or wrong-peer routing** — the design fails closed.

## Why not …

- **NATS / Redis pub-sub for the data plane.** Earlier sketches
  used NATS as the broker. Killed because: (a) puts a broker on the
  hot path → ~1–2 ms per message, broker becomes a bottleneck and
  a new failure mode; (b) adds an external dependency for an
  otherwise self-contained binary; (c) the same routing-by-broadcast
  protocol works fine without it for the size of clusters relays
  realistically run at (≤ 10 pods).
- **Per-pod URLs (Postura A).** The original NetBird-style
  workaround: give each pod a distinct external URL, let the client
  do `foreign-dial`. Works, but requires the operator to provision
  N LB IPs / N DNS records / N entries in `Relay.Addresses`. The
  operator complexity grows linearly with pod count, which is
  exactly the thing K8s deployments expect to abstract. We keep
  this pattern documented in [ADR-0009](0009-bare-metal-ansible-and-ha.md)
  for VM and bare-metal deployments where it's the natural fit, but
  this ADR's Headless-Service design is the K8s-native answer.
- **NATS embedded in the relay binary.** Replaces external NATS
  with in-process NATS clustered between pods. Doesn't actually
  remove NATS — just hides it. Same complexity, debugged inside the
  relay's process now. Net negative for operations.
- **Sticky LB session affinity.** "What if the LB just routes A and
  B to the same pod?" The LB doesn't read the relay handshake to
  know which peers will eventually pair, so it can't make routing
  decisions on that basis. Source-IP hashing also doesn't help —
  A and B have different source IPs.
- **Shared kernel state (eBPF / shared memory).** Only works for
  pods on the same node, which K8s anti-affinity should explicitly
  prevent for HA reasons. Doesn't generalise.

## Operator-facing changes

```yaml
# values.yaml (when this ADR ships)
relay:
  replicaCount: 3                # was: must be 1
  cluster:
    discovery: k8s-headless      # opt-in; default is single-pod
    headlessService: relay-internal
  service:
    type: LoadBalancer           # one IP, unchanged
  internalService:               # new
    enabled: true
    type: ClusterIP
    clusterIP: None              # headless
    port: 7090                   # internal only
```

The chart provisions both Services. `relay-internal` is bound to
NetworkPolicy so only relay pods can talk to it. The relay pod's
container picks up `POD_IP` and `POD_NAME` via the downward API.

## Security

- **Inter-pod traffic is plain TCP.** This is acceptable because
  the cluster network is a trusted backplane (NetworkPolicy
  prevents any non-relay pod from reaching `relay-internal:7090`).
- **No new attack surface for external peers.** External peers
  speak the existing relay protocol on the existing port. The
  inter-pod port is only exposed to other relay pods.
- **Peer identity is unchanged.** PolicyID checking, HMAC
  authentication of relay sessions, all of that lives at the relay
  binary level and is identical between same-pod and cross-pod
  sessions.

## Out of scope

- **VM / bare-metal multi-pod.** Operators in those environments
  use the per-region pattern from [ADR-0009](0009-bare-metal-ansible-and-ha.md).
  the operator's data plane is in this category and **never invokes the
  code in this ADR**.
- **Cross-region multi-pod.** Each region runs its own coordinated
  cluster (or a single-pod cluster — see flag `cluster.discovery`).
  Cross-region routing keeps using the client's foreign-dial
  protocol — that's already what works and we don't move it
  inside the relay.
- **Operator scaling beyond 10 pods.** The broadcast-on-miss
  protocol assumes a single-digit pod count. If a deployment grows
  past that, we revisit with a different routing strategy
  (consistent hashing, dedicated lookup service). Tracked separately
  if/when it happens.
- **Relay session migration on pod restart.** Sessions whose owning
  pod restarts simply close. Clients reconnect. We don't try to
  preserve session state across pod restarts — the cost-benefit
  doesn't justify the complexity.

## Trigger conditions

This ADR is shelved at "Proposed" until **at least one** of the
following becomes true:

1. **A self-hosting operator opens an issue or reaches out** asking
   for `relay.replicaCount > 1` support, with a concrete deployment
   description (peer count, region count, why per-region single-pod
   isn't acceptable).
2. **openZro launches a SaaS** product where the relay tier needs
   to scale horizontally per region without operator intervention.
3. **A regression in the upstream** (or an unrelated openZro
   change) makes single-pod relay materially less reliable, and
   the workaround is to have multiple pods.

When triggered, this ADR walks straight into implementation — the
design questions are already settled. Estimated effort:
**1.5 – 2 weeks of focused work**, broken roughly:

| Phase | Effort | Notes |
|---|---|---|
| Inter-pod TCP framing + multiplex | 3 days | New package `relay/server/cluster` |
| `PeerLocator` + cache + broadcast | 2 days | TTLs, race tests, concurrency |
| `ClusterDiscovery` (Headless DNS) | 1 day | Wraps `net.LookupHost` + ticker |
| Server wiring (existing same-pod path stays) | 2 days | Feature flag gates the new path |
| Helm chart updates | 1 day | StatefulSet, Headless Svc, downward API |
| Chaos test + benchmarks | 2 days | Pod kill mid-stream, scale-up, stale cache |
| Smoke in lab | 1 day | Two real peers across two pods |
| Docs (operator README + this ADR transition to Accepted) | 0.5 day | |

## References

- [`relay/server/store/store.go`](../../relay/server/store/store.go)
  — current single-pod store.
- [`relay/client/manager.go::OpenConn`](../../relay/client/manager.go)
  — `isForeignServer` and the existing foreign-dial path.
- [ADR-0009](0009-bare-metal-ansible-and-ha.md) — bare-metal HA via
  per-region deployments. The fallback / non-K8s answer.
- [openZro issue #9](https://github.com/openzro/openzro/issues/9) —
  original lab incident with `relay.replicaCount=2`.
- Upstream NetBird issue
  [netbirdio/netbird#5724](https://github.com/netbirdio/netbird/issues/5724)
  — confirms upstream OSS gates active-active.
