# Deploying openZro on Kubernetes

This guide walks an operator from a fresh Kubernetes cluster to a
working openZro deployment with the dashboard, management API, signal,
relay, embedded Dex IdP, and (optionally) the openZro
Kubernetes operator that reconciles peers / groups / policies / setup
keys / network resources from CRDs.

**Current versions** (as of 2026-05-05):

| Artifact | Version | Source |
|---|---|---|
| Helm chart `openzro` | `2.1.0-alpha.11` (appVersion `0.53.1-alpha.38`) | https://openzro.github.io/helms |
| Helm chart `openzro-operator` | `0.3.2-alpha.1` | same repo |
| Container images | `0.53.1-alpha.38` (core) / `0.3.2-alpha.1` (operator) | `ghcr.io/openzro/{management,signal,relay,dashboard,openzro-operator}` |

For the architectural decisions behind this layout, see
[ADR-0008](../adr/0008-kubernetes-helm-operator.md). For the IdP
choice (Dex vs external), see [ADR-0006](../adr/0006-embed-dex.md).
For client-side packaging see [ADR-0007](../adr/0007-client-packaging.md).

## Prerequisites

- Kubernetes 1.27+ (Gateway API CRDs require 1.31+ if you choose
  that ingress path)
- `helm` v3.12+
- `kubectl` matching your cluster
- A DNS record pointing at your cluster's ingress / Gateway —
  this guide uses `openzro.example.com` throughout
- (Optional) `cert-manager` for TLS — the chart references certs by
  Secret name and lets cert-manager provision them
- (Optional) Gateway API CRDs installed if you want to use the
  Gateway-API path:
  ```
  kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/latest/download/standard-install.yaml
  ```

### Pull-secret note (private GHCR packages)

At the time of writing the openZro container packages on `ghcr.io`
are private by default. Until they are flipped to public via the
GitHub UI (`https://github.com/orgs/openzro/packages/container/<name>/settings`
→ Danger Zone → Change visibility → Public), `kubectl` needs an
`imagePullSecret` to pull. Create one with a Personal Access Token
that has `read:packages`:

```bash
kubectl -n openzro create secret docker-registry ghcr-openzro \
  --docker-server=ghcr.io \
  --docker-username=<your-github-handle> \
  --docker-password=<PAT-with-read:packages>
```

Then pass it to all components in your values override:

```yaml
management:    {imagePullSecrets: [{name: ghcr-openzro}]}
signal:        {imagePullSecrets: [{name: ghcr-openzro}]}
relay:         {imagePullSecrets: [{name: ghcr-openzro}]}
dashboard:     {imagePullSecrets: [{name: ghcr-openzro}]}
dex:           {imagePullSecrets: [{name: ghcr-openzro}]}
```

Once the packages are flipped public this section becomes obsolete
and the secret can be removed.

## Quick start

### 1. Add the helm repo

Two options — both publish on every tag push from
[openzro/helms](https://github.com/openzro/helms):

**Traditional helm repo (recommended for most operators):**
```bash
helm repo add openzro https://openzro.github.io/helms
helm repo update
```

**OCI registry (modern):**
```bash
# No `helm repo add` needed — install directly from OCI
helm install --version 2.1.0-alpha.11 \
  openzro oci://ghcr.io/openzro/charts/openzro
```

### 2. Author your values override

Copy `values.example.yaml` from the chart and customize:

```yaml
# my-openzro.yaml

management:
  ingress:
    enabled: true
    className: nginx
    hosts:
      - host: openzro.example.com
        paths:
          - path: /api
            pathType: Prefix
    tls:
      - hosts: [openzro.example.com]
        secretName: openzro-tls

dashboard:
  ingress:
    enabled: true
    className: nginx
    hosts:
      - host: openzro.example.com
        paths:
          - path: /
            pathType: Prefix
    tls:
      - hosts: [openzro.example.com]
        secretName: openzro-tls

dex:
  enabled: true
  config:
    issuer: https://openzro.example.com/dex
    web:
      allowedOrigins:
        - https://openzro.example.com
    staticPasswords:
      # REPLACE THIS bcrypt hash before going to prod.
      # Generate with:
      #   htpasswd -bnBC 10 "" yourpassword | tr -d ':\n' | sed 's/$2y/$2a/'
      - email: admin@openzro.example.com
        hash: "$2a$10$REPLACE_ME"
        username: admin
        userID: openzro-bootstrap-admin
    staticClients:
      - id: openzro-dashboard
        name: openZro Dashboard
        public: true
        redirectURIs:
          - https://openzro.example.com/auth
          - https://openzro.example.com/silent-auth
          - https://openzro.example.com/
```

### 3. Install the chart

```bash
helm install openzro openzro/openzro \
  --create-namespace \
  --namespace openzro \
  -f my-openzro.yaml
```

The chart's NOTES.txt prints the next-steps URL once everything is
healthy.

### 4. Verify

```bash
kubectl -n openzro get pods
# Expect: dex, management, signal, relay, dashboard all Running

kubectl -n openzro get ingress
# Expect: openzro-dashboard, openzro-management with your hostname

curl https://openzro.example.com/dex/.well-known/openid-configuration
# Expect: 200 with the OIDC discovery document
```

Open `https://openzro.example.com/`, sign in with the bootstrap
admin credentials. Settings → Authentication Providers →
"Add provider" wires Google / GitHub / Microsoft Entra / Keycloak
/ Okta / generic OIDC at runtime — see [ADR-0006](../adr/0006-embed-dex.md).

## Optional: Gateway API instead of Ingress

If your cluster runs Envoy Gateway, Cilium Gateway, Istio (ambient
mode), or any conformant Gateway API controller, set:

```yaml
gatewayApi:
  enabled: true
  gatewayClassName: envoy   # or istio / cilium / traefik
  createGateway: true
  gateway:
    hostname: openzro.example.com
    tls:
      certificateRefs:
        - name: openzro-tls

# Disable the per-component Ingress to avoid double-binding
management:
  ingress: {enabled: false}
  ingressGrpc: {enabled: false}
dashboard:
  ingress: {enabled: false}
signal:
  ingress: {enabled: false}
relay:
  ingress: {enabled: false}
```

The chart emits HTTPRoute resources for HTTP traffic (dashboard,
management REST, relay) and GRPCRoute for the gRPC services
(management gRPC, signal). For shared Gateways managed outside this
chart, set `gatewayApi.createGateway: false` and provide
`gatewayApi.parentRefs`.

## Optional: high availability (multi-replica)

The chart's defaults run one replica of each component — fine for
small teams and labs. Two HA paths are wired in:

### Management + signal

These two share the same broker mode toggle (`cluster.mode`):

```yaml
cluster:
  mode: embedded   # each pod runs its own NATS+JetStream cluster
  embedded:
    clientPort: 4222
    clusterPort: 6222

management:
  replicaCount: 3
signal:
  replicaCount: 3
```

In `embedded` mode the chart switches both deployments to
`StatefulSet`, renders a Headless Service that anchors per-pod DNS
names, and wires NATS routes via `OPENZRO_CLUSTER_PEERS` so the
embedded brokers gossip between siblings — see
[ADR-0009](../adr/0009-bare-metal-ansible-and-ha.md) for the
broker mode rationale.

If you already operate NATS at a known endpoint, point at it:

```yaml
cluster:
  mode: external
  external:
    url: nats://my-nats.svc.cluster.local:4222
```

### Relay (multi-pod fabric, ADR-0014)

The relay has its own multi-pod fabric independent of `cluster.mode`.
At `relay.replicaCount > 1` the chart auto-wires:

- A Headless Service (`<release>-relay-internal`) that resolves to
  every relay pod's IP (used for inter-pod discovery)
- A second container port (`relay.cluster.port`, default `7090`)
  for the inter-pod TCP fabric
- Downward API env vars (`POD_IP`, `POD_NAME`)
- An HMAC-SHA256 secret (auto-generated on first install, preserved
  across upgrades) that authenticates inter-pod HELLO frames

```yaml
relay:
  replicaCount: 3
  cluster:
    enabled: true        # null (default) = auto when replicaCount > 1
    port: 7090
    authSecret:
      value: ""              # leave empty for chart auto-gen
      # value: "your-32-char-secret"        # OR pin a literal
      # existingSecret: "my-relay-secret"   # OR point at your own
```

Operators with strict pod-to-pod NetworkPolicy must allow TCP/7090
between pods labeled `app.kubernetes.io/name: openzro-relay`. The
HMAC gate authenticates HELLO either way — NetworkPolicy is
defense-in-depth, not the primary trust boundary.

See [ADR-0014](../adr/0014-coordinated-multi-pod-relay.md) for the
full design (broadcast-on-miss locator, single-pod bypass, HELLO
handshake) and the trade-offs against alternatives like
state-replication (pfsync-style) or external coordination.

## Optional: geolocation database (MaxMind GeoLite2)

The dashboard's geolocation posture-check populates from a GeoLite2
database the management binary fetches on cold boot. By default it
pulls from the openZro mirror (`pkg.openzro.io`) — zero operator
config:

```yaml
# default — leave geoLite empty and country/city dropdowns just work
```

For first-party-only egress (or to fetch at MaxMind's freshness
cadence), provide a free MaxMind license key:

```yaml
management:
  geoLite:
    licenseKey:
      value: "abc123-your-key"             # OR
      # existingSecret: "my-mm-secret"     # ...point at your own
      # existingSecretKey: "licenseKey"    # ...with this key name
```

Get the free key at [maxmind.com/en/geolite2/signup](https://www.maxmind.com/en/geolite2/signup).
Air-gapped installs stage their own `GeoLite2-City_<date>.mmdb` in
the management `datadir` and pass `--disable-geolite-update=true`
via `management.extraArgs`.

## Optional: external database (postgres / mysql)

The chart auto-wires the management daemon, flow store, and activity
event store against PostgreSQL or MySQL when either subchart is
enabled. Each store gets its own database + dedicated user with
restricted grants, provisioned by a pre-install Helm hook:

```yaml
postgres:
  enabled: true
  username: openzro
  password: change-me-in-production

# OR

mysql:
  enabled: true
  rootPassword: change-me-in-production
```

Skip the auto-wiring entirely by leaving both `enabled: false` and
configuring DSNs manually via `management.envFromSecret`. See the
chart [`values.yaml`](https://github.com/openzro/helms/blob/main/charts/openzro/values.yaml)
for the full set of knobs.

## Optional: install the operator

The operator reconciles openZro's domain objects (groups, policies,
peers, setup keys, network resources) from Kubernetes manifests so
GitOps + multi-tenant patterns work naturally.

```bash
# 1. Issue a Personal Access Token in the dashboard:
#    Settings → Users → admin → Personal Access Tokens → Generate
PAT="oz_pat_..."

# 2. Store it as a Kubernetes secret
kubectl -n openzro create secret generic openzro-operator-mgmt \
  --from-literal=managementApiUrl=https://openzro.example.com \
  --from-literal=managementApiToken="$PAT"

# 3. Install the operator chart (currently 0.3.2-alpha.1 — pulls
#    ghcr.io/openzro/openzro-operator:0.3.2-alpha.1 multi-arch image)
helm install openzro-operator openzro/openzro-operator \
  --namespace openzro \
  --set managementApiSecret=openzro-operator-mgmt
```

Once the operator's reconciler is up, you can apply CRD instances:

```yaml
# my-group.yaml
apiVersion: openzro.io/v1
kind: OZGroup
metadata:
  name: backend-team
  namespace: openzro
spec:
  name: "Backend Team"
```

```bash
kubectl apply -f my-group.yaml
kubectl get ozgroups
# NAME            STATUS
# backend-team    Reconciled
```

The same pattern applies to `OZPolicy`, `OZSetupKey`, `OZResource`,
`OZRoutingPeer`. See the
[`openzro/openzro-operator`](https://github.com/openzro/openzro-operator)
repo's `examples/` directory for full manifests.

### CRD support tier — known limitations

At the time of this writing (2026-05-05):

- **Native CRDs** (`OZGroup`, `OZPolicy`, `OZSetupKey`, `OZRoutingPeer`)
  reconcile against the openZro management server end-to-end.
  `OZRoutingPeer` provisions the gateway pod, materializes the
  setup-key Secret, and runs the openZro client binary in `up`
  mode — see the operator's [`examples/`](https://github.com/openzro/openzro-operator/tree/main/examples).
- **`OZNetworkResource`** + **`OZHTTPRoute`** controllers (which
  provision DNS zones / records and reverse-proxy services) reach
  the management server but receive 404 because the **server-side
  handlers haven't shipped yet** — they're tracked under
  [ADR-0008 Stage 3](../adr/0008-kubernetes-helm-operator.md#stage-3--server-side-handlers-for-dns-zones--reverse-proxy-services).
  The CRDs themselves apply cleanly; reconciliation will start
  succeeding once that work lands.

## Upgrades

`helm upgrade` is the typical path. Two notes:

- **Dex storage**: by default the chart provisions a 1Gi PVC for
  Dex's sqlite. Migrating to postgres (recommended for HA) is a
  values flip — see the upstream [`dexidp/helm-charts`](https://github.com/dexidp/helm-charts)
  values for the postgres backend, then mirror that under
  `dex.config.storage` in your values. Plan a manual export/import
  of static passwords + connectors before flipping.
- **CRD upgrades**: helm doesn't upgrade CRDs by default. After
  every chart bump, re-apply the CRDs:
  ```bash
  helm pull openzro/openzro-operator --untar --untardir /tmp/op
  kubectl apply -f /tmp/op/openzro-operator/crds/
  ```

## Uninstall

```bash
# Operator first (releases CRD resources)
helm uninstall openzro-operator -n openzro

# CRDs survive helm uninstall — remove them explicitly to fully
# clean up
kubectl delete crd ozgroups.openzro.io ozpolicies.openzro.io \
                   ozresources.openzro.io ozroutingpeers.openzro.io \
                   ozsetupkeys.openzro.io

# Then the main chart
helm uninstall openzro -n openzro
kubectl delete namespace openzro
```

The Dex sqlite PVC is namespace-scoped and is deleted with the
namespace.

## Where to file issues

- **Helm chart bugs / values questions**: [openzro/helms](https://github.com/openzro/helms)
- **Operator bugs / CRD reconciler issues**: [openzro/openzro-operator](https://github.com/openzro/openzro-operator)
- **Management server / dashboard / Dex integration**: [openzro/openzro](https://github.com/openzro/openzro)
