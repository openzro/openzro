# ADR-0008: Kubernetes deployment — Helm chart + Operator

- **Status**: Accepted (Stages 1 + 2 shipped 2026-04-29; chart
  `openzro-2.0.0-alpha.3` published at https://openzro.github.io/helms,
  operator image at `ghcr.io/openzro/openzro-operator:0.3.2-alpha.1`,
  Stage 3 + 4 deferred — see "Plan" below)
- **Date**: 2026-04-28
- **Decision-makers**: openZro maintainers
- **Related**: [ADR-0006](./0006-embed-dex.md) (embedded Dex IdP),
  [ADR-0007](./0007-client-packaging.md) (client packaging)

## Context

openZro until now has been deployable in two shapes:

1. **`docker-compose`** (`infrastructure_files/docker-compose.yml.tmpl`)
   for self-hosters running on a single VM. Configured by
   `infrastructure_files/configure.sh` which generates the mTLS PKI,
   Dex config, and management.json.
2. **Manual binary install** for the agent on Linux/macOS/Windows
   (covered by ADR-0007's MSI/PKG/install.sh path).

Operators on Kubernetes had no first-class option — they had to
either run docker-compose inside a VM that K8s schedules
(awkward), translate the compose file to Deployments by hand
(error-prone), or fork the upstream NetBird helm chart (license-
hostile post-AGPL, and the upstream's chart didn't bundle Dex).

Two sibling repos cover this gap, **forked at our v0.53.0 BSD-3
fork point** so the license posture stays clean:

- **`openzro/helms`** — forked from `netbirdio/helms` (BSD-3
  `main` HEAD, 2026-04-26). Three charts: `openzro` (control
  plane), `openzro-operator`, `openzro-operator-config`.
- **`openzro/openzro-operator`** — forked from
  `netbirdio/kubernetes-operator` (BSD-3 tag `v0.3.1`, 2026-04-24).
  Five CRDs that reconcile peers, networks, policies, setup keys,
  and routing peers as Kubernetes resources.

Both repos came in lightly-rebranded but functionally **broken at
the fork point**: the operator pinned a NetBird version that
didn't exist in our core; the chart referenced upstream image
registries; neither knew about ADR-0006's Dex pivot.

## Decision

Land both repos as **first-class K8s deployment paths** for openZro,
with the changes documented below. Operators on K8s install via:

```bash
helm repo add openzro https://openzro.github.io/helms
helm install openzro openzro/openzro \
  --create-namespace \
  --namespace openzro \
  -f my-values.yaml
```

or via OCI:

```bash
helm install openzro oci://ghcr.io/openzro/charts/openzro \
  --version 2.0.0-alpha.1 \
  -f my-values.yaml
```

`my-values.yaml` minimally sets `dex.config.issuer` (your
domain), `dex.config.staticPasswords` (rotate the bootstrap admin),
plus optional Gateway API configuration.

The operator (CRDs + reconcilers) is a separate `helm install` — it
talks to the openZro management API (Personal Access Token auth)
and reconciles `OZGroup` / `OZPolicy` / `OZSetupKey` / `OZResource`
/ `OZRoutingPeer` resources from Kubernetes manifests into the
openZro account.

## What changed at this commit (Helms)

`openzro/helms` repo:

- **`charts/openzro/Chart.yaml`** — `version: 2.0.0-alpha.1` (chart
  reset for fork + Dex subchart introduction), `appVersion: "0.53.1-alpha.1"`
  (tracks the openzro/openzro release).
- **`charts/openzro/values.yaml`** — image refs aligned to
  `ghcr.io/openzro/{management,signal,relay,dashboard}` (matches
  `.goreleaser.binaries.yaml` publish targets).
- **`charts/openzro/values.yaml`** + **`Chart.yaml`** — Dex
  subchart (`dex/dex@0.23.0`) wired as a conditional dependency,
  enabled by default, configurable via `dex.enabled: false` to
  point at an external Dex. Default config seeds the
  openzro-dashboard SPA client (PKCE), bootstrap admin
  staticPassword, mTLS gRPC (mountable via cert-manager),
  `DEX_API_CONNECTORS_CRUD=true` feature gate.
- **`charts/openzro/templates/gatewayapi.yaml`** (new) — Gateway
  API resources (HTTPRoute / GRPCRoute) opt-in via
  `gatewayApi.enabled`, parallel to the existing Ingress
  templates. Supports both bundled-Gateway and externally-
  managed-Gateway modes via `createGateway` flag.
- **`.github/workflows/helm.yml`** — extended the existing
  `chart-releaser-action` step (gh-pages publish) with an OCI push
  to `ghcr.io/openzro/charts/`. Both install paths stay in sync.

## What changed at this commit (Operator)

`openzro/openzro-operator` repo:

- **Path rewrite** — 35 import lines across 19 files moved from
  `github.com/openzro/openzro/shared/management/...` (upstream's
  post-fork layout) to `github.com/openzro/openzro/management/...`
  (our fork's layout).
- **`go.mod`** — `replace github.com/openzro/openzro => ../openzro`
  for sibling-repo development. CI substitutes a pseudo-version
  pin (`go get github.com/openzro/openzro@<sha>`) before build.
- **`NB*` → `OZ*` rename** across all CRDs (`NBGroup` → `OZGroup`,
  etc.), 40 files / 807 references. CRD plurals were already
  lowercase `oz*` at the fork point; the Kind capitalization is
  now consistent. **Clusters running the unbranded fork-point
  operator cannot in-place upgrade — Kind is part of the K8s API
  identity. Fresh install + recreate CRD resources.**
- **Reconciler field rename** — the unexported `openZro` field on
  every Reconciler struct became exported `OpenZro`, so
  `cmd/main.go` (different package) can populate via struct
  literals (Go visibility rules).
- **File renames** — `nb*.go` → `oz*.go` in `api/v1/`,
  `internal/controller/`; `crds/openzro.io_nb*.yaml` →
  `openzro.io_oz*.yaml`; `helm/openzro-operator/templates/nb*.yaml`
  → `oz*.yaml`.

## What changed at this commit (`openzro/openzro` core)

API surface backports (clean-room, BSD-3) so the operator's
imports resolve. Server-side handlers + storage migrations are
**not** implemented in this commit — runtime calls into the
backported features will return 404 until Tier 2 work ships.
Tier 2 is tracked in
`/home/kleber/.claude/projects/-home-kleber-Dados-openzro-openzro/memory/project_enterprise_gaps.md`
under "DNS Zones" and "Reverse-proxy Services".

| Artifact added | Purpose |
|---|---|
| `management/server/http/api/dns_zones.go` | `Zone`, `ZoneRequest`, `DNSRecord`, `DNSRecordRequest`, `DNSRecordType*` |
| `management/server/http/api/reverse_proxy.go` | `Service`, `ServiceRequest`, `ServiceTarget`, mode/protocol/target-type enums |
| `management/client/rest/dns_zones.go` | `DNSZonesAPI` — 8 methods (zones + records CRUD) |
| `management/client/rest/reverse_proxy.go` | `ReverseProxyServicesAPI` — 5 methods |
| `management/client/rest/errors.go` | `*APIError` typed error + `IsNotFound` / `IsUnauthorized` / `IsForbidden` / `IsConflict` helpers |
| `management/client/rest/options.go` | `WithUserAgent(string) option` for client identification in mgmt server logs |
| `management/client/rest/groups.go` | `GroupsAPI.GetByName(ctx, name)` — looks up groups by display name (404 → `*APIError`) |

`management/server/http/api/dns_zones.go` and
`reverse_proxy.go` live as hand-written files (not in
`types.gen.go`) because the OpenAPI spec hasn't been extended
yet. When the server side ships, the spec gains the endpoints and
codegen will continue producing `types.gen.go` alongside —
the hand-written files can either merge or be deleted at that
time.

## Trade-offs considered

### Alternative A — keep upstream operator (re-fork from a tag aligned to v0.53)

NetBird's `kubernetes-operator` history doesn't tag a clean
intermediate version that matches our v0.53.0 fork point — the
`shared/management/` package layout was introduced in their core
post-v0.53. Re-forking from an older operator tag would lose
recent features (Gateway API integration, NetworkResource
controller).

Rejected: backporting the API surface is bounded (~200 LOC of
types + client methods, types.gen.go untouched) and gives us
the most-recent operator features, which include things we
specifically want (Gateway API).

### Alternative B — strip DNS-zone / reverse-proxy controllers from the operator

Comment out the controllers that depend on the missing API
surface, ship a "core operator" with just the 5 native CRDs.

Rejected: the controllers ARE the value-add of running the
operator on top of bare K8s manifests. Without zones + services,
the operator is mostly a thin shim. Tier 1 + Tier 2 split lets
us ship the operator now (builds, tests pass) and backfill the
server side without re-touching the operator.

### Alternative C — bundle Dex via init container instead of subchart

Pull the dexidp/dex image from inside the openzro management
deployment, run as a sidecar init container. Avoids the helm
dependency on `dex/dex@0.23.0`.

Rejected: fights the K8s sidecar model (Dex needs its own
service + ingress + RBAC), and we'd reimplement what the
upstream Dex chart already does. The subchart is bounded —
disabling it (`dex.enabled: false`) is a one-line override for
operators using an external Dex.

### Alternative D — kustomize instead of helm

Kustomize-style overlays for self-hosters. Pros: no templating,
no Go template syntax. Cons: smaller community in the openZro
audience (mostly self-hosters on Helm), no equivalent of
goreleaser's helm chart publishing pipeline, harder for upstream-
NetBird-migrating users.

Rejected: helm is the de-facto standard for our audience.
Kustomize can layer on top of the chart's output if needed
(`helm template … | kubectl kustomize -`).

## Plan

This ADR covers the build-green, tests-passing checkpoint. Field
validation needs:

### Stage 1 — chart publishing first release ✓ shipped 2026-04-29

Chart `openzro-2.0.0-alpha.3` (chart version) tracking
`appVersion: 0.53.1-alpha.1` (core release) is published at:

- gh-pages: https://openzro.github.io/helms — operators add via
  `helm repo add openzro https://openzro.github.io/helms`
- OCI: `oci://ghcr.io/openzro/charts/openzro:2.0.0-alpha.3` —
  manually bootstrapped (the CI step is non-blocking, see
  "Open questions" below for the namespace-collision rationale)

### Stage 2 — operator publishing ✓ shipped 2026-04-29

`openzro/openzro-operator` tagged `v0.3.2-alpha.1`. CI publishes
multi-arch image at `ghcr.io/openzro/openzro-operator:0.3.2-alpha.1`
(linux/amd64 + linux/arm64) on every tag push. The operator's
helm chart at `openzro-operator-0.3.2-alpha.1` references the
matching image tag automatically.

### Stage 1 (interim) — kind / k3d smoke test (deferred)

Field validation against a real cluster needs a `helm install`
end-to-end and a CRD-reconcile flow. Pre-reqs are met (chart
publishes, operator image publishes, package visibility is
flippable to public via UI), so this becomes a single
afternoon's work whenever an operator or maintainer takes it
on. Not blocking the release.

### Stage 3 — server-side handlers for DNS Zones + Reverse Proxy Services

Out of scope for this ADR — tracked separately in the
enterprise gaps memo. When that ships:

- The operator's `NetworkResource` controller starts succeeding
  (no longer 404 on DNS record CRUD).
- The `HTTPRoute` controller starts succeeding (no longer 404 on
  reverse-proxy service CRUD).
- No changes needed in this ADR's chart or operator commit;
  the API surfaces in `openzro/openzro` were designed against
  the operator's expected shapes.

### Stage 4 — Apple Developer ID + SignPath EV signing

Already tracked under [ADR-0007](./0007-client-packaging.md)
issues #1 and #2. When those land, the chart's
`dashboard.config.adminCallback` UI hint can drop the
"unsigned client" disclaimer for Windows/macOS download buttons.

## Consequences

### Operator UX

K8s self-hosters get a one-command install. Dex, mTLS,
ingress/Gateway, and CRDs are all wired through values overrides
— the typical override file is ~30 lines.

### Brand surface

CRD Kinds (`OZGroup`, `OZPolicy`, …) and resource paths
(`oz.openzro.io/v1/ozgroups`) match the project name. Manifests
operators write read consistently, e.g.:

```yaml
apiVersion: openzro.io/v1
kind: OZGroup
metadata:
  name: backend-team
spec:
  name: "Backend Team"
```

### Code complexity

Three repos to maintain (`openzro/openzro` + `openzro/helms` +
`openzro/openzro-operator`). The operator's API surface is a
subset of the core's; backported types provide the contract.
When the server side ships handlers, the contracts already
exist on both sides.

### Backwards compatibility

There is none — this is a fresh fork, and clusters running the
upstream operator must recreate resources under the new
`openzro.io` group + `OZ*` Kinds. The ADR-0006 Dex pivot also
broke parity with the upstream chart's auth assumptions; bundled
Dex is the supported path.

## Open questions

- **Sub-chart for the operator config**: `openzro-operator-config`
  in the helms repo currently has minimal scaffolding. Should
  it bundle cert-manager Issuer + Certificate resources for the
  Dex gRPC mTLS pair, or stay neutral?
- **Helm chart for monitoring**: ServiceMonitor templates are
  there for prometheus-operator; operators on plain Prometheus
  or VictoriaMetrics don't get a usable scrape. Provide a
  PodMonitor variant or document the gap?
- **Operator multi-tenancy**: the operator authenticates as a
  single PAT against one openZro account. Should we support
  per-namespace PAT secrets so multiple tenants share one
  operator? (Probably yes — track separately.)
