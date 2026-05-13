# openZro Governance

This document describes how decisions get made in the openZro project.

## Project status

openZro is an **independent, community-driven open-source project**.
It is not the product of any single company. The project welcomes
contributions from individuals and organizations of any size, and
treats contributors equally regardless of their employer.

The project is currently in its **early growth phase** — the
governance model below is intentionally light to keep iteration fast.
As the contributor base grows, the maintainer team will adopt a more
formal Technical Steering Committee (TSC) structure modelled on
[CNCF Sandbox-level projects](https://github.com/cncf/foundation/blob/main/governance-charters.md).
The transition will happen via a GOVERNANCE-CHANGE proposal (see
"Changing this document" below).

## Roles

### Contributor
Anyone who submits a PR, files an issue, helps in discussions, or
writes documentation. No formal status, no membership process.

### Maintainer
A contributor with **write access** to one or more openZro repositories.
Maintainers are listed in [MAINTAINERS.md](MAINTAINERS.md) with their
respective areas of expertise. Maintainers:

- Review and merge PRs in their area.
- Triage issues and assign labels.
- Cut releases (run the release-binaries workflow).
- Decide on roadmap items in their area via lazy consensus.

A maintainer can step back at any time by opening a PR removing
themselves from MAINTAINERS.md. Maintainers who are inactive for 6+
months without notice may be moved to "emeritus" status by majority
vote of the remaining maintainers.

### Lead maintainer
A single maintainer designated as the project's primary point of
contact for legal/IP/governance matters. Same code-review rights as
any maintainer; the additional responsibility is administrative
(trademark, foundation discussions, security advisories coordination).
The current lead maintainer is in [MAINTAINERS.md](MAINTAINERS.md).

## Becoming a maintainer

New maintainers are added by **majority vote of existing maintainers**.
The path:

1. Sustained, high-quality contribution over a 3-6 month window —
   meaningful PRs merged, code reviews provided, issues triaged.
2. An existing maintainer opens a PR adding the candidate to
   [MAINTAINERS.md](MAINTAINERS.md) with a short justification.
3. Other maintainers approve / object on the PR. Lazy consensus —
   if no maintainer objects within 7 calendar days, the PR merges.
4. If anyone objects, the PR moves to formal vote: majority of
   maintainers (excluding the candidate) approves.

Maintainer additions actively prioritize contributors from
**different employers / no employer** to keep the project vendor-neutral.

## Decision making

### Lazy consensus
Day-to-day technical decisions are made by **lazy consensus**: a
maintainer proposes a change (PR, issue comment, RFC), and if no
other maintainer objects within a reasonable window (24-72 hours for
small changes, 7 days for non-trivial), the decision stands.

### RFCs
Non-trivial changes — new features, API changes, new dependencies,
architectural shifts — require a written RFC. RFCs live under
[`docs/adr/`](docs/adr/) using the existing Architecture Decision
Record format. An RFC needs at least **one supporting maintainer** and
no sustained objection from another maintainer to merge.

### Disputes
If maintainers cannot reach consensus via discussion, any maintainer
can call for a formal vote. Voting:

- Each active maintainer gets one vote.
- Majority wins. Ties break in favour of the status quo (no change).
- Vote can be on GitHub (reactions / comments) or in a dedicated issue.
- Voting window is 7 days minimum.

The lead maintainer does NOT have a tie-breaking vote — they vote
like any other maintainer.

## Code of conduct enforcement

The [Code of Conduct](CODE_OF_CONDUCT.md) applies to all project
spaces. Enforcement is handled by the active maintainer team. Reports
go to `conduct@openzro.io` (forwards to all maintainers). Sanctions
range from private warning → public warning → temporary ban → permanent
ban, decided by maintainer majority vote on the basis of severity and
prior incidents.

## Security advisories

Vulnerability reports go through GitHub's private security advisory
flow described in [SECURITY.md](SECURITY.md). Coordination is led by
the lead maintainer, with the relevant area maintainer(s) drafting the
fix. Public disclosure follows the 90-day default of the CVE
coordination policy unless a critical exploit warrants a shorter
window.

## Releases

Anyone can propose a release by opening an issue. Maintainers cut the
release by pushing a `vX.Y.Z` tag on `main` — the release-binaries.yml
workflow does the rest (containers, binaries, signed macOS PKG, MSI).

Release cadence is opportunistic, not calendar-driven. Alpha tags are
cut whenever there's a meaningful change to validate; stable tags
require explicit RFC approval (no stable tags exist yet — the project
is alpha-track).

## Trademark and IP

The openZro name and logo are governed by [TRADEMARK.md](TRADEMARK.md).
The code is licensed under BSD-3-Clause; that license does NOT include
trademark rights. The lead maintainer is the trustee of the trademark
on behalf of the project until the project joins a foundation, at
which point the trademark transfers to that foundation.

Contributors retain copyright on their contributions and license them
under BSD-3-Clause via the [DCO sign-off](CONTRIBUTING.md#developer-certificate-of-origin-dco)
required on every commit.

## Changing this document

Governance changes are themselves RFCs. Open a PR modifying
GOVERNANCE.md with a paragraph in the PR description explaining the
motivation. Requires:

- 7-day minimum discussion window on the PR
- Approval from a majority of maintainers
- No sustained objection from any maintainer (objections must propose
  an alternative; raw "I don't like it" doesn't block)

## Future state

When the contributor base reaches 3+ active maintainers from 2+
distinct employers (or no employer) and 3+ public adopters, the
project will apply for [CNCF Sandbox](https://github.com/cncf/sandbox)
status. At that point, this document will be replaced by a
Sandbox-compliant governance charter.

---

_This document is alive — open a PR to suggest changes._
