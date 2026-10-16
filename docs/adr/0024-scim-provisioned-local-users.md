# ADR-0024 — SCIM-provisioned local users (password credentials in embedded Dex)

## Status

**Proposed** (v1 scope reduced after two review rounds on #206). Design gate for
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

### Scope: a minimal, fail-closed v1

v1 covers exactly the operations the driving IGA needs:

- create a local user with an initial password;
- reset the password, as an isolated operation;
- manage group membership (existing SCIM `/Groups`);
- deactivate or delete the user, **terminally**.

Re-activating a deactivated local user and re-creating a local user with an
email that was used before are **out of v1**. They are what make identity
reuse, and with it token/PAT/MFA/peer resurrection, possible. The section
"v2: re-activation and re-creation" records the mechanisms they require, so
they are not lost.

### D1 — Surface: extend SCIM `/Users`, not a new REST endpoint

The feature ships on the existing `/scim/v2/Users` endpoints using the
RFC 7643 §4.1.1 core `password` attribute (writeOnly, `returned: never`).
IGA systems integrate over SCIM; groups already work there; the auth model
(PAT of an admin service user) is already in place. A `password` field on
`POST /api/users` is a non-goal.

### D2 — Identity binding: a fresh `sub`, persisted before Dex

At reservation (D4 step 2) management generates a random `user_id`
(UUIDv4), persists it on the `pending` record, and computes
`sub = base64.RawURLEncoding(proto{1: user_id, 2: "local"})` with an encoder
placed next to `parseDexSubConnector`. The `types.User` is stored with
`Id = sub`. On first login, `GetAccountIDByUserID(sub)` finds the
pre-provisioned user, with role and `AutoGroups` already set, so no JIT user
is created.

Idempotency comes from the persisted attempt: a retried create finds its own
`pending` record by `userName` and resumes with the stored `user_id`. A
`sub` is never reused.

### D3 — Local credential state on the user record

`types.User` gains `LocalCredentialState` and `LocalCleanupPending`:

| State | Meaning | Access | Visible to SCIM |
|---|---|---|---|
| `""` | not a password-backed user (every existing user) | as today | as today |
| `pending` | create in progress or interrupted | none (`Blocked=true`) | no |
| `active` | provisioned and usable | yes | yes, `active: true` |
| `deactivated` | terminal; deactivated via `active: false` | none (`Blocked=true`) | yes, `active: false` |
| `tombstoned` | terminal; deleted via `DELETE` | none (`Blocked=true`) | no (404, excluded from lists) |

`LocalCleanupPending` marks a terminal transition whose cleanup (D7) has not
converged yet.

**Terminal means terminal on every path.** The store-level user save
(`SaveUser`, and through it the REST `SaveOrAddUser(s)` used by the dashboard
and any internal caller) refuses `Blocked=false` when
`LocalCredentialState` is `pending`, `deactivated` or `tombstoned`. Only the
create saga's own finalize step may unblock a `pending` record. Enforcing
this only in the SCIM handlers would let an administrator or the dashboard
unblock the same record and revive access tokens issued before the
deactivation.

Invariant: **a Dex credential never exists for a `sub` that has no openZro
user record**, and every non-`active` record is `Blocked`. Because a record
with that `sub` exists, `getAccountIDWithAuthorizationClaims` never takes
the JIT path for it. An authenticated but blocked identity is refused by
`ValidateUserPermissions` and by the peer-login owner check. Connected peers
are cut off explicitly on every block (D7, building on the #211 fix). Under
crashes the design fails closed: any interrupted step leaves the user
without access, and compensation is only an optimization.

### D4 — Create saga (fail-closed, idempotent)

`POST /Users` with `password`, entirely under the identity lock (D9):

1. **Validate**: Dex wired, `userName` is an email in the account's allowlist
   (ADR-0025), password policy (D8).
2. **Reserve locally** (one transaction): look up any password-backed record
   with this `userName` in the account:
   - absent → insert `pending`, `Blocked=true`, a fresh `user_id`/`sub`, and
     the requested attributes;
   - `pending` → resume with its stored `user_id`;
   - `active`, `deactivated` or `tombstoned` → SCIM **409** `uniqueness`.
     There is no re-creation in v1.
3. **Credential preflight**: `ListPasswords`, then find the entry by
   case-insensitive email:
   - no entry → `CreatePassword(email, hash, user_id)`. If it nonetheless
     returns `AlreadyExists` (a writer slipped in after the preflight), run
     the ownership check again and return **409**. This path never adopts the
     entry automatically;
   - an entry whose `user_id` equals this record's, **and** the record existed
     as `pending` before this request (a resumed attempt) → `UpdatePassword`;
   - anything else → **409**, credential untouched. That covers another
     account, a read-only `staticPasswords` entry, an unknown writer, or a
     matching `user_id` on a reservation created in this very request. If
     this request created the reservation, delete it. A failed delete leaves
     it blocked and hidden, which is harmless.
4. **Finalize** (one transaction, fenced as in D9): `active`,
   `Blocked=false`. A create with `active: false` is rejected (400). An
   inactive local user is a terminal state, not an initial one.

Validation and conflicts are reported as 400 and 409, as stated in each
step. Any internal failure after the reservation returns 500, meaning
"outcome unknown, retry". The same request resumes at step 2 and converges.

### D5 — Credential ownership is verified, never assumed

The Dex password DB is keyed by lowercased email. `UpdatePassword` and
`DeletePassword` take only the email and neither read nor change `user_id`.
Every Dex password write is therefore preceded by the `ListPasswords`
preflight, and it proceeds only if the entry's `user_id` is the one stored on
this user's record. On reset or delete, a mismatch → 500, no Dex write, and
an `Errorf` naming the user id: it indicates out-of-band tampering and is
not "fixed" automatically.

`ListPasswords` also returns the read-only `staticPasswords` entries, so a
collision with the bootstrap admin email is caught by the same preflight
(409 on create).

**Assumption:** openZro is the only writer of the Dex password DB. The
identity lock (D9) closes the window between `ListPasswords` and the write
against openZro's own replicas. Third-party writers are out of scope.
`ListPasswords` is a full scan, acceptable at the expected number of local
users.

### D6 — Password reset is an isolated operation

A request that sets `password` may change nothing else:

- `PATCH`: the `password` operation must be the only operation, and must be a
  `replace`. Anything else → 400.
- `PUT`: accepted with `password` only if every other attribute equals the
  current representation. SCIM clients commonly reset passwords with a full
  `PUT`, and this keeps them working. Any other difference → 400.

The reset runs under the identity lock: preflight (D5) → `UpdatePassword` →
revoke the user's Dex refresh tokens (`ListRefresh(sub)` →
`RevokeRefresh(sub, client_id)`). It is only allowed on an `active` user.
Every step is idempotent. After a 500 the password may be old or new, and the
client retries the same request, which converges.

**What a reset does not do.** Revoking refresh tokens stops renewal only.
Access tokens already issued stay valid until their TTL, and connected peers
keep their WireGuard sessions, which are bound to the peer key, not the
password. To cut access immediately, the client deactivates the user (D7).

### D7 — Terminal deactivation and DELETE, with converging cleanup

`active: false` (via `PUT` or `PATCH`) and `DELETE` on a password-backed user
run the same cleanup under the identity lock:

1. **Mark** (one transaction): `deactivated` or `tombstoned`, `Blocked=true`,
   `LocalCleanupPending=true`.
2. **Expire peers**: the `expireAndUpdatePeers` path (#211). Connected peers
   are marked `LoginExpired` and their Sync streams are ended **on every
   replica** through the cluster-wide `DisconnectPeers` broadcast. Peers that
   were already expired are disconnected again, because the mark alone does
   not prove disconnection (#212). A failed broadcast fails the step.
3. **Delete the user's PATs.**
4. **Revoke refresh tokens**: `ListRefresh(sub)` → `RevokeRefresh` per
   client.
5. **Remove the credential**: preflight (D5), then `DeletePassword(email)`.
   `NotFound` counts as success.
6. Clear `LocalCleanupPending`.

The response is a success only after step 6. If any of steps 2–5 fails, the
record stays blocked (access is already denied at the API, at peer login, and
for any peer already expired) and the response is **500**. The IGA retries
the same request, which resumes the remaining steps. Specifically:

- `DELETE` on a tombstone with cleanup pending resumes and returns 204;
- `DELETE` on a fully cleaned tombstone returns 404 (RFC 7644 §3.6);
- deactivating an already `deactivated` user resumes any pending cleanup and
  returns the resource;
- `DELETE` of a `deactivated` user runs the same steps and moves it to
  `tombstoned`.

Re-activation (`active: true` on a `deactivated` user) → 400 with an explicit
"not supported for local users" message. The same rule holds on the REST
path (D3). `DELETE /api/users/{id}` on a
password-backed user follows the same cleanup instead of removing the row,
so the REST path cannot reopen JIT.

### D8 — Password policy

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
  set or reset, not its value.

### D9 — Concurrency across replicas

`AcquireWriteLockByUID` is a process-local `sync.RWMutex`
(`store/sql_store.go`), so on its own it does not serialize a saga across HA
replicas. Every saga in D4, D6 and D7 runs under a **deployment-wide identity
lock**:

- key: `scim-local:` + hex(SHA-256(`"dex-password\x00"` + lower(email))). It
  is **global per email, not per account**, because the Dex password DB is
  unique per email across the whole deployment: two accounts creating the
  same email concurrently must contend for one lock. The `dex-password\x00`
  prefix provides domain separation from any other hashed key. It is still
  per identity, so independent users don't serialize on bcrypt and the
  `ListPasswords` scan, and no plaintext email appears in the key;
- primitive: the existing `cluster.Coordinator.Lock` (TTL with heartbeat), so
  no new infrastructure. Single-replica deployments without a coordinator
  fall back to the process-local lock, which is sufficient there.

**Fencing.** Each local transaction re-reads the record and compares the
expected `(Id, LocalCredentialState, LocalCleanupPending)` before writing. A
mismatch aborts the saga with 500 and issues no further Dex writes, which
covers a lock lost mid-saga.

### D10 — Gating and zero regression, scoped

- The password path runs only when `dex_proxy` is configured. A `password`
  sent without Dex → SCIM 400 with an explicit message. It is never silently
  dropped.
- Users with `LocalCredentialState == ""` (every user that exists today, and
  any SCIM user created without `password`) keep their current behavior
  byte-for-byte, including today's soft `DELETE` (`Blocked=true`, still
  returned by `GET`). That legacy behavior deviates from RFC 7644 §3.6 and is
  tracked in [#210](https://github.com/openzro/openzro/issues/210). The legacy
  block paths now expire peers too: that is the #211 fix, shipped
  independently in [#212](https://github.com/openzro/openzro/pull/212).
- `userName` must be an email on the password path. Changing `userName` on a
  password-backed user is rejected (400): Dex cannot re-key a password entry.

### D11 — Groups and MFA: no new mechanism

Groups are assigned through SCIM `/Groups` `members` (existing
`applySCIMGroupMembers` → `AutoGroups`), or through the `AutoGroups` input
on the user, in requests that do not carry `password` (D6). Operators are
advised to enable **Require 2FA for local accounts** (`mfa_enforce_local`),
because the Dex password connector has no MFA of its own.

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

## v2: re-activation and re-creation

Out of v1 by design. Both re-open a terminal identity, so each needs the
following before it can ship. These were established in the #206 review.

- **Re-creation of a previously used email.** A new provisioning must mint a
  new `user_id`/`sub` (already true by D2) and leave the old record
  tombstoned forever, so access tokens, PATs, MFA enrollment, peers and
  refresh tokens bound to the old identity can never attach to the new one.
  The create preflight must recognize a leftover Dex entry owned by this
  account's tombstone for the same `userName`, and delete it before creating
  the new credential.
- **Re-activation.** It reuses the same `sub`, so it is safe only if every
  credential issued before the deactivation is dead: refresh tokens revoked
  and verified, PATs and MFA sessions removed, peers expired, and either
  access tokens past their maximum TTL or a per-user token epoch/cutoff
  enforced on every authentication path. No transition to `Blocked=false`
  while `LocalCleanupPending` is set.
- **Mixed updates with `password`.** If allowed, they must run
  restrictive-first: restrictions (deactivation, removals) → credential +
  refresh revocation → expansions (re-activation, additions). That way a
  partial failure never leaves access wider than either the old or the
  requested state.

## Non-goals

- Linking SCIM-created users to **federated** Dex connectors (Google, Entra, …).
  The orphan defect described in Context remains for that path. It needs the
  connector-side subject, which SCIM does not carry.
- `password` on `POST /api/users`.
- Self-service password reset, password expiry, configurable complexity policy.
- Migrating existing `staticPasswords` entries into the password DB.
- SCIM bulk operations.

## Risks accepted

- **No way back in v1.** A local user deactivated or deleted by mistake, or a
  person who leaves and returns, cannot reuse the same email until v2.
  Accepted with the driving operator; the alternative is a different email.
- **Dex `sub` format coupling.** The format has been stable since Dex 2.x and
  is already relied on by MFA enforcement. It is pinned by an integration test
  against a real Dex (see Verification). A Dex upgrade that changes it fails
  CI, not production logins.
- **Plaintext password in transit.** This is inherent to SCIM provisioning.
  TLS on the management endpoint is required (it already is for the PAT).
- **Global email uniqueness.** One Dex instance serves the deployment, so a
  local email is unique deployment-wide. A second account provisioning the
  same email gets 409.
- **Access-token TTL window after a reset** (D6). Deactivation is the access
  cut.
- **Leftover Dex state on a `pending` record** after a failed step. It can
  obtain Dex tokens, but openZro refuses them (blocked record, no JIT), and
  the next retry adopts or replaces it.
- **`ListPasswords` is a full scan** on every credential write (D5).
- **Third-party writers to the Dex password DB** are outside the model (D5).
- **Lock lost mid-saga.** Fencing (D9) stops the local writes, but a Dex write
  issued just before a stall can still race with the new holder. The outcome
  is a 500 or 409 and a retry, never access for an unowned `sub`.

## Verification

- **Unit:**
  - the `sub` encoder round-trips through `parseDexSubConnector`;
  - table-driven create, reset, deactivate and delete against a fake Dex
    client and store, with fault injection **at every saga step** (D4, D6,
    D7), asserting `LocalCredentialState`, `LocalCleanupPending` and
    `Blocked`, and that the same request retried converges;
  - deactivate and DELETE return 500 while cleanup is incomplete and succeed
    once it converges; DELETE on a cleaned tombstone → 404;
  - re-activation → 400; create with the email of a `deactivated` or
    `tombstoned` user → 409; create with `active: false` → 400;
  - isolation (D6): `PATCH` with `password` plus any other operation → 400;
    `PATCH` `add`/`remove` on `password` → 400; `PUT` with `password` and an
    unchanged representation → accepted; `PUT` with `password` and any changed
    attribute → 400;
  - no-Dex gating.
- **Ownership (D5):**
  - the same email in two accounts → 409 for the second, and the first
    account's credential still logs in;
  - a `staticPasswords` email → 409;
  - a matching `user_id` on a reservation created in the same request → 409;
  - a reset or delete with mismatched ownership → 500 and no Dex write.
- **Password policy (D8):** exactly 72 bytes accepted; 73 bytes rejected;
  multi-byte UTF-8 at the boundary (24 three-byte characters = 72 bytes
  accepted, 25 rejected); 11 code points rejected; 12 accepted.
- **Peers (D7):** a connected peer is expired and its channel closed on
  `active: false` and on DELETE.
- **HA (D9)**, with the two-replica harness: concurrent create/create (exactly
  one 201 and one 409; the stored hash matches the winner), the same email in
  **two different accounts** on two replicas (one wins, the other gets 409
  and never updates or deletes the winner's credential), reset/delete, and
  deactivate/delete.
- **Terminal state (D3):** `Blocked=false` through the REST user update, and
  through a direct `SaveUser`, is refused for `pending`, `deactivated` and
  `tombstoned` records.
- **Disconnect (D7):** a peer stream held by the other replica is closed on
  deactivate and DELETE, and a retried request closes an already-expired peer
  whose stream is still open.
- **Integration** (testcontainers, real Dex with `enablePasswordDB`):
  - SCIM create with password → password-grant login against Dex → the
    token's `sub` equals the SCIM `id` → `GET /api/users/current` returns the
    pre-set role and groups, and no second user exists;
  - kill between D4 steps 3 and 4 → a login with the new credential is
    refused and no JIT user appears → retry finalizes;
  - a reset revokes existing refresh tokens; DELETE revokes them and the old
    password no longer authenticates at Dex.
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
