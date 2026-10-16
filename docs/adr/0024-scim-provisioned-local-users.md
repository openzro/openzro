# ADR-0024 — SCIM-provisioned local users (password credentials in embedded Dex)

## Status

**Proposed** (revised after review on #206). Design gate for
[openzro/openzro#205](https://github.com/openzro/openzro/issues/205);
**no code lands with this ADR.** Extends the
SCIM 2.0 surface (`/scim/v2`) and the embedded Dex integration of
[ADR-0006](./0006-embed-dex.md).

## Context

Operators running openZro with the bundled Dex and no external IdP want an
identity-governance (IGA) system to own the user lifecycle: create a user
with an initial password, put them in groups, rotate the password, disable
and delete them. The concrete driver is an operator whose in-house IGA
speaks SCIM 2.0 and must provision openZro without a human in the
dashboard.

**Verified current behavior** (openZro tree at time of writing):

- `POST /api/users` without `is_service_user` goes to `inviteNewUser`
  (`management/server/user.go`), which fails with `IdP manager must be
  enabled to send user invites`. There is no IdP manager for Dex
  (`management/server/idp/` has Auth0, Azure, Keycloak, Zitadel, Authentik,
  Okta, Google, JumpCloud only), so with the bundled Dex the REST API cannot
  create a human user at all.
- `POST /scim/v2/Users` (`management/server/user_scim.go:SCIMCreateUser`)
  stores a `types.User` with `Id = uuid.NewString()`. No credential is
  created anywhere. On first login the JWT `sub` (Dex's id) never equals
  that UUID, so `getAccountIDWithAuthorizationClaims`
  (`management/server/account.go`) creates a **second, unrelated** user via
  `addNewUserToDomainAccount` with role `user` and no groups. The SCIM
  record is orphaned. This is a defect independent of this ADR's feature.
- SCIM Groups (`management/server/group_scim.go`) already map `members` onto
  each member's `User.AutoGroups`, which propagates to their peers. Group
  membership needs no new mechanism once the user id is right.
- `DELETE /scim/v2/Users/{id}` (`SCIMDeactivateUser`) is a soft delete: it
  sets `Blocked=true`, and the user is still returned by `GET` and lists.
- A user record that exists and is `Blocked` is never re-created by JIT on
  login (the record is found by `sub`), and every action it attempts is
  refused (`ValidateUserPermissions` in `permissions/manager.go`, the
  peer-login owner check in `peer.go`).
- In the Dex API, `CreatePassword` is unique per lowercased email.
  `UpdatePassword` and `DeletePassword` are keyed by email only and never read
  or change `user_id`. `ListPasswords` returns `user_id` (not the hash).
  `ListRefresh` / `RevokeRefresh` take the token `sub` (plus `client_id` for
  revoke).
- `ServiceProviderConfig` advertises `changePassword: false`
  (`management/server/http/handlers/scim/discovery.go`).
- Local credentials live only in Dex: `staticPasswords` in
  `infrastructure_files/dex.config.yaml.tmpl` (YAML, restart to change),
  with `enablePasswordDB: true` already set. The Dex gRPC API
  (`github.com/dexidp/dex/api/v2`, already in `go.mod` at v2.4.0) exposes
  `CreatePassword` / `UpdatePassword` / `DeletePassword` / `ListPasswords`
  against that password DB. Management already holds an mTLS gRPC client to
  Dex (`management/server/dex_proxy`, wired from `OPENZRO_DEX_GRPC_*`), used
  today only for connectors.
- The openZro user id **is** the JWT `sub`. For Dex, `sub` is
  `base64url(proto{1: user_id, 2: conn_id})`; for password-DB users
  `conn_id = "local"`. `parseDexSubConnector`
  (`management/server/auth/jwt/extractor.go`) already decodes this format
  for MFA enforcement.

Because the `sub` of a password-DB user is a pure function of a `user_id`
we choose at `CreatePassword` time, management can compute the final user
id **before** the user ever logs in. That is the whole enabling fact.

## Decision

### D1 — Surface: extend SCIM `/Users`, not a new REST endpoint

The feature ships on the existing `/scim/v2/Users` endpoints using the
RFC 7643 §4.1.1 core `password` attribute (writeOnly, `returned: never`).
IGA systems integrate over SCIM; groups already work there; the auth
model (PAT of an admin service user) is already in place. A
`password` field on `POST /api/users` is a non-goal for v1 (it can reuse
the same internal function later).

### D2 — Identity binding: precomputed Dex `sub`

On create with a `password`, management:

1. derives `user_id = UUIDv5(ns, accountID + "/" + lower(email))`, where `ns`
   is a fixed openZro namespace UUID;
2. computes `sub = base64.RawURLEncoding(proto{1: user_id, 2: "local"})` with
   an encoder placed next to `parseDexSubConnector`;
3. stores the `types.User` with `Id = sub`.

On first login, `GetAccountIDByUserID(sub)` finds the pre-provisioned user,
with role and `AutoGroups` already set. No JIT user is created.

The `user_id` is **deterministic** (not random) so a retried create after a
partial failure converges on the same identity instead of minting a second
one (see D4).

### D3 — Local credential state on the user record

`types.User` gains `LocalCredentialState`:

| State | Meaning | Login | Visible to SCIM / `GET /api/users` |
|---|---|---|---|
| `""` | not a password-backed user (every existing user) | as today | as today |
| `pending` | create in progress or interrupted | denied (`Blocked=true`) | no |
| `active` | provisioned; `Blocked` follows SCIM `active` | per `active` | yes |
| `tombstoned` | deleted via SCIM | denied (`Blocked=true`) | no (404, excluded from lists) |

The invariant every step preserves: **a Dex credential never exists for a
`sub` that has no openZro user record.** A local record that is `pending` or
`tombstoned` is always `Blocked`. Because a record with that `sub` exists,
`getAccountIDWithAuthorizationClaims` never takes the JIT path for it. An
authenticated but blocked identity is refused by `ValidateUserPermissions`
(`permissions/manager.go`) and by the peer-login owner check (`peer.go`).
This is what makes the design fail-closed under crashes. Compensation is an
optimization, not the safety mechanism.

### D4 — Create saga (fail-closed, idempotent)

`POST /Users` with `password`:

1. **Validate**: Dex wired, `userName` is an email in the account's
   allowlist (ADR-0025), password policy (D7). Derive `user_id` and `sub`
   (D2).
2. **Reserve locally** (one transaction) by looking up the record with
   `Id = sub`:
   - absent → insert it as `pending`, `Blocked=true`, with the requested
     attributes;
   - `pending` → resume (a retried create after a crash or 500);
   - `tombstoned` → re-provision: switch to `pending`, `Blocked=true`, and
     apply the new attributes;
   - `active` → SCIM **409** `uniqueness`; nothing else happens.
3. **Credential**: `CreatePassword(email, hash, user_id)`. On `AlreadyExists`,
   run the ownership check (D5). If the existing entry carries the derived
   `user_id`, it is a leftover from an earlier attempt for this same identity:
   overwrite it with `UpdatePassword`. Otherwise return **409** and leave the
   credential untouched. If this request inserted the `pending` record in
   step 2, delete it; a failed delete leaves it blocked and hidden, which is
   harmless.
4. **Finalize** (one transaction): `active`, with `Blocked = !active` as
   requested.

A crash or error after step 2 leaves a blocked `pending` record, with or
without a credential. Neither state can authenticate into openZro, and
retrying the same request resumes at step 2 and converges. The derived
`user_id` is deterministic for exactly this reason. Any failure returns 500,
meaning "outcome unknown, retry", which is standard SCIM client behavior.

### D5 — Credential ownership is verified, never assumed

The Dex password DB is keyed by lowercased email. `UpdatePassword` and
`DeletePassword` take only the email and neither read nor change `user_id`.
So "no openZro user has this `sub`" does **not** prove that an existing entry
is ours. It may belong to another account (a different derived `user_id`) or
to an operator-created entry.

Before every `UpdatePassword` or `DeletePassword`, management calls
`ListPasswords`, finds the entry by case-insensitive email, and requires
`entry.user_id == derived user_id`:

- on create, a mismatch → **409**, credential untouched (D4 step 3);
- on update or delete, a mismatch → **500**, no Dex write, and an `Errorf`
  naming the user id. It indicates out-of-band tampering, so it must not be
  "fixed" automatically.

`ListPasswords` is a full scan. That is acceptable at the expected number of
local users. If it becomes a bottleneck, the fix is a Dex-side lookup, not
skipping the check.

### D6 — Delete and deactivate (password-backed users only)

`DELETE /Users/{id}` on a user with `LocalCredentialState != ""`:

1. **Tombstone** (one transaction): `tombstoned`, `Blocked=true`. From this
   commit on, SCIM `GET` returns 404, the user is excluded from lists, and a
   new `POST` with the same `userName` re-provisions it (D4 step 2), as
   RFC 7644 §3.6 requires. Access tokens issued before the delete are still
   valid JWTs, but they hit an existing, blocked record: no JIT, no access.
2. **Revoke sessions**: `ListRefresh(sub)` → `RevokeRefresh(sub, client_id)`
   for each client.
3. **Remove the credential**: `DeletePassword(email)` after the ownership
   check (D5). `NotFound` counts as success.

The response is 204 once step 1 commits, because access is closed at that
point. Steps 2–3 are best effort and logged on failure. They are re-run on
any later `DELETE` of the tombstone (still 404 to SCIM, but cleanup is
retried) and implicitly by re-provisioning. The tombstone is kept
indefinitely: it is the permanent deny for that `sub` until an explicit
re-provision. `DELETE /api/users/{id}` on a password-backed user follows the
same steps instead of removing the row, so the REST path cannot reopen JIT
either.

`active: false` sets `Blocked=true`, revokes refresh tokens (step 2 above) and
**keeps** the credential, so reactivation needs no new password.

### D7 — Password policy

- Minimum 12 characters, counted as Unicode code points.
- **Maximum 72 bytes of UTF-8.** bcrypt rejects longer inputs
  (`bcrypt.ErrPasswordTooLong` in the `x/crypto` version in `go.mod`), and Dex
  verifies logins with bcrypt, so neither truncation nor pre-hashing is an
  option without diverging from Dex's own check.
- Violations → SCIM 400 `invalidValue`. The message states the limits and
  never echoes the value.
- Plaintext exists only in the request body in memory. Management hashes it
  with bcrypt (cost 12). It is never logged, stored by openZro, returned, or
  placed in activity-event metadata. Audit events record that a password was
  set or changed, not its value.

### D8 — Password change: ordering and partial failure

`PATCH` may touch `password` only with `replace` (`add`/`remove` → 400). A
`PUT` or `PATCH` carrying `password` runs in a fixed, restrictive-first order:

1. Commit every non-password change **except** a re-activation (an `active`
   change from false to true is held back). This includes deactivation and
   group removals, so restrictions take effect before anything else.
2. Ownership check (D5) → `UpdatePassword`, then revoke refresh tokens, so a
   password change ends existing sessions.
3. Commit the held-back re-activation, if any.

Every step is idempotent: attribute replacement, set-based group membership,
and re-hashing the same password. Any failure returns 500 and leaves the
user at a prefix of this sequence. `UpdatePassword` does not return the
previous hash, so no rollback is attempted. The contract is instead that
**after a 500, the password may be old or new and the client must retry the
same request**, which converges. Because re-activation is always last, a
partial failure can never leave a user newly active with an unintended
credential.

`changePassword` is advertised as `true` in `ServiceProviderConfig` only
when the Dex client is wired.

### D9 — Gating and zero regression, scoped

- The password path runs only when `dex_proxy` is configured. A `password`
  sent without Dex → SCIM 400 with an explicit message. It is never silently
  dropped.
- Users with `LocalCredentialState == ""` (every user that exists today, and
  any SCIM user created without `password`) keep their current behavior
  byte-for-byte, **including** today's `DELETE` semantics (soft delete:
  `Blocked=true`, still returned by `GET`). That legacy behavior deviates
  from RFC 7644 §3.6 and is tracked in
  [#210](https://github.com/openzro/openzro/issues/210). It is not changed here, so
  existing IdP integrations see no difference.
- `userName` must be an email on the password path. Changing `userName` on a
  password-backed user is rejected in v1: Dex cannot re-key a password entry,
  and the derived `sub` depends on it.

### D10 — Groups and MFA: no new mechanism

Groups are assigned through SCIM `/Groups` `members` (existing
`applySCIMGroupMembers` → `AutoGroups`), or through the `AutoGroups` input
on the user. Operators are advised to enable **Require 2FA for local
accounts** (`mfa_enforce_local`), because the Dex password connector has
no MFA of its own.

### D11 — Permissions

Unchanged: `requireSCIMPermission` (Users module), meaning a PAT of an
admin service user.

### D12 — License posture

NetBird v0.62 shipped "built-in local users" over an embedded IdP inside
upstream `management/`, which is **AGPLv3**. That code and its diffs are not
consulted. This design comes from RFC 7643/7644, the public Dex gRPC API
(`api/v2`, Apache-2.0), and the openZro tree. No new dependencies:
`golang.org/x/crypto/bcrypt` and `google.golang.org/protobuf/encoding/protowire`
are already in the module graph.

## Non-goals / v2

- Linking SCIM-created users to **federated** Dex connectors (Google, Entra, …).
  The orphan defect described in Context remains for that path. It needs the
  connector-side subject, which SCIM does not carry.
- `password` on `POST /api/users`.
- Self-service password reset, password expiry, configurable complexity policy.
- Migrating existing `staticPasswords` entries into the password DB.
- SCIM bulk operations.

## Risks accepted

- **Dex `sub` format coupling.** The format has been stable since Dex 2.x and
  is already relied on by MFA enforcement. It is pinned by an integration test
  against a real Dex (see Verification). A Dex upgrade that changes it fails
  CI, not production logins.
- **Plaintext password in transit.** This is inherent to SCIM provisioning.
  TLS on the management endpoint is required (it already is for the PAT).
- **Global email uniqueness.** One Dex instance serves the deployment, so a
  local email is unique deployment-wide, not per account. This is acceptable
  for self-hosted single-account deployments.
- **Leftover Dex state.** A `pending` or `tombstoned` user may still have a
  Dex credential or refresh token after a failed step. It can obtain Dex
  tokens, but openZro refuses them (blocked record, no JIT). Cleanup is
  retried on the next create or delete.
- **Tombstones accumulate.** One row per deleted password-backed user, kept
  as a permanent deny until re-provisioned. The cost is negligible.
- **`ListPasswords` is a full scan** on every credential write (D5).
  Acceptable at the expected number of local users.
- **An email equal to a `staticPasswords` entry** (the bootstrap admin) is
  shadowed by the static entry at login. Management cannot see Dex's YAML, so
  this is documented rather than enforced.

## Verification

- **Unit:**
  - the `sub` encoder round-trips through `parseDexSubConnector`;
  - table-driven create, patch, put and delete against a fake Dex client and
    store with fault injection **at every saga step** (D4, D6, D8), asserting
    the resulting `LocalCredentialState` and `Blocked`, and that the same
    request retried converges;
  - no-Dex gating;
  - `add`/`remove` on `password` rejected.
- **Ownership (D5):**
  - the same email provisioned in two accounts → 409 for the second, and the
    first account's credential still logs in;
  - an `AlreadyExists` entry with a foreign `user_id` is never updated or
    deleted;
  - an update or delete with mismatched ownership → 500 and no Dex write.
- **Password policy (D7):** exactly 72 bytes accepted; 73 bytes rejected;
  multi-byte UTF-8 at the boundary (e.g. 24 three-byte characters = 72 bytes
  accepted, 25 rejected); 11 code points rejected; 12 accepted.
- **Integration** (testcontainers, real Dex with `enablePasswordDB`):
  - SCIM create with password → password-grant login against Dex → the
    token's `sub` equals the SCIM `id` → `GET /api/users/current` returns the
    pre-set role and groups, and no second user exists;
  - kill between D4 steps 3 and 4 → a login with the new credential is
    refused and no JIT user appears → retry finalizes;
  - `DELETE` → an access token obtained before the delete is refused and does
    not re-create the user → refresh tokens are revoked → `GET` is 404 →
    `POST` with the same `userName` re-provisions;
  - a password change revokes existing refresh tokens.
- **Regression:** SCIM create, `DELETE` and `GET` **without** password behave
  exactly as before (soft delete still returned).
- **Log scan in tests:** the plaintext password never appears in log output
  or activity events.

## References

- RFC 7643 §4.1.1 (User `password` attribute), RFC 7644 (SCIM protocol).
- Dex gRPC API v2: <https://github.com/dexidp/dex/blob/master/api/v2/api.proto>
- [ADR-0006](./0006-embed-dex.md) — embedded Dex.
- `management/server/user_scim.go`, `management/server/group_scim.go`,
  `management/server/dex_proxy/client.go`,
  `management/server/auth/jwt/extractor.go`.
