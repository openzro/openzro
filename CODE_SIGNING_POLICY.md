# Code signing policy

Free code signing for Windows is provided by [SignPath.io](https://about.signpath.io),
with the certificate issued by the [SignPath Foundation](https://signpath.org).

> **Status:** the application to the SignPath Foundation is in progress
> ([#1](https://github.com/openzro/openzro/issues/1)). Until it is approved,
> the Windows artifacts on the
> [releases page](https://github.com/openzro/openzro/releases) are unsigned.
> This page describes the policy the signed releases follow.

## What is signed

Only binaries built from this repository's source code, by the
[`release-binaries.yml`](.github/workflows/release-binaries.yml) GitHub Actions
workflow, on a pushed release tag:

| Artifact | Contents |
| --- | --- |
| `openzro.exe` | CLI and background service (daemon) |
| `openzro-ui.exe` | Desktop tray application |
| `openzro_<version>_windows_amd64.msi` | Installer that ships the two binaries above |

Third-party binaries bundled in the installer keep their own vendor
signature and are not re-signed with the openZro certificate. Today that is
`wintun.dll`, downloaded from [wintun.net](https://www.wintun.net) and
checked against a pinned SHA-256 in the workflow.

No binary is signed from a developer's machine. Every signing request comes
from the release workflow, and each one has to be approved by hand.

## Upstream code

openZro is an independent fork of
[`netbirdio/netbird@v0.52.2`](https://github.com/netbirdio/netbird/tree/v0.52.2),
the last release under BSD-3-Clause (see [docs/FORK.md](docs/FORK.md)).
The openZro team maintains all of the source code and build scripts in this
repository. We do not sign NetBird's binaries or builds of the upstream
repository; we sign only the openZro binaries built by our own workflow.

## Team roles

| Role | Members | Responsibility |
| --- | --- | --- |
| Committers and reviewers | [Maintainers](MAINTAINERS.md) | Write and review the code that goes into `main` |
| Approvers | [Lead maintainer](MAINTAINERS.md) ([@klinux](https://github.com/klinux)) | Approve each signing request in SignPath for a release |

Outside contributions come in through pull requests, and a maintainer reviews
them before they are merged. Everyone with write access to the repository, and
everyone who can approve a signing request, must have multi-factor
authentication enabled. The `openzro` GitHub organization requires 2FA for all
of its members.

## Privacy

See the [privacy policy](PRIVACY.md). In short, the signed programs do not send
information to any system other than the ones the person installing or
operating them configures, unless a user explicitly asks them to.

## Uninstalling

The MSI registers with *Settings → Apps → Installed apps* (and *Programs and
Features*). Removing it there stops and removes the openZro service and
deletes the installed files.

## Reporting a problem

To report a signed binary you believe was not built from this repository, or
that behaves maliciously, follow [SECURITY.md](SECURITY.md).
