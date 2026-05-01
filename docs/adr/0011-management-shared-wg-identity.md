# ADR-0011: Persistent + shared WireGuard identity for the management daemon

**Status:** Accepted — 2026-05-01
**Deciders:** openZro maintainers
**Supersedes / amended by:** —

## Context

Every peer's `Login` and `Sync` request to the management daemon is wrapped
in an envelope encrypted with the daemon's WireGuard public key (see
[`management/server/grpcserver.go::parseRequest`][parseRequest] and
[`encryption/message.go`][encrypt]). The daemon decrypts with its own
WireGuard private key.

Upstream NetBird (and openZro before this ADR) generated that key
in-process on every `NewServer()` call:

```go
// management/server/grpcserver.go (pre-ADR)
key, err := wgtypes.GeneratePrivateKey()
```

There is **no config knob, no env override, no persistence**. Every pod
restart rotates the key; every horizontal-scaled replica generates an
independent key.

For single-replica deployments this only causes a brief ripple on
restart — peers reconnect, fetch the new public key from `GetServerKey`,
and proceed. **For multi-replica HA it is fatal**: the K8s Service
round-robins requests across pods, so a peer that encrypted with pod A's
public key sees ~50 % of its requests land on pod B and fail with
`InvalidArgument: invalid request message`. We hit this directly during
the v0.53.1-alpha.22 lab smoke test of the new `cluster.mode=external`
profile (chart 2.1.0-alpha.2):

```text
2026-05-01T13:55:58Z WARN encryption/message.go:30: error while decrypting
  Sync request message from peer j+jQ6IFGvXZtS/cat9tK+sZIK7MkIdD09qa4Uo184yA=
2026-05-01T13:57:11Z WARN encryption/message.go:30: error while decrypting
  Sync request message from peer j+jQ6IFGvXZtS/cat9tK+sZIK7MkIdD09qa4Uo184yA=
[…]
```

NetBird's managed cloud is presumed to run either single-replica
management per region/account or carry an internal patch that has never
been upstreamed. Issue [netbirdio/netbird#3547][netbird-3547] tracking
"management HA" has been open without resolution since 2024. openZro's
fork posture (BSD-3, ADR-0001 §3.1) is to do the work clean-room and
share it — leaving operators with the same single-replica corner the
upstream is in defeats the point of the fork.

## Decision

The management daemon's WireGuard identity becomes a **first-class
config field** with three resolution paths, in priority order:

1. **`OPENZRO_MGMT_WG_PRIVATE_KEY` environment variable**
   — operator override. The chart populates it from a Kubernetes Secret
   so every replica reads the same value.

2. **`WgPrivateKey` field in `management.json`**
   — base64-encoded `wgtypes.Key`. Persisted across restarts of the
   same pod; mirrored across nodes when operators ship the same
   `management.json` to multiple instances (bare-metal HA pattern in
   ADR-0009).

3. **Generate fresh + persist** to the config file
   — back-compat for single-instance deployments. The first boot
   generates a key, writes it back into `management.json`, and logs a
   warning telling the operator to persist or inject before scaling.

`NewServer()` calls a new `resolveManagementWgKey()` that walks the
priority list, parses, and returns the key. Single-instance behaviour
is preserved — operators who never touch the env var or config file get
the same on-disk-persisted key as the legacy generate-on-boot path,
just stable across restarts.

The chart (helms 2.1.0-alpha.4+) ships:

- A pre-install Helm hook job that generates a fresh WG key with
  `wg genkey` if the target Secret does not yet exist (idempotent —
  reuses on upgrades).
- A `<release>-management-identity` Secret with `wgPrivateKey` data.
- An env-var ref in the management Deployment / StatefulSet pointing
  at the Secret.

## Consequences

**Positive**

- HA deployments work out of the box: `helm install` produces N
  replicas that share one identity, peers see consistent public keys
  regardless of which pod handles the request.
- Single-instance deployments survive pod restarts without forcing
  every peer to refresh `GetServerKey` on every reboot.
- The mechanism is operator-explicit: an `OPENZRO_MGMT_WG_PRIVATE_KEY`
  in the env list is more discoverable than a generate-and-pray default,
  and operators auditing for HA-readiness can grep for it.

**Negative**

- New surface area in `management.json` and the chart that operators
  must understand at upgrade time. Mitigated by the persist-on-generate
  path — existing single-instance installs migrate transparently on the
  first boot post-upgrade.
- The shared Secret is sensitive material — losing it forces every
  peer in the deployment to re-register (their cached server public
  key is invalidated). Operators must back it up alongside Postgres.
  Documented in the chart README + ADR-0009 (bare-metal HA).
- We diverge from upstream `cmd.management.go` boot wiring. The
  divergence is small (one helper) and clearly attributed in
  `grpcserver.go` comments.

## Alternatives considered

- **Session affinity on the K8s Service** (`sessionAffinity: ClientIP`).
  Single-client-per-pod works but multi-client cross-pod still breaks,
  rolling upgrade still loses state, and bare-metal operators get no
  benefit (no Service in front). Rejected as a half-fix.

- **Active-passive with leader election** (Kubernetes lease). Failover
  would be clean but loses horizontal scale. Significant complexity for
  no upside over the shared-identity approach. Rejected.

- **Operator-injected Secret only, no generate-on-empty fallback**.
  Cleaner code path but breaks every existing single-instance
  deployment on upgrade. Rejected — back-compat is a hard requirement
  per ADR-0001.

- **In-binary leader election picking one pod's key**. Would push key
  rotation to a runtime decision, requires distributed coordination on
  every boot, and creates a window where peers can hit a pod that
  doesn't yet know the elected key. Rejected — ahead-of-time shared
  Secret is simpler and stricter.

## References

- [NetBird issue #3547 — management HA support][netbird-3547]
- [`management/server/grpcserver.go`][grpcserver] — `NewServer` +
  `parseRequest` + `resolveManagementWgKey`
- [`management/server/types/config.go`][config] — `WgPrivateKey` field
- [Chart `helms/charts/openzro` 2.1.0-alpha.4+][chart] — Secret + hook
- [ADR-0001][adr-1] §3.1 (license posture, fork patches stay clean-room)
- [ADR-0009][adr-9] (bare-metal HA)

[parseRequest]: ../../management/server/grpcserver.go
[encrypt]: ../../encryption/message.go
[grpcserver]: ../../management/server/grpcserver.go
[config]: ../../management/server/types/config.go
[chart]: https://github.com/openzro/helms/tree/main/charts/openzro
[adr-1]: 0001-openzro-foundation.md
[adr-9]: 0009-bare-metal-ansible-and-ha.md
[netbird-3547]: https://github.com/netbirdio/netbird/issues/3547
