<div align="center">
  <img width="120" src="brand/openzro-icon.svg" alt="openZro"/>
  <h1>open<span style="color:#7c3aed;font-weight:700">Z</span>ro</h1>
  <p>
    <strong>Open-source zero-trust mesh networking — BSD-3, no AGPL strings, no artificial limits.</strong>
  </p>

  <p>
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-BSD--3-7c3aed" alt="BSD-3-Clause"/></a>
    <a href="go.mod"><img src="https://img.shields.io/badge/go-1.23%2B-7c3aed" alt="Go 1.23+"/></a>
    <a href="docs/adr/0001-openzro-foundation.md"><img src="https://img.shields.io/badge/ADR-0001-7c3aed" alt="ADR-0001"/></a>
  </p>
</div>

---

openZro is a [WireGuard®](https://www.wireguard.com/)-based zero-trust overlay
network: every machine you put on it gets a flat, encrypted private LAN with
SSO, MFA, posture checks, and granular access policies — no port-forwarding,
no VPN gateways, no per-device manual config.

It is a fork of [`netbirdio/netbird@v0.52.2`](https://github.com/netbirdio/netbird/tree/v0.52.2)
([the last BSD-3 release](docs/FORK.md) before the upstream relicensed three
core components to AGPLv3 in August 2025), continued under BSD-3-Clause for
self-hosted deployments that want a freely-licensed, fully-featured baseline.

## Project status

**In production today.** openZro is already running real production
deployments — full control plane (management + signal + relay +
dashboard) on Kubernetes, with peers signing in through Dex and the
operator surface exercised daily.

**Still being built.** The codebase moves fast. Feature work and the
near-term plan live in [docs/ROADMAP.md](docs/ROADMAP.md) and the
[open issues](https://github.com/openzro/openzro/issues); chart
values and config formats may shift between releases until the
project stabilizes.

**Documentation is catching up.** A few pages still mirror the
upstream NetBird wording verbatim and are being refreshed as the
[docs parity audit](https://github.com/openzro/docs/issues/7) closes.
Where the docs and the code disagree, the code is authoritative.

**Try it and tell us what breaks.** Open an
[issue](https://github.com/openzro/openzro/issues/new/choose) or a
[discussion](https://github.com/openzro/openzro/discussions) — every
report from a real deployment helps shape what ships next.

## Why openZro and not upstream NetBird?

| | NetBird ≥ v0.53 | **openZro** |
|---|---|---|
| **License** of `management/`, `signal/`, `relay/` | AGPLv3 | **BSD-3-Clause** |
| Self-hosted with full features | Possible, but AGPL obligations attach to any modification served over a network | **No license obligations beyond BSD attribution** |
| Per-peer update buffer | Hardcoded at 100 (silent drops above that) | **Configurable, default 1000** ([commit](https://github.com/openzro/openzro/commit/17f40f94)) |
| Account fan-out concurrency | Hardcoded at 10 | **Configurable, default 64** ([commit](https://github.com/openzro/openzro/commit/092ddb6f)) |
| HA story | Sticky session required, no first-class cluster support | **First-class HA via Redis-compatible (Valkey/Redis/Dragonfly), external NATS, or embedded NATS** |
| Security advisory backports | N/A (you're on a current upstream version) | Tracked in [`docs/security/advisories.md`](docs/security/advisories.md), reimplemented clean-room |

The full reasoning is captured in [ADR-0001](docs/adr/0001-openzro-foundation.md).

## Architecture

```
┌──────────────┐         ┌─────────────────┐
│   client     │◄────────│ signal-server   │  WebRTC ICE candidate exchange
│ (WireGuard)  │         │ (HA-capable)    │
└──────┬───────┘         └─────────────────┘
       │
       ▼
┌──────────────┐         ┌─────────────────┐         ┌────────────────┐
│   client     │◄────────│ management      │◄───────►│ Postgres/MySQL │
│ (WireGuard)  │  gRPC   │ (HA-capable)    │   DB    │ (state of      │
└──────────────┘  Sync   └────────┬────────┘         │  truth)        │
                                  │                  └────────────────┘
                                  │ pub/sub + locks
                                  ▼
                          ┌───────────────┐
                          │ Valkey / NATS │  (only required for HA)
                          └───────────────┘
```

### HA modes (pick one — only required for ≥2 instances)

| Mode | What you run | When it fits |
|---|---|---|
| **None (single-instance)** | management + signal + Postgres/MySQL | Default. Works out of the box. |
| **Valkey** *(recommended)* | + Valkey 8 (or Redis 5+, or Dragonfly) | Same license family as openZro. |
| **NATS (external)** | + a NATS 2.7+ broker with JetStream | Already running NATS for other workloads. |
| **NATS (embedded)** | nothing extra; each openZro instance starts an in-process NATS server | Zero infra outside openZro itself. |

Activate by setting **one** of:

```bash
OPENZRO_REDIS_URL=valkey://broker:6379/0     # Valkey/Redis/Dragonfly
OPENZRO_NATS_URL=nats://broker:4222          # external NATS
OPENZRO_BROKER=embedded                      # embedded NATS
OPENZRO_CLUSTER_PEERS=nats://node2:6222,nats://node3:6222
```

The same broker selection drives both signal HA and management HA — one
piece of stateful infra, two components served. See
[ADR-0001 §3.4](docs/adr/0001-openzro-foundation.md#34-ha-architecture).

## Repository layout

```
openzro/
├── CLAUDE.md             Brand & engineering rules (read by Claude Code)
├── design-tokens.md      Colors / typography reference
├── brand/                Official brand assets (icon, etc.)
├── client/               WireGuard agent
├── management/           Control plane (gRPC + HTTP API)
├── signal/               WebRTC signaling
├── relay/                TURN-style relay
├── cluster/              Distributed coordinator (HA primitives)
├── dashboard/            Next.js web UI (with its own CLAUDE.md)
├── deploy/               Local docker-compose for dev/HA testing
└── docs/
    ├── FORK.md           Fork-point provenance
    ├── adr/              Architecture Decision Records
    └── security/         Security advisories tracking
```

## Install

### Client agent (Linux / macOS / Windows)

**Linux** — distro-detecting one-liner (covers Debian/Ubuntu/RHEL/Fedora/SUSE
via signed apt/yum/zypper repos, falls through to pacman/AUR for
Arch/CachyOS, binary tarball otherwise):

```bash
curl -fsSL https://pkg.openzro.io/install.sh | sh
```

Manual repo setup (apt example):

```bash
curl -sSL https://pkg.openzro.io/openzro-archive-key.asc | \
  sudo gpg --dearmor -o /usr/share/keyrings/openzro-archive-keyring.gpg
echo 'deb [signed-by=/usr/share/keyrings/openzro-archive-keyring.gpg] \
  https://pkg.openzro.io/apt stable main' | \
  sudo tee /etc/apt/sources.list.d/openzro.list
sudo apt-get update && sudo apt-get install openzro
```

**Windows** — `.msi` installer + `.zip` for portable use:

- Installer: [`openzro_<version>_windows_amd64.msi`](https://github.com/openzro/openzro/releases/latest)
- Tray UI: extract `openzro-ui_<version>_windows_amd64.zip`, run `openzro-ui.exe` as administrator

The MSI is currently unsigned (Windows shows a SmartScreen warning on first
run; click *More info → Run anyway*). EV signing via [SignPath
Foundation](https://signpath.org/foundation) is tracked as
[issue #1](https://github.com/openzro/openzro/issues/1).

**macOS** — universal `.pkg` installer or Homebrew tap:

```bash
# Homebrew (CLI)
brew install openzro/tap/openzro
sudo brew services start openzro

# Or .pkg installer from GH Releases
# https://github.com/openzro/openzro/releases/latest →
#   openzro_<version>_darwin_universal.pkg
```

The `.pkg` is unsigned; first run may need `xattr -d com.apple.quarantine`
or right-click → *Open*. Apple Developer ID notarization is tracked as
[issue #2](https://github.com/openzro/openzro/issues/2).

### Self-hosted control plane on Kubernetes

```bash
helm repo add openzro https://openzro.github.io/helms
helm repo update

# Control plane (management + signal + relay + dashboard + Dex)
helm install openzro openzro/openzro \
  --create-namespace -n openzro \
  -f my-values.yaml

# Optional: K8s operator (CRDs that reconcile peers/groups/policies)
helm install openzro-operator openzro/openzro-operator -n openzro
```

See [`docs/operator/k8s-deployment-guide.md`](docs/operator/k8s-deployment-guide.md)
for the full walk-through (values overrides, Gateway API instead of Ingress,
operator Personal Access Token wiring, troubleshooting).

### Self-hosted control plane on a single VM

`infrastructure_files/configure.sh` generates the docker-compose stack
(management + signal + relay + dashboard + Dex + mTLS PKI). See
[ADR-0006](docs/adr/0006-embed-dex.md) for the IdP architecture.

## Quick start (development)

```bash
# 1. Bring up Postgres + Valkey + NATS locally
make dev.deps.up

# 2. Build the Go core
make build

# 3. Run tests
make test
```

Single-instance dev:

```bash
export OPENZRO_STORE_ENGINE=postgres
export OPENZRO_STORE_ENGINE_POSTGRES_DSN=postgres://openzro:openzro@localhost:5432/openzro?sslmode=disable
./management/management management --datadir=/tmp/openzro
```

HA dev (one of):

```bash
# Valkey
export OPENZRO_REDIS_URL=valkey://localhost:6379/0

# external NATS
export OPENZRO_NATS_URL=nats://localhost:4222

# embedded NATS (no broker container needed)
export OPENZRO_BROKER=embedded
export OPENZRO_CLUSTER_PEERS=nats://localhost:6222
```

`make help` lists every available target.

## Documentation

| Document | What's there |
|---|---|
| [docs/adr/0001-openzro-foundation.md](docs/adr/0001-openzro-foundation.md) | Why this fork exists, license posture, HA architecture |
| [docs/adr/0006-embed-dex.md](docs/adr/0006-embed-dex.md) | Embedded Dex IdP — federation via gRPC API |
| [docs/adr/0007-client-packaging.md](docs/adr/0007-client-packaging.md) | MSI / PKG / Homebrew / Linux packages strategy + roadmap |
| [docs/adr/0008-kubernetes-helm-operator.md](docs/adr/0008-kubernetes-helm-operator.md) | Helm chart + Kubernetes operator architecture |
| [docs/operator/k8s-deployment-guide.md](docs/operator/k8s-deployment-guide.md) | Hands-on guide for K8s self-hosting (helm + operator + CRDs) |
| [docs/FORK.md](docs/FORK.md) | Exact fork point and license boundary |
| [docs/ROADMAP.md](docs/ROADMAP.md) | Prioritized roadmap (security backports, posture providers, …) |
| [docs/security/advisories.md](docs/security/advisories.md) | Triage record of every CVE/GHSA we've evaluated |
| [CLAUDE.md](CLAUDE.md) | Brand + engineering rules (read by AI assistants) |
| [dashboard/CLAUDE.md](dashboard/CLAUDE.md) | Frontend-specific engineering rules |

Sibling repos:

| Repo | What's there |
|---|---|
| [`openzro/helms`](https://github.com/openzro/helms) | Helm charts (`openzro` control plane, `openzro-operator`, `openzro-operator-config`) |
| [`openzro/openzro-operator`](https://github.com/openzro/openzro-operator) | Kubernetes operator — CRDs for peers/groups/policies/setup-keys/network-resources |
| [`openzro/homebrew-tap`](https://github.com/openzro/homebrew-tap) | Homebrew formula for macOS (auto-published from this repo on tag) |

## Contributing

1. **No CLA.** openZro accepts contributions under the inbound-equals-outbound
   BSD-3 rule. By submitting a PR you agree it will be released as BSD-3.
2. **No AGPL ingestion ever.** Do not paste, mirror, or translate code from
   `netbirdio/netbird` post-`v0.53.0` (the AGPLv3 cut). Reimplementation from
   public CVE/CWE/protocol descriptions is fine and is how we backport security
   fixes — see the existing examples in [`docs/security/advisories.md`](docs/security/advisories.md).
3. **TDD is the default.** New code lands with tests written first. See
   [CLAUDE.md](CLAUDE.md) §Engineering rules.

## Upstream credit

openZro inherits and credits prior work from [`netbirdio/netbird`](https://github.com/netbirdio/netbird)
through `v0.52.2` (BSD-3-Clause). The upstream `LICENSE` and `AUTHORS`
files are preserved verbatim under the BSD-3 attribution clause. New
contributors to openZro itself are added to `AUTHORS` separately.

WireGuard® and the WireGuard logo are
[registered trademarks](https://www.wireguard.com/trademark-policy/) of
Jason A. Donenfeld.

## License

[BSD 3-Clause](LICENSE) — forever, in every directory.
