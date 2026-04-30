# Lab validation — v0.53.1-alpha.16

> Fill in this checklist as you go. Save the result at the bottom of
> the file as `evidence` so the next operator can read it before the
> beta cut. Don't skip the `[ ]` checkboxes — a green pass requires
> all of them ticked. Anything red blocks the beta milestone.

**Tester:** _________________________
**Date:**   _________________________
**Lab:**    _________________________ (cluster + DNS domain)
**Helm chart version:** `2.0.0-alpha.9`
**App version:** `v0.53.1-alpha.16`

---

## Phase 1 — Wipe + clean redeploy

Goal: prove every artifact in alpha.16 deploys cleanly from scratch
on a non-pre-existing namespace + database.

```bash
# 1.1 Wipe namespace
kubectl delete ns openzro --grace-period=0 --force

# 1.2 Drop databases (adjust to your postgres host)
kubectl exec -it lab-postgres-0 -- psql -U postgres -c \
  "DROP DATABASE IF EXISTS openzro; \
   DROP DATABASE IF EXISTS dex; \
   CREATE DATABASE openzro; \
   CREATE DATABASE dex;"

# 1.3 Pull alpha.9 chart
helm repo add openzro https://charts.openzro.io
helm repo update
helm search repo openzro/openzro --versions | head -3

# 1.4 Generate dex gRPC certs (one-off, see helm chart README)
#     Or copy from previous lab if certs are still valid.

# 1.5 Edit lab-values.yaml, replace REPLACE_* placeholders.

# 1.6 Install
helm install openzro openzro/openzro \
  --version 2.0.0-alpha.9 \
  --namespace openzro --create-namespace \
  -f docs/operator/lab/lab-values.yaml

# 1.7 Wait + verify
kubectl get pods -n openzro -w
```

- [ ] All pods `Running` (`management`, `signal`, `relay`, `dashboard`, `dex`)
- [ ] `kubectl get certificate -n openzro` → ready=True (cert-manager issued)
- [ ] Dashboard at `https://openzro.lab.example.com/` renders
- [ ] Click "Login" → redirected to `/dex` → static-password login works → back to dashboard with peer list visible (empty)

**Anything red here:** stop, debug, redeploy. Don't proceed to phase 2 with a half-broken control plane.

---

## Phase 2 — Mesh básica (3 peers)

Goal: prove the install path (`pkg.openzro.io`) works end-to-end and
the default ALL-bidirectional policy lets peers talk.

| Peer | Platform | Install method |
|------|----------|----------------|
| `peerA` | Ubuntu 22.04 VM | `curl -fsSL https://pkg.openzro.io/install.sh \| sh` |
| `peerB` | Arch / CachyOS | pacman repo (see install.sh output) |
| `peerC` | Fedora 40 | dnf repo (see install.sh output) |

For each peer:

```bash
openzro version                            # expect: 0.53.1-alpha.16
openzro up --management-url https://openzro.lab.example.com:443
# browser opens → dex login → grant
openzro status                             # expect: Connected, peer ID, IP 100.x.y.z
```

Then exercise the mesh:

```bash
# from peerA
ping -c 3 <peerB-mesh-ip>
ping -c 3 <peerC-mesh-ip>
# from peerB
ping -c 3 <peerA-mesh-ip>
ping -c 3 <peerC-mesh-ip>
```

- [ ] All three peers register successfully (visible in dashboard's Peers tab)
- [ ] All three see each other in `openzro status --detail`
- [ ] Bidirectional pings succeed (default policy = allow ALL bidirectional)
- [ ] `pkg.openzro.io` served alpha.16 immediately (Cloudflare purge confirmed: see Phase 6)

---

## Phase 3 — Unidirectional `Protocol=ALL` (the new feature)

Goal: prove ADR-0010's lab plan — unidirectional ALL drops reverse
initiation while honoring stateful conntrack reply traffic.

### 3.1 Setup groups + policy

In dashboard:

1. **Groups** → create:
   - `lab-clients` → add `peerA`
   - `lab-servers` → add `peerB`, `peerC`
2. **Access Control** → create policy `unidirectional-test`:
   - Source group: `lab-clients`
   - Destination group: `lab-servers`
   - Protocol: `All`
   - Direction: click toggle until **single arrow** (out from clients to servers)
   - **Verify**: yellow callout appears warning about conntrack semantics
3. Disable the **Default** policy (toggle off) so only `unidirectional-test` applies.

### 3.2 Run a small HTTP server on peerB

```bash
# on peerB (destination)
python3 -m http.server 8080
```

### 3.3 Forward direction — should succeed

```bash
# from peerA (source) — TCP open
nc -zv <peerB-mesh-ip> 8080
# expect: succeeded

# from peerA — full HTTP (forward + conntracked reply)
curl -s -o /dev/null -w "%{http_code}\n" http://<peerB-mesh-ip>:8080/
# expect: 200

# from peerA — UDP echo
echo "ping" | nc -u -w 1 <peerB-mesh-ip> 9999
# expect: no error from nc itself
```

### 3.4 Reverse direction — should fail

```bash
# from peerB (destination, allowed only as response target)
nc -zv <peerA-mesh-ip> 22
# expect: timeout / connection refused

nc -zv <peerC-mesh-ip> 22
# expect: timeout (peerC also in destinations group, no peer-to-peer initiation)
```

### 3.5 Audit / flow exports

In dashboard's **Network Traffic** view (or via API):

```bash
# query last 5 minutes of dropped events
curl -s -H "Authorization: Bearer $TOKEN" \
  "https://openzro.lab.example.com/api/flow-events?event=drop&since=5m" \
  | jq '.[].direction'
```

- [ ] Forward (peerA → peerB:8080) `nc -zv` succeeds
- [ ] Forward `curl` returns 200 (proves conntrack reply path works)
- [ ] Reverse (peerB → peerA:22) `nc -zv` times out
- [ ] Reverse (peerC → peerA:22) also times out
- [ ] Flow export shows `drop, direction=reverse, rule=unidirectional-test` for the failed attempts
- [ ] Dashboard's Access Control table shows the policy with a **single arrow** icon (not the bidirectional badge)

**Repeat 3.3 + 3.4 with `iptables`-only client (peerA Ubuntu) and `uspfilter`-only client (peerC Fedora with `OZ_FIREWALL=uspfilter` env).**

---

## Phase 4 — Posture check (1 type)

Goal: prove a posture check actually gates peer admission.

In dashboard:

1. **Posture Checks** → create `os-min-linux-6-0`:
   - Type: OS version
   - Constraint: Linux kernel ≥ 6.0
2. Attach to a policy that requires it (or set as account-wide AdmissionPostureCheck).
3. Test:

```bash
# on a peer running kernel >= 6.0
openzro down && openzro up
# expect: succeeds, registers

# on a peer running kernel < 6.0 (use a deliberately old VM)
openzro down && openzro up
# expect: rejected with PermissionDenied + posture failure reason in management logs
```

- [ ] Compatible peer admits cleanly
- [ ] Incompatible peer rejected at gRPC Login boundary
- [ ] Audit log entry: `peer.admission.denied` with posture check name + value
- [ ] Dashboard's Audit log shows the rejection

---

## Phase 5 — HA failover

Goal: prove embedded NATS clustering keeps peers connected when a
management replica dies.

```bash
# 5.1 Scale to 2 replicas
helm upgrade openzro openzro/openzro \
  --version 2.0.0-alpha.9 \
  --namespace openzro \
  -f docs/operator/lab/lab-values.yaml \
  --set management.replicas=2

# 5.2 Wait for both pods Running, NATS gossip established
kubectl logs -n openzro -l app.kubernetes.io/name=openzro-management \
  --tail=100 | grep -i "nats.*joined cluster"

# 5.3 Identify the pod the peer is connected to
kubectl get pods -n openzro -l app.kubernetes.io/name=openzro-management

# 5.4 Kill it (replace ID)
kubectl delete pod openzro-management-<id> -n openzro --grace-period=0 --force

# 5.5 On peerA: watch openzro status
watch -n 1 'openzro status'
```

- [ ] Within 5s, `openzro status` reports Connected = true again
- [ ] Peer's IP doesn't change
- [ ] No alert/error in client logs other than the brief reconnect
- [ ] Audit log: `peer.reconnected` event with new replica's pod name

---

## Phase 6 — Fresh install via `pkg.openzro.io`

Goal: prove the public install pipeline (Cloudflare cache + sort -V
+ pacman/apt/yum repo metadata) actually serves alpha.16 to a brand
new operator.

On a **brand new VM** (no openzro touched, no cached repo metadata):

```bash
# Ubuntu 22.04
curl -fsSL https://pkg.openzro.io/install.sh | sh -x | tee /tmp/install.log

# Verify
openzro version
# expect: 0.53.1-alpha.16   (NOT alpha.15 or older)

grep -i "Got release tag" /tmp/install.log
# expect: v0.53.1-alpha.16
```

- [ ] `install.sh` resolves "latest" to `v0.53.1-alpha.16` (sort -V working)
- [ ] APT path: `apt list --upgradable | grep openzro` shows alpha.16 as available
- [ ] Pacman path: `pacman -Si openzro | grep Version` shows `0.53.1.alpha.16-1`
- [ ] YUM path: `dnf info openzro | grep Version` shows `0.53.1` with release suffix `alpha.16`
- [ ] Time from `git push --tags` → `pkg.openzro.io` serving alpha.16 was **≤2min** (Cloudflare purge fired correctly)

---

## Aggregate result

- **Total checkboxes:** 30
- **Passed:** ___ / 30
- **Failed:** ___ / 30
- **Recommend beta cut?** ☐ Yes / ☐ No

### Failures (with logs / screenshots / reproduction notes)

> Paste copy-paste-able reproduction for each failure. The next
> operator picking up the lab will thank you. If a failure is
> intermittent, note how often it reproduces and over what window.

```
[paste here]
```

### Sign-off

> Name and date once you're satisfied. Don't sign for someone else's
> phase — if Phase 4 was done by another operator, ask them to sign
> their own line.

| Phase | Signed by | Date |
|-------|-----------|------|
| 1     |           |      |
| 2     |           |      |
| 3     |           |      |
| 4     |           |      |
| 5     |           |      |
| 6     |           |      |

---

## Evidence (paste here once done)

```
[summary metrics, screenshots, logs — anything the beta cut decision
should hinge on. Keep it brief but complete enough that a reader
can audit your "yes" without re-running the lab.]
```
