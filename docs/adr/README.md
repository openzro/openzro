# Architecture Decision Records

This directory contains the ADRs for openzro. Each ADR captures a single decision, the context that drove it, and the consequences we accept by making it. ADRs are append-only — when a decision is reversed, a new ADR supersedes the old one (the old one stays, marked `Superseded by`).

## Keep each ADR self-contained

An ADR must stand on its own when read years later. When the problem
statement, discussion, or scope sign-off lives in a GitHub issue or PR,
summarize the parts the decision depends on inline — the problem, the
constraints, and the scope that was agreed — in a few sentences. The link
stays, as provenance, not as a substitute for the summary.

Issues are mutable (edited, re-scoped, buried under later comments) and
live outside the repository; the ADR is the record. A reader should be able
to understand *what* was decided and *why* without opening the issue.

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
| [0010](./0010-unidirectional-all-policies.md) | Unidirectional `Protocol=ALL` access policies | Accepted |
| [0011](./0011-management-shared-wg-identity.md) | Persistent + shared WireGuard identity for the management daemon | Accepted |
| [0012](./0012-flow-archive-read-path.md) | Cold-archive read path for flow events (Parquet + DuckDB) | Proposed |
| [0013](./0013-flow-policy-correlation.md) | Flow event policy correlation via ctmark on Linux | Accepted |
| [0014](./0014-coordinated-multi-pod-relay.md) | Coordinated multi-pod relay for K8s deployments | Accepted |
| [0015](./0015-hostname-network-resources-and-acl.md) | Hostname Network Resources + zero-trust ACL onboarding | Accepted |
| [0016](./0016-dashboard-redesign-v2.md) | Dashboard redesign v2 (incremental) | Accepted (planning) |
| [0017](./0017-control-center-access-graph.md) | Control Center: topological access-graph view | Proposed |
| [0018](./0018-server-side-flow-policy-resolver.md) | Server-side flow event policy resolver | Proposed |
| [0019](./0019-schedule-posture-check.md) | Time-based posture check (`ScheduleCheck`) | Accepted |
| [0020](./0020-openzro-ssh-identity-protocol.md) | openZro SSH: identity-based access via a dedicated policy protocol | Proposed |
| [0021](./0021-policy-propagation-consistency-model.md) | Policy-change peer propagation: consistency model | Proposed |
| [0022](./0022-custom-dns-zones.md) | Custom DNS Zones: private authoritative zones with group-based distribution | Proposed |
| [0023](./0023-policy-based-multi-resolver-dns.md) | Policy-based DNS response selection for multi-resolver zones | Proposed |
| [0024](./0024-scim-provisioned-local-users.md) | SCIM-provisioned local users (password credentials in embedded Dex) | Accepted |
| [0025](./0025-allowed-email-domains.md) | Account-level allowlist of email domains for new users | Accepted |
| [0026](./0026-console-local-users-and-email-domains.md) | Console management of local users and the email-domain allowlist | Proposed |
