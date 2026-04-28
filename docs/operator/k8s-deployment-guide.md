# Deploying openZro on Kubernetes

This guide walks an operator from a fresh Kubernetes cluster to a
working openZro deployment with the dashboard, management API, signal,
relay, embedded Dex IdP, and (optionally) the openZro
Kubernetes operator that reconciles peers / groups / policies / setup
keys / network resources from CRDs.

For the architectural decisions behind this layout, see
[ADR-0008](../adr/0008-kubernetes-helm-operator.md). For the IdP
choice (Dex vs external), see [ADR-0006](../adr/0006-embed-dex.md).

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
helm install --version 2.0.0-alpha.1 \
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

## Optional: install the operator

The operator reconciles openZro's domain objects (groups, policies,
peers, setup keys, network resources) from Kubernetes manifests so
GitOps + multi-tenant patterns work naturally.

```bash
# Issue a Personal Access Token in the dashboard:
#   Settings → Users → admin → Personal Access Tokens → Generate
PAT="oz_pat_..."

kubectl -n openzro create secret generic openzro-operator-mgmt \
  --from-literal=managementApiUrl=https://openzro.example.com \
  --from-literal=managementApiToken="$PAT"

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

At the time of this writing (2026-04-28):

- **Native CRDs** (OZGroup, OZPolicy, OZSetupKey, OZRoutingPeer)
  reconcile against the openZro management server end-to-end.
- **NetworkResource** + **HTTPRoute** controllers (which provision
  DNS zones / records and reverse-proxy services) reach the
  management server but receive 404 because the **server-side
  handlers haven't shipped yet** — they're tracked under
  [ADR-0008 Stage 3](../adr/0008-kubernetes-helm-operator.md#stage-3--server-side-handlers-for-dns-zones--reverse-proxy-services).
  The CRDs themselves apply cleanly; reconciliation will
  start succeeding once that work lands.

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
