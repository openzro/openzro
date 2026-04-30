# Architecture Decision Records

This directory contains the ADRs for openzro. Each ADR captures a single decision, the context that drove it, and the consequences we accept by making it. ADRs are append-only — when a decision is reversed, a new ADR supersedes the old one (the old one stays, marked `Superseded by`).

| #     | Title                                       | Status     |
|-------|---------------------------------------------|------------|
| [0001](./0001-openzro-foundation.md) | openzro foundation (fork rationale, license posture, technical strategy) | Accepted |
| [0002](./0002-flow-events-storage.md) | Flow events storage architecture | Accepted |
| [0003](./0003-device-admission-gate.md) | Device admission gate (control-plane refusal of non-compliant peers) | Accepted (§Consequences "no per-peer overrides" superseded by 0004) |
| [0004](./0004-admission-bypass-and-group-scope.md) | Admission bypass + group-scope exemption | Accepted |
| [0005](./0005-centralized-login.md) | openZro-branded centralized login (broker-based) | Superseded by 0006 |
| [0006](./0006-embed-dex.md) | Embed Dex as openZro's federated IdP | Accepted (course-corrected 2026-04-28) |
| [0007](./0007-client-packaging.md) | Client packaging — native installers, signing, package matrix | Proposed |
| [0008](./0008-kubernetes-helm-operator.md) | Kubernetes deployment — Helm chart + Operator | Proposed |
| [0009](./0009-bare-metal-ansible-and-ha.md) | Bare-metal Ansible flow + HA via embedded NATS + Dex single-ingress | Accepted |
