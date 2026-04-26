# Security Policy

openZro's goal is to provide a secure network. If you find a security
vulnerability, please report it privately so we can fix it before the
details are public.

There is no official bug bounty program for the openZro project.

## Supported Versions

We currently support only the latest released version. Backports for
critical CVEs are evaluated on a case-by-case basis and tracked in
[`docs/security/advisories.md`](docs/security/advisories.md).

## Reporting a Vulnerability

Use **GitHub's private vulnerability reporting** — open the
[Security advisories page](https://github.com/openzro/openzro/security/advisories/new)
on this repository and click **Report a vulnerability**. Only project
maintainers will see the report; details stay private until a fix is
ready.

This is the only supported channel today. Please do **not** open a
public GitHub issue, post on Discussions, or share details on social
media before the advisory is published — coordinated disclosure
protects every operator running openZro.

When you submit, please include:

- A clear description of the vulnerability and the impact you observed.
- Steps to reproduce, ideally with a minimal proof of concept.
- The affected version (commit SHA or tag) and platform.
- Whether you would like to be credited in the advisory.

We aim to acknowledge new reports within three business days and to
provide a remediation plan within ten business days. Critical
vulnerabilities (RCE, auth bypass, key disclosure) are prioritised
ahead of everything else.

## Non-security bugs

For functional bugs that are not security-sensitive, please use the
public [bug report template](https://github.com/openzro/openzro/issues/new?assignees=&labels=&template=bug-issue-report.md&title=).
