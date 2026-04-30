# ADR-0009 — Bare-metal Ansible flow, HA via embedded NATS, Dex single-ingress

**Status:** Accepted (2026-04-30)

**Supersedes:** none

**Relates to:** [ADR-0006](0006-embed-dex.md) (embedded Dex IdP),
[ADR-0007](0007-client-packaging.md) (client packaging),
[ADR-0008](0008-kubernetes-helm-operator.md) (K8s/helm deployment)

## Context

ADR-0008 covers the K8s deployment story: one Helm chart at
`openzro/helms` deploys management + signal + relay + dashboard +
embedded Dex onto an existing cluster. That works for operators
who already run K8s.

It doesn't cover three real-world cases:

1. **VPS / bare-metal hosts** without Kubernetes. A small team
   running openZro on three Hetzner / DigitalOcean / Linode boxes
   shouldn't need to stand up RKE2 first.
2. **Cloud VMs without managed K8s**. EC2 + an ALB is a perfectly
   reasonable production shape; pulling EKS in just to deploy four
   Go binaries is overhead.
3. **HA management** without an external broker. The K8s helm chart
   inherits HA "for free" via Pod replication + a managed
   Postgres, but the management daemon's internal cluster
   coordinator (`cluster.Coordinator` in `cluster/coordinator.go`)
   needs distributed state — historically that meant standing up a
   Redis or NATS cluster alongside.

This ADR captures three intertwined decisions made during a
multi-day sprint that landed in v0.53.1-alpha.13 → alpha.14:

- A. Build a generic bare-metal Ansible playbook
  (`github.com/openzro/openzro-ansible`) that operators can clone,
  parametrise via `group_vars`, and run.
- B. Use the embedded NATS + JetStream server inside `openzro-mgmt`
  itself as the default HA cluster coordinator — **no external
  broker required**.
- C. Reverse-proxy Dex on the same hostname under `/dex/*`
  (single-ingress) instead of the `dex.<host>` subdomain we used
  during early lab testing.

Each is independently useful; together they let an operator stand
up an HA-capable openZro deployment on three Ubuntu VMs with one
playbook run.

## A. Bare-metal Ansible playbook

### Decision

Ship a public, generic Ansible repo at
`github.com/openzro/openzro-ansible` (BSD-3, mirrors the openZro
project's license). Roles wrap the native server packages produced
by `openzro/openzro` — `openzro-management`, `openzro-signal`,
`openzro-relay`, `openzro-dashboard` — published to `pkg.openzro.io`
as deb / rpm / archlinux every tag.

**Layout:**

```
openzro-ansible/
├── inventories/
│   ├── lab/         single host, all-in-one, self-signed cert
│   └── production/  multi-host, HA-capable
├── playbooks/
│   ├── site.yml     full provisioning
│   └── update.yml   rolling upgrade with cloud-LB drain/undrain
└── roles/
    ├── common/                apt/yum repo + GPG key
    ├── openzro_management/    daemon + datastore + cluster coord
    ├── openzro_signal/        stateless rendezvous server
    ├── openzro_relay/         L4 WireGuard relay
    ├── openzro_dashboard/     apt install + render-env + nginx
    ├── openzro_nginx/         TLS termination + reverse proxy
    ├── openzro_nats_cluster/  optional standalone NATS
    ├── openzro_redis_cluster/ optional standalone Redis
    ├── aws_lb/                ALB + NLB + ACM + Route53
    └── gcp_lb/                HTTPS LB + NLB + managed cert + Cloud DNS
```

### Trade-offs considered

| Approach | Why we picked / didn't |
|---|---|
| **Ansible** ✓ | Idempotent, pull-based, no agent on targets, fits the small/medium team operator profile, cheap to learn |
| Salt / Puppet / Chef | Heavier, agent-based, longer learning curve. Marginal benefit for our scale (≤ 10 hosts per deployment). |
| Pure shell script | Hard to make idempotent at scale, no dry-run, poor multi-host primitives. install.sh is for single-host one-shot only. |
| Terraform / Pulumi | Overlaps our intent for the cloud-LB roles, but they don't manage `apt install` cleanly. Use Terraform later if/when operators want it as a sibling of Ansible — not as a replacement. |
| Docker Compose stacks | Forces Docker daemon as a SPOF, splits logs, adds 200-300 MB resident memory per host, complicates WireGuard kernel access. **openZro is network infrastructure** — daemon needs to be as low-overhead as `tailscaled` or `wg-quick`. |

### Native binaries vs containers (bare-metal)

For the Ansible flow, native systemd units are mandatory:

- `openzro-management` (Go binary, CGO_ENABLED=0, ~30 MB) → systemd
- `openzro-signal` (stateless) → systemd
- `openzro-relay` (kernel-touching for WG hooks) → systemd
- `openzro-dashboard` (Next.js static export) → nginx serves files

The helm chart still builds the dashboard as a container (with
nginx + static bundle baked in) for K8s. Bare-metal flow installs
the static files via the dashboard package and the operator's
existing nginx serves them.

This is the same architectural call Tailscale, Postgres, and
Caddy take for bare-metal: native binaries + systemd, containers
only for K8s.

## B. HA management via embedded NATS

### Decision

Default the cluster coordinator to **`embedded`** — each
`openzro-mgmt` process runs an internal NATS+JetStream server on
loopback (`127.0.0.1:4222`), and instances gossip with each other
over `tcp/6222`. No external broker process required.

Operators who want a separately-monitored broker can set
`openzro_cluster_backend: nats` (standalone NATS via the
`openzro_nats_cluster` role) or `redis` (standalone Redis via the
`openzro_redis_cluster` role). Both alternatives install the
broker on the management hosts themselves.

### Why embedded NATS over external Redis / NATS

| Backend | Extra deps | Distributed lock | JetStream | Operational cost |
|---|---|---|---|---|
| **embedded** ✓ | none | gossip + Raft (NATS native) | yes | ≈ +20 MB RAM per management replica |
| nats (standalone) | `nats-server` daemon | gossip + Raft | yes | separate process, separate restart cycle, separate logs |
| redis | `redis-server` daemon | RedLock | no | sentinel for HA Redis is a non-trivial choreography |

The `cluster/embedded` package wraps
`github.com/nats-io/nats-server/v2` directly — it's NATS the
project, not a clone or re-implementation. Embedding it saves
operators one daemon to keep alive.

**Verified:** the `cluster/factory.NewFromEnv` switch path for
`OPENZRO_BROKER=embedded` builds a Coordinator pointed at
loopback. Two replicas with `OPENZRO_CLUSTER_PEERS` pointing at
each other form a working cluster after ~2-3 seconds of gossip
convergence.

### Firewall

Single requirement: `tcp/6222` between management hosts. Default
NATS cluster port. The `openzro_management` role auto-derives the
peer list from the inventory's `management` host group, but the
operator still needs to open the port on whatever firewall /
security group sits in front.

## C. Dex single-ingress (path `/dex` on the same hostname)

### Decision

Reverse-proxy Dex at `<host>/dex/*` from the same nginx that
serves the dashboard, management API, and signal gRPC. **Do NOT**
add `nginx.org/rewrites` or any other path-rewrite annotation.

The helm chart now ships a `templates/dex-ingress.yaml` matching
this convention; the Ansible `openzro_nginx` role wires the same
shape into its templated nginx config.

### Why no path rewrite

Dex's template engine (`server/templates.go`'s `relativeURL`
function) computes static-asset paths from `issuerURL.Path`. With
`issuer: https://example.com/dex`, Dex knows it's mounted at
`/dex/*` and emits relative URLs (`../theme/styles.css` resolves
to `/dex/theme/styles.css`).

If the ingress strips the `/dex` prefix before forwarding to Dex,
Dex receives requests like `/auth` (instead of `/dex/auth`), the
relativeURL function gets confused, and it emits paths under `/`
instead of `/dex/` — the browser then 404s on `/theme/styles.css`.

This was the trap we walked into during the v0.53.1-alpha.7 lab
smoke. The escape hatch we used (run Dex on a separate
`dex.<host>` subdomain) worked but requires an extra DNS record,
extra cert, and same-origin cookie / CORS gymnastics.

### Trade-offs

| Approach | DNS records | Certs | CORS / cookies | Path-prefix bug |
|---|---|---|---|---|
| **Single-ingress (`/dex`)** ✓ | 1 | 1 | same-origin | already handled by Dex's relativeURL — just don't rewrite the path |
| Subdomain (`dex.<host>`) | 2 | 2 (or wildcard) | cross-origin — needs `Access-Control-Allow-Origin` + `Domain=.<host>` cookie | N/A |

The single-ingress shape is the default for new deployments.
Existing deployments that already cut over to a subdomain don't
need to migrate — both work. The chart leaves Dex's ingress
disabled by default (`dex.ingress.enabled: false`) so neither path
fires until an operator picks one.

## Consequences

**Positive:**
- Operators on bare-metal / VPS / cloud VMs without K8s can deploy
  openZro with one playbook command. Sub-15-minute first-time
  provisioning.
- HA management costs zero extra infrastructure — just add a
  second host to the `management` group and open one port.
- Single TLS cert covers dashboard + management API + signal +
  Dex. One certbot HTTP-01 challenge handles the renewal for the
  whole stack.
- Same release artifacts feed both Helm chart (K8s) and Ansible
  (bare-metal). Tag once, ship both paths.

**Negative:**
- Two deployment surfaces to keep in sync (Helm + Ansible). When a
  new component / feature lands, both paths need to be updated.
  Mitigated by reusing the same release artifacts and the same
  `management.json` schema.
- Embedded NATS adds ~20 MB RAM per management replica. Negligible
  for our scale; flagged here for completeness.
- The Ansible `aws_lb` / `gcp_lb` roles use community.aws /
  google.cloud collections, which lag behind Terraform providers.
  Operators with > 10 cloud resources or multi-region / HA-cloud
  needs should layer Terraform in front of Ansible. Documented in
  the `openzro-ansible/README.md` "Cloud LB" section.

**Neutral:**
- Postgres bootstrap is still operator-owned. The `production`
  inventory assumes managed Postgres (RDS / Cloud SQL) or
  hand-installed. Building an `openzro_postgres` role with
  Patroni-style HA is out of scope for this ADR — separate
  follow-up if demand exists.

## What's NOT here yet

- Backup / restore for the management datastore. Today it's
  whatever the chosen backend (sqlite / Postgres) provides.
- Sentinel-based automatic failover for the optional
  `openzro_redis_cluster` role. Master/replica only — manual
  promotion. For real HA Redis, point at managed Elasticache or
  Memorystore.
- Multi-region / cross-region HA. The `production` inventory has
  the host groups but no region awareness. Per-region instances
  of the Ansible playbook is the current workaround.
- Dashboard server package on macOS / Windows. The dashboard is
  Linux-only as a deb/rpm/archlinux package; Windows / macOS
  dashboards are still container-only via the helm chart.

These are tracked individually in the openzro-ansible repo's
README under "What's NOT here yet" — kept out of this ADR so
future updates don't churn the formal record.

## References

- `openzro/openzro-ansible` — public playbook repository
  (BSD-3, mirrors openZro)
- `cluster/embedded/embedded.go` — NATS server wrapper
- `cluster/factory/factory.go` — coordinator selection from env
  vars
- `dex/server/templates.go` — relativeURL implementation that
  makes path-prefix work
- ADR-0008 — K8s/helm parallel deployment story
- helm chart 2.0.0-alpha.7 — first release with the
  `templates/dex-ingress.yaml` single-ingress template
