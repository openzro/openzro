# ADR-0024 — SCIM-provisioned local users (password credentials in embedded Dex)

## Status

**Proposed** (revised after two review rounds on #206). Design gate for
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
  revoke). `ListPasswords` also returns `staticPasswords` entries, which Dex
  treats as read-only (writes to them are rejected).
- `Blocked=true` gates new activity only. The REST block path also expires the
  user's peers (`expireAndUpdatePeers`). The SCIM block paths do not, so
  connected peers keep access (#211). PATs are removed only by the hard
  `DeleteUser` path.
- `AcquireWriteLockByUID` is a process-local mutex, not a deployment-wide
  lock. `cluster.Coordinator.Lock` is the HA primitive.
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

### D2 — Identity binding: one fresh `sub` per provisioning

Each provisioning of a local user gets a **new identity**:

1. management generates a random `user_id` (UUIDv4) when it reserves the user
   (D4 step 2) and persists it on the `pending` record;
2. it computes `sub = base64.RawURLEncoding(proto{1: user_id, 2: "local"})`
   with an encoder placed next to `parseDexSubConnector`;
3. the `types.User` is stored with `Id = sub`.

On first login, `GetAccountIDByUserID(sub)` finds the pre-provisioned user,
with role and `AutoGroups` already set. No JIT user is created.

A `sub` is **never reused**. Re-creating a user after `DELETE` mints a new
`sub` and leaves the old one tombstoned forever (D6). Nothing bound to the
old identity can attach to the new one: access tokens issued before the
delete, PATs, MFA enrollment, peers and Dex refresh tokens. Idempotency
comes from the persisted attempt, not from deriving the id: a retried create
finds its own `pending` record by `userName` and resumes with the `user_id`
stored there.

### D3 — Local credential state on the user record

`types.User` gains `LocalCredentialState` and `LocalCleanupPending`:

| State | Meaning | Access | Visible to SCIM / `GET /api/users` |
|---|---|---|---|
| `""` | not a password-backed user (every existing user) | as today | as today |
| `pending` | create in progress or interrupted | none (`Blocked=true`) | no |
| `active` | provisioned; `Blocked` follows SCIM `active` | per `active` | yes |
| `tombstoned` | deleted via SCIM; permanent deny for this `sub` | none (`Blocked=true`) | no (404, excluded from lists, does not block a new create) |

`LocalCleanupPending` is set when a revocation step (D6) has not completed
yet. **No transition to `Blocked=false` is allowed while it is set.** The
cleanup must first be re-run successfully.

Invariant: **a Dex credential never exists for a `sub` that has no openZro
user record.** A record that is `pending` or `tombstoned` is always
`Blocked`. Because a record with that `sub` exists,
`getAccountIDWithAuthorizationClaims` never takes the JIT path for it. Note
that `Blocked=true` on its own only gates **new** activity (API permission
checks and peer login). Every transition to blocked in this ADR therefore
also expires the user's peers (D6). Today's SCIM block path does not do this
for legacy users either; that is fixed separately in #211.

### D4 — Create saga (fail-closed, idempotent)

`POST /Users` with `password`, entirely under the identity lock (D10):

1. **Validate**: Dex wired, `userName` is an email in the account's
   allowlist (ADR-0025), password policy (D7).
2. **Reserve locally** (one transaction): look up the live (non-tombstoned)
   record with this `userName` in the account:
   - absent → insert `pending`, `Blocked=true`, a fresh `user_id`/`sub` (D2),
     and the requested attributes;
   - `pending` → resume with its stored `user_id`;
   - `active` → SCIM **409** `uniqueness`; nothing else happens.
3. **Credential preflight**: `ListPasswords`, then find the entry by
   case-insensitive email:
   - no entry → `CreatePassword(email, hash, user_id)`;
   - entry whose `user_id` equals this record's `user_id`, **and** the record
     existed as `pending` before this request (a resumed attempt) →
     `UpdatePassword`;
   - entry whose `user_id` belongs to a **tombstone of this account with the
     same `userName`** (leftover cleanup) → `DeletePassword`, then
     `CreatePassword`;
   - anything else → **409**, credential untouched. That covers another
     account, a read-only `staticPasswords` entry, an unknown writer, or a
     matching `user_id` on a reservation created in this very request. If
     this request created the reservation, delete it. A failed delete leaves
     it blocked and hidden, which is harmless.
4. **Finalize** (one transaction, fenced as in D10): `active`, and
   `Blocked = !active` as requested.

A crash or error after step 2 leaves a blocked `pending` record, with or
without a credential. Neither can authenticate into openZro. A retry of the
same request resumes at step 2 and converges. Any failure returns 500,
meaning "outcome unknown, retry".

### D5 — Credential ownership is verified, never assumed

The Dex password DB is keyed by lowercased email. `UpdatePassword` and
`DeletePassword` take only the email and neither read nor change `user_id`.
Every Dex password write is therefore preceded by the `ListPasswords`
preflight, and the write proceeds only if the entry's `user_id` is one this
saga owns (D4 step 3, D6 step 5). On update or delete, a mismatch → 500, no
Dex write, and an `Errorf` naming the user id: it indicates out-of-band
tampering and is not "fixed" automatically.

`ListPasswords` also returns the read-only `staticPasswords` entries (Dex
rejects writes to them), so a collision with the bootstrap admin email is
detected by the same preflight and surfaces as 409.

**Assumption:** openZro is the only writer of the Dex password DB. The
identity lock (D10) closes the window between `ListPasswords` and the write
against openZro's own replicas. A third party writing to Dex concurrently is
out of scope; the worst case is a 500 or 409 on the next request, never a
credential for an unowned `sub`.

`ListPasswords` is a full scan. That is acceptable at the expected number of
local users.

### D6 — Delete and deactivate (password-backed users only)

`DELETE /Users/{id}` on a user with `LocalCredentialState != ""`, under the
identity lock:

1. **Tombstone** (one transaction): `tombstoned`, `Blocked=true`,
   `LocalCleanupPending=true`. From this commit on, the user is hidden from
   SCIM and a new `POST` with the same `userName` creates a new identity
   (D2).
2. **Expire peers**: the same `expireAndUpdatePeers` path the REST block
   uses. Connected peers are marked `LoginExpired`, their channels are closed,
   and the account is updated.
3. **Delete the user's PATs** and its MFA enrollment.
4. **Revoke sessions**: `ListRefresh(sub)` → `RevokeRefresh(sub, client_id)`
   for each client.
5. **Remove the credential**: preflight (D5), then `DeletePassword(email)`.
   `NotFound` counts as success.
6. Clear `LocalCleanupPending`.

The response is **204 only after step 6**. If any of steps 2–5 fails, the
tombstone stays (access is already denied) and the response is **500**. The
IGA retries the `DELETE`: a `DELETE` on a tombstone with
`LocalCleanupPending` resumes the remaining steps and returns 204 once they
converge. A `DELETE` on a fully cleaned tombstone returns 404, as RFC 7644
requires for an already-deleted resource. Access tokens issued before the
delete stay valid JWTs until they expire, but they carry the old `sub`, which
maps to a blocked tombstone forever.

`active: false` runs steps 2 and 4 (expire peers, revoke refresh tokens) and
**keeps** the credential and PATs, so reactivation needs no new password.
Failure → 500 with `LocalCleanupPending` set, and per D3 the user cannot be
re-activated until a later request completes the cleanup.

`DELETE /api/users/{id}` on a password-backed user follows the same steps
instead of removing the row, so the REST path cannot reopen JIT either.

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
`PUT` or `PATCH` carrying `password` runs under the identity lock in three
phases, so a failure can never leave access wider than either the old or the
requested state:

1. **Restrictions**: deactivation (with D6's peer expiry and refresh
   revocation), group and role removals.
2. **Credential**: preflight (D5) → `UpdatePassword` → revoke refresh tokens.
3. **Expansions**: re-activation (only if `LocalCleanupPending` is clear),
   group and role additions.

Every step is idempotent: attribute replacement, set-based membership,
re-hashing the same password. Any failure returns 500 and leaves the user at
a prefix of this sequence. `UpdatePassword` does not return the previous
hash, so no rollback is attempted. The contract is that **after a 500, the
password may be old or new, expansions have not been applied, and the client
must retry the same request**, which converges.

**What a password change does not do.** Revoking refresh tokens stops
renewal only. Access tokens already issued stay valid until their TTL, and
connected peers keep their WireGuard sessions, because those are bound to the
peer key, not the password. A password change is therefore not an access
cut. To remove access immediately, the client sends `active: false` (D6).

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
  [#210](https://github.com/openzro/openzro/issues/210). The legacy block path
  not expiring peers is a security fix tracked in
  [#211](https://github.com/openzro/openzro/issues/211), independent of this
  ADR.
- `userName` must be an email on the password path. Changing `userName` on a
  password-backed user is rejected in v1: Dex cannot re-key a password entry.

### D10 — Concurrency across replicas

`AcquireWriteLockByUID` is a process-local `sync.RWMutex`
(`store/sql_store.go`), so on its own it does not serialize a saga across HA
replicas. Every saga in D4, D6 and D8 runs under a **deployment-wide identity
lock**:

- key: `scim-local:` + hex(SHA-256(accountID + "/" + lower(email))). It is
  per identity, so independent users don't serialize on bcrypt and the
  `ListPasswords` scan, and the plaintext email never appears in the key;
- primitive: `cluster.Coordinator.Lock` (TTL with heartbeat). Single-replica
  deployments without a coordinator fall back to the process-local lock,
  which is sufficient there.

**Fencing.** Each local transaction re-reads the record and compares the
expected `(Id, LocalCredentialState, LocalCleanupPending)` before writing. A
mismatch aborts the saga with 500 and issues no further Dex writes. This
protects against a lock lost mid-saga (an expired TTL during a stall).

### D11 — Groups and MFA: no new mechanism

Groups are assigned through SCIM `/Groups` `members` (existing
`applySCIMGroupMembers` → `AutoGroups`), or through the `AutoGroups` input
on the user. Operators are advised to enable **Require 2FA for local
accounts** (`mfa_enforce_local`), because the Dex password connector has
no MFA of its own.

### D12 — Permissions

Unchanged: `requireSCIMPermission` (Users module), meaning a PAT of an
admin service user.

### D13 — License posture

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
  local email is unique deployment-wide, not per account. A second account
  provisioning the same email gets 409 (D4 step 3).
- **Access-token TTL window.** A password change does not invalidate access
  tokens already issued (D8). Deactivation and delete do close access
  immediately, through blocked-record checks and peer expiry.
- **Leftover Dex state.** A `pending` record may carry a Dex credential after
  a failed step. It can obtain Dex tokens, but openZro refuses them (blocked
  record, no JIT), and the next retry adopts or replaces it.
- **Tombstones accumulate.** One row per deleted password-backed user, kept
  forever as the deny for that `sub`. The cost is negligible.
- **`ListPasswords` is a full scan** on every credential write (D5).
  Acceptable at the expected number of local users.
- **Third-party writers to the Dex password DB** are outside the model (D5).
- **Lock lost mid-saga.** If the identity lock's TTL expires during a stall,
  fencing (D10) stops the local finalize, but a Dex write issued just before
  the stall can still race with the new holder. The outcome is a 500 or 409
  and a retry, never access for an unowned `sub`.

## Verification

- **Unit:**
  - the `sub` encoder round-trips through `parseDexSubConnector`;
  - table-driven create, patch, put and delete against a fake Dex client and
    store with fault injection **at every saga step** (D4, D6, D8), asserting
    `LocalCredentialState`, `LocalCleanupPending` and `Blocked`, and that the
    same request retried converges;
  - DELETE returns 500 while cleanup is incomplete, 204 once it converges,
    and 404 afterwards;
  - re-activation is refused while `LocalCleanupPending` is set;
  - a phase-2 failure in D8 never applies phase-3 expansions;
  - no-Dex gating; `add`/`remove` on `password` rejected.
- **Ownership (D5):**
  - the same email in two accounts → 409 for the second, and the first
    account's credential still logs in;
  - a `staticPasswords` email → 409;
  - a matching `user_id` on a reservation created in the same request → 409;
  - an update or delete with mismatched ownership → 500 and no Dex write.
- **Password policy (D7):** exactly 72 bytes accepted; 73 bytes rejected;
  multi-byte UTF-8 at the boundary (24 three-byte characters = 72 bytes
  accepted, 25 rejected); 11 code points rejected; 12 accepted.
- **No identity resurrection (D2, D6):** DELETE then `POST` with the same
  `userName` → the new `id` differs. Every credential bound to the old
  identity is refused **after** the new user is active: a pre-delete access
  token, a refresh token, a PAT, the MFA enrollment, and a peer.
- **Peers (D3, D6):** a connected peer is expired and its channel closed on
  both `active:false` and DELETE.
- **HA (D10)**, with the two-replica harness: concurrent create/create
  (exactly one 201, one 409; the stored hash matches the winner),
  delete/recreate (no active user without a credential; no 204 for a
  resource the other replica re-created), and update/delete.
- **Integration** (testcontainers, real Dex with `enablePasswordDB`):
  - SCIM create with password → password-grant login against Dex → the
    token's `sub` equals the SCIM `id` → `GET /api/users/current` returns the
    pre-set role and groups, and no second user exists;
  - kill between D4 steps 3 and 4 → a login with the new credential is
    refused and no JIT user appears → retry finalizes;
  - DELETE revokes refresh tokens; a password change revokes them too.
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
