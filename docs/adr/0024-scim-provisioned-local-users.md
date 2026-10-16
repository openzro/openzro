# ADR-0024 — SCIM-provisioned local users (password credentials in embedded Dex)

## Status

**Proposed**. Design gate for
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
one (see D3).

### D3 — Write ordering, compensation, idempotency

Create: Dex `CreatePassword` first, then `SaveUser`. If `SaveUser` fails,
call `DeletePassword` (best effort; on failure log at `Errorf` with the user
id, not the email).

If `CreatePassword` returns `AlreadyExists`:

- an openZro user with the derived `sub` exists → SCIM **409** `uniqueness`;
- no such user (a leftover from a failed compensation) → adopt: `UpdatePassword`
  with the new hash, then `SaveUser`. The deterministic id from D2 makes this
  safe.

Delete: remove the openZro user first (revokes access immediately), then
`DeletePassword`. A failed Dex delete leaves an inert credential, because no
openZro user maps to it, and the next create with that email adopts it.

### D4 — Password handling

- Plaintext exists only in the request body in memory. Management hashes it
  with bcrypt (cost 12) because Dex accepts hashes only.
- Never logged, never stored by openZro, never returned, never placed in
  activity-event metadata. Audit events record that a password was set or
  changed, not its value.
- v1 policy is a server-side minimum length of 12 characters. Violations
  return SCIM 400 `invalidValue`. A configurable policy is a non-goal.

### D5 — Password lifecycle

`PATCH` (`replace` on `password`) and `PUT` with `password` call Dex
`UpdatePassword`. `changePassword` is advertised as `true` in
`ServiceProviderConfig` **only when the Dex client is wired**.

### D6 — Deactivate vs delete

`active: false` keeps today's semantics (`User.Blocked = true`, enforced by
openZro) and **keeps** the Dex credential, so reactivation needs no new
password. `DELETE` removes both (D3).

### D7 — Gating and zero regression

- The password path runs only when `dex_proxy` is configured. A `password`
  sent without Dex → SCIM 400 with an explicit message. It is never silently
  dropped.
- SCIM requests **without** `password` keep their current behavior
  byte-for-byte. Existing SCIM integrations against external IdPs are
  unaffected.
- `userName` must be an email on the password path (the Dex password DB is
  keyed by email). Changing `userName` on a password-backed user is rejected
  in v1 (Dex cannot re-key a password entry).

### D8 — Groups and MFA: no new mechanism

Groups are assigned through SCIM `/Groups` `members` (existing
`applySCIMGroupMembers` → `AutoGroups`), or through the `AutoGroups` input
on the user. Operators are advised to enable **Require 2FA for local
accounts** (`mfa_enforce_local`), because the Dex password connector has
no MFA of its own.

### D9 — Permissions

Unchanged: `requireSCIMPermission` (Users module), meaning a PAT of an
admin service user.

### D10 — License posture

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
- **An email equal to a `staticPasswords` entry** (the bootstrap admin) is
  shadowed by the static entry at login. Management cannot see Dex's YAML, so
  this is documented rather than enforced.

## Verification

- Unit: `sub` encoder round-trips through `parseDexSubConnector`; table-driven
  create/patch/put/delete with a fake Dex client covering success,
  `AlreadyExists` (both branches of D3), compensation failure, no-Dex gating,
  and password policy.
- Integration (testcontainers, real Dex with `enablePasswordDB`): SCIM create
  with password → password-grant login against Dex → the token's `sub` equals
  the SCIM `id` → `GET /api/users/current` returns the pre-set role and
  groups, and no second user exists.
- Regression: SCIM create **without** password behaves exactly as before.
- Log scan in tests: the plaintext password never appears in log output or
  activity events.

## References

- RFC 7643 §4.1.1 (User `password` attribute), RFC 7644 (SCIM protocol).
- Dex gRPC API v2: <https://github.com/dexidp/dex/blob/master/api/v2/api.proto>
- [ADR-0006](./0006-embed-dex.md) — embedded Dex.
- `management/server/user_scim.go`, `management/server/group_scim.go`,
  `management/server/dex_proxy/client.go`,
  `management/server/auth/jwt/extractor.go`.
