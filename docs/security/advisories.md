# Security advisories — tracking

This file tracks every public security advisory that may affect openzro, along with our assessment and the action taken. Advisories are reimplemented clean-room per the policy in [ADR-0001](../adr/0001-openzro-foundation.md#33-clean-room-reimplementation-policy): we read public advisory text only, never upstream patch code.

Entries are append-only; status changes are recorded as new lines or edits with the date.

| ID | Severity | Component | Status | Resolved in commit | Notes |
|---|---|---|---|---|---|
| [CVE-2025-10678](https://cert.pl/en/posts/2025/10/CVE-2025-10678/) | High | install script (ZITADEL default admin) | **Fixed** | [`0f956e72`](https://github.com/openzro/openzro/commit/0f956e72) | Hijacks the ZITADEL `zitadel-admin/Password1!` default with a randomized `ZITADEL_FIRSTINSTANCE_ORG_HUMAN_PASSWORD`. |
| Mgmt API Authorization Bypass ([forum post](https://forum.netbird.io/t/netbird-management-api-authorization-bypass/521)) | High | management auth middleware (CWE-639) | **Fixed** | [`3196cbbf`](https://github.com/openzro/openzro/commit/3196cbbf) | Removed the `?account=<id>` query-string override that let any authenticated user spoof `userAuth.AccountId` and set `IsChild=true` (the latter bypasses admin checks in `peers_handler.go` and `user.go`). |
| [GHSA-rxmp-8h9v-56cx](https://github.com/netbirdio/netbird/security/advisories/GHSA-rxmp-8h9v-56cx) | Moderate | management `SaveOrAddUsers` (CWE-362, race) | **Fixed** | [`c761e80f`](https://github.com/openzro/openzro/commit/c761e80f) | Re-fetch initiator inside the transaction with `LockingStrengthUpdate` and re-validate admin power before iterating updates — closes the TOCTOU between `ValidateUserPermissions` and the in-transaction role read. |
| [CVE-2025-55182](https://nvd.nist.gov/vuln/detail/CVE-2025-55182) / [CVE-2025-66478](https://nextjs.org/blog/CVE-2025-66478) | Critical (CVSS 10.0) | dashboard, React Server Components RCE | **Not applicable** | — | Vercel advisory (CVE-2025-66478) explicitly states: *"Next.js 13.x, Next.js 14.x stable, Pages Router applications, and the Edge Runtime are not affected."* Our `dashboard/package.json` pins `next: ^14.2.28` stable. We must re-evaluate if/when we upgrade to Next.js 15.x or 16.x. |
| [GHSA-q4gf-8mx6-v5v3](https://github.com/advisories/GHSA-q4gf-8mx6-v5v3) | High | dashboard, Next.js App Router DoS via Server Components | **Open — bump pending** | — | Affects every Next.js 13.x–15.5.14 and 16.x–16.2.2 using the App Router. Our 14.2.28 is in range. No patch in the 14.x line — fix requires Next 15.5.15 or 16.2.3+. App Router is what `dashboard/src/app/` uses, so we are exposed. Tracked: bump to Next 15.5.15 (smallest jump that closes the advisory) in a dedicated PR with React 18 → 19 review and Cypress regression. |
| [GHSA-h25m-26qc-wcjf](https://github.com/advisories/GHSA-h25m-26qc-wcjf) | High | dashboard, Next.js insecure deserialization in Server Components | **Open — bump pending** | — | Same exposure profile as q4gf-8mx6-v5v3. Patches: 15.0.8 / 15.1.12 / 15.2.9 / 15.3.9 / 15.4.11 / 15.5.10 / 16.0.11 / 16.1.5 — all in the 15.x+ line. Will close with the same Next bump. |
| [GHSA-9g9p-9gw9-jx7f](https://github.com/advisories/GHSA-9g9p-9gw9-jx7f) | Moderate | dashboard, next/image remotePatterns DoS | **Open — bump pending** | — | Closes with the same Next bump. We do not currently configure `images.remotePatterns` in production, so the practical exposure is limited to dev. |
| [GHSA-ggv3-7p47-pfv8](https://github.com/advisories/GHSA-ggv3-7p47-pfv8) | Moderate | dashboard, Next.js HTTP request smuggling in rewrites | **Open — bump pending** | — | Closes with the same Next bump. Our `next.config.js` does not declare custom `rewrites` — the practical exposure is bounded. |
| [GHSA-3x4c-7xq6-9pq8](https://github.com/advisories/GHSA-3x4c-7xq6-9pq8) | Moderate | dashboard, next/image disk cache growth | **Open — bump pending** | — | Closes with the same Next bump. |
| [GO-2025-3553](https://pkg.go.dev/vuln/GO-2025-3553) | High | management JWT auth + IdP zitadel/auth0 paths | **Fixed** | [`34c87a33`](https://github.com/openzro/openzro/commit/34c87a33) | golang-jwt v3 has no upstream patch. Migrated to v5 across 14 files (auth, idp, http test tools); aud/iss verification moved from `MapClaims.VerifyAudience/VerifyIssuer` (removed) to `jwt.WithAudience/WithIssuer` parser options. `WithIssuedAt()` re-introduces the v3 default of rejecting iat-in-the-future tokens. |
| GO-2026-4762 / GO-2025-4017 / GO-2026-4394 / GO-2026-4815 / GO-2025-{3528,4100,4108} | Mixed | grpc, quic-go, otel/sdk, x/image, containerd | **Fixed** | [`e4ba4a50`](https://github.com/openzro/openzro/commit/e4ba4a50) | Bulk dependency bumps (Wave 1) — no API migration required. govulncheck went from 11 → 4 reachable. |
| GO-2026-4887 / GO-2026-4883 / GO-2026-4479 | Mixed | docker/docker, pion/dtls/v2 | **Open — upstream blocked** | — | No fixed version published by upstream. Re-check on the next dependency review window. |

## Process

For each new advisory:

1. **Triage** — read the public advisory text (CVE / GHSA / CWE entry / vendor blog post). Do **not** open the upstream patch diff or PR if the upstream code is now AGPL.
2. **Map to our code** — locate the function, file, or behavior described, in our v0.52.2-derived tree. Line numbers from the upstream advisory will not match — go by function names and CWE patterns.
3. **Assess applicability** — confirm the vulnerable code path actually exists at the openzro fork point. Some advisories may target features added after v0.52.2 / v2.15.0 (not applicable to us); others may target behaviors we inherit (applicable).
4. **Fix or document** — if applicable, write an original, clean-room implementation of the fix from the description. If not applicable, record *why* in this table so future contributors don't re-litigate the question.
5. **Update this file and commit** — every status change is its own commit, referenced by SHA in the table above.

## What "clean-room" means here

Before writing a security fix, the contributor confirms (in the commit message) that they did **not** read:

- the upstream patch commit, PR, or diff (any branch where the affected files are now AGPL — i.e., `netbirdio/netbird` post-`v0.53.0`)
- mirrored / cached copies of the same diff (security trackers sometimes embed snippets)
- third-party reverse-engineering write-ups that quote significant chunks of the patch

The contributor **may** read:

- the CVE / GHSA / CWE prose
- the upstream advisory page (text only)
- public blog posts and write-ups that describe the *vulnerability conceptually* without quoting patch code
- documentation of unrelated upstream projects (ZITADEL, Postgres, Redis, etc.) that the fix may need to interact with

When in doubt, write the fix from a description, not from code.
