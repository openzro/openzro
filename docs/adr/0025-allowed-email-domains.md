# ADR-0025 — Account-level allowlist of email domains for new users

## Status

**Proposed**. Design gate for
[openzro/openzro#207](https://github.com/openzro/openzro/issues/207).
Companion to [ADR-0024](./0024-scim-provisioned-local-users.md)
(SCIM-provisioned local users), but independent: either can ship without the
other.

## Context

Self-hosted openZro runs in single-account mode: every identity the IdP
authenticates is mapped onto the one account
(`GetAccountIDFromUserAuth`, `management/server/account.go`, overrides the
domain claims with `singleAccountModeDomain`). On first login,
`addNewUserToDomainAccount` creates a `types.User` with role `user` and no
groups. There is **no openZro-side control over who may join**. Today the
only gate is whatever the IdP or Dex connector enforces:

- Dex `google` has `hostedDomains`, and `microsoft` has `tenant` / `groups`.
- `github`, generic `oidc`, and many other connectors either have no domain
  filter or depend on correct configuration in a system the openZro operator
  may not own.
- `JWTAllowGroups` (`types.Settings`) gates on a groups claim. It does
  nothing for IdPs or connectors that don't emit groups, including the Dex
  local password connector.

A new user with no groups is harmless only while no policy matches the
`All` group. That is a property of the policy set, not a guarantee. Operators
asked for a declarative "only `@example.com` may join" control that does not
depend on per-connector configuration.

**Verified current behavior** (openZro tree at time of writing):

- The JWT extractor (`management/server/auth/jwt/extractor.go`) already
  copies the `email` claim into `UserAuth.Email`. It does **not** read
  `email_verified`.
- Every path where a JWT identity joins an existing account funnels through
  `addNewUserToDomainAccount`. That includes the dashboard (HTTP) path, the
  agent login (gRPC, `grpcserver.go` → `GetAccountIDFromUserAuth`), and the
  primary-domain race fallback inside `addNewPrivateAccount`.
- Other ways a human user record is created: SCIM `POST /Users`
  (`user_scim.go`) and the IdP-manager invite (`inviteNewUser`, `user.go`).

## Decision

### D1 — Setting

`types.Settings.AllowedEmailDomains []string` (gorm JSON serializer), exposed
as `settings.allowed_email_domains` on `GET/PUT /api/accounts/{id}`. An
empty or absent list means **off**, which is today's behavior byte-for-byte.

### D2 — Normalization and validation (on write)

Each entry is trimmed and lowercased, and a leading `@` is stripped. The
result must be a syntactically valid DNS name. Duplicates are removed. The
list holds at most 100 entries. Wildcards are rejected (`*.example.com` is
not a domain). Invalid input → 400 `InvalidArgument`.

### D3 — Matching

The domain of an email is the part after the last `@`, lowercased. A match is
an **exact** equality with a list entry. Subdomains are not implied:
`eng.example.com` must be listed explicitly. This keeps the rule auditable
from the list alone.

### D4 — Enforcement points

| Path | Behavior when the list is non-empty |
|---|---|
| `addNewUserToDomainAccount` (first login, HTTP and gRPC) | Deny unless the token's email is **verified** and its domain matches. On deny, no user record is created. |
| SCIM `POST /Users`, `PUT`/`PATCH` changing `userName` | 400 `invalidValue` unless `userName` is an email with an allowed domain. |
| `POST /api/users` invite (IdP-manager deployments) | 400 unless the invited email's domain matches. |

**Not enforced:** existing users (the list is not retroactive; use
`is_blocked`), service users and PATs (no email), and account creation by
the very first user (no settings exist yet; that is the bootstrap owner).

### D5 — Verified email required at login

The JWT extractor gains `UserAuth.EmailVerified`, read from the standard OIDC
`email_verified` claim. At the login gate, a missing `email` claim, a missing
`email_verified` claim, or `email_verified=false` all **deny**. The gate fails
closed. Without this, a connector that lets users choose their own email
would bypass the rule. Dex sets `email_verified` on the tokens it issues; for
the local password connector it is true. The trade-off is that an IdP whose
access tokens carry no `email` / `email_verified` cannot be used together
with this setting. It is opt-in, and the deny is logged with the reason.

### D6 — Errors, logs, audit

- **Login deny:** returns `status.PermissionDenied` with the message
  `email domain not allowed for this account`. The same message is used for
  the unverified case, so an attacker cannot tell which check failed.
- **Log:** one `Warnf` line per deny, with the user id, the email domain, and
  the reason (`domain` / `unverified` / `missing`). The full email address is
  not logged.
- **Audit:** an activity event `UserJoinDeniedByDomain`
  (`user.join.denied_domain`) is recorded on the account, with the domain and
  reason as metadata. Security teams can then see rejected join attempts
  without reading server logs.

### D7 — License posture

openZro-original design: OIDC Core §5.1 (`email`, `email_verified`) plus the
openZro tree. No upstream AGPL code (`management/` post-relicense) was
consulted.

## Non-goals / v2

- Wildcard or suffix matching (`*.example.com`).
- Per-domain role or group mapping on join. Use SCIM (ADR-0024) or JWT groups.
- Retroactively blocking existing users whose domain is no longer listed.
- Multi-account (non-single-account) deployments creating **new** accounts.
  The list is per account and governs joining it, not account creation.

## Risks accepted

- **Fail-closed on missing claims** can lock out new users of an IdP whose
  access tokens omit `email` / `email_verified` when the list is set.
  Existing users are unaffected. The deny log states the reason, and clearing
  the list restores today's behavior.
- **`email_verified` trust is delegated to the IdP or connector.** A
  connector that asserts `email_verified=true` for unverified addresses
  defeats the gate. That is an IdP misconfiguration, outside openZro's
  boundary.

## Verification

- Unit (table-driven): normalization and validation on write (case, `@`
  prefix, whitespace, duplicates, wildcard and invalid rejection, 100-entry
  cap); matching (exact, subdomain not implied, last-`@` split, mixed case).
- Account manager: with the list set, first login is denied for a
  non-matching domain, an unverified email, a missing email, and a missing
  `email_verified`. In every deny case no user is persisted and the deny
  activity event is stored. First login is allowed for a match. An existing
  user with a non-matching domain still logs in. With an empty list,
  behavior is unchanged.
- Extractor: `email_verified` is parsed when true, false, or absent.
- SCIM and invite: a non-matching `userName` / email returns 400.

## References

- OpenID Connect Core 1.0 §5.1 (standard claims `email`, `email_verified`).
- `management/server/account.go` (`GetAccountIDFromUserAuth`,
  `addNewUserToDomainAccount`), `management/server/auth/jwt/extractor.go`,
  `management/server/user_scim.go`, `management/server/user.go`.
- [ADR-0024](./0024-scim-provisioned-local-users.md).
