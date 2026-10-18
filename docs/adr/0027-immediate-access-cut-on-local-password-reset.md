# ADR-0027 — Immediate access cut on a local password reset

## Status

**Proposed.** Supersedes the "What a reset does not do" paragraph of
[ADR-0024](./0024-scim-provisioned-local-users.md) D6, and with it the
description of `POST /api/users/{id}/password` in
[ADR-0026](./0026-console-local-users-and-email-domains.md) D1. The rest of
both ADRs stands.

## Context

ADR-0024 D6 defined the local password reset as: ownership check, Dex
`UpdatePassword`, revoke the user's Dex refresh tokens. It stated what the
reset leaves alone: access tokens already issued stay valid until their TTL
(24 hours with Dex defaults), and connected peers keep their sessions. To
cut access immediately the operator had to deactivate the user, which is
terminal (ADR-0024 D7): the account cannot be re-activated, and its email
cannot be provisioned again.

That makes the most common response to a leaked or shared password, "reset
it and kick everyone out", impossible without destroying the user. Three
gaps let an attacker who held the old password keep access after a reset:

1. An access token minted before the reset keeps passing the HTTP API's JWT
   check until it expires.
2. The user's peers stay connected; the reset never touches them.
3. A peer that is logged out can log back in with any still-valid JWT:
   `handleExpiredPeer` clears the expiry whenever a JWT is presented, and the
   gRPC token validation dropped every claim except the user ID.

Two constraints shape the fix. JWTs are self-contained, so revoking one
means comparing it with server-side state on every use. And the `users` row
is rewritten wholesale by several generic paths (`SaveUser`, `SaveUsers`,
`SaveUserLastLogin`, `SaveAccount`) from structs read earlier, mostly
without a lock, so any new per-user state written by the reset would be
rolled back by the next unrelated save.

## Decision

### D1 — A reset cuts existing access without blocking the user

Once a reset (SCIM or console; both call `SCIMResetLocalPassword`) returns
success:

- every JWT of that user issued at or before the reset is rejected on the
  HTTP API and on peer login;
- every peer the user added through SSO is login-expired and disconnected,
  so the device has to authenticate again through SSO, which needs the new
  password.

The user is **not** blocked and its local credential state does not change.
It signs in again with the new password and its devices reconnect after the
SSO flow. Peers added with a setup key carry no user and are not touched.

One exception is bounded rather than immediate: a login that passed the old
password just before the reset and finishes after it. D6 cuts it within
about half an hour.

### D2 — A per-user credentials cutoff with a single writer

`users.credentials_valid_after` (`types.User.CredentialsValidAfter`, a
nullable timestamp) holds the cutoff. NULL means no cutoff, which is every
user that never had a reset, so nothing changes for them. It is not exposed
through the REST or SCIM API.

The column has exactly one writer, `Store.SetUserCredentialsValidAfter`:

- a targeted `UPDATE users SET credentials_valid_after = ? WHERE account_id = ?
  AND id = ? AND (credentials_valid_after IS NULL OR credentials_valid_after < ?)`,
  so it is **monotonic**: a retry or a late, slower replica never moves it
  backwards;
- the value is stored in UTC, truncated to the second (D3);
- NotFound when the account has no such user; a no-op when the stored
  cutoff is already at or past the new one;
- it runs under the per-account mutex row (`account_locks`, ADR-0024 D3).

The generic saves cannot clobber it: `SaveUser`, `SaveUsers` and
`SaveUserLastLogin` omit the column, and `SaveAccount`, which deletes and
re-inserts every user of the account from its snapshot, copies the
persisted value into the snapshot inside its transaction after taking the
same account mutex. A save built from a struct read before the reset, or
one that carries an older value, therefore leaves the cutoff intact. A
regression test exercises each path with a stale struct on every engine.

### D3 — Cutoff semantics

A JWT is revoked when the user has a cutoff and `iat <= cutoff`, both at
whole-second precision (`types.User.JWTRevoked`).

- **Granularity.** JWT `iat` is whole seconds, so the cutoff is stored
  truncated to the second. The comparison is deliberately `<=`: a token
  minted in the same second as the reset, *before* it, is rejected. The
  cost is that a sign-in completed in that same second, *after* the reset,
  is rejected once too, and the user signs in again a moment later. The
  alternative (`<`) would let a token minted with the old password in the
  reset's own second live until it expires, including one minted by a
  refresh that was in flight while the reset revoked refresh tokens (D4).
  We take one retried sign-in over a window of up to a full token lifetime.
- **No `iat`.** A token without `iat` (or with a malformed one) is rejected
  only for a user that has a cutoff: fail closed where it matters, no change
  for everyone else. Dex always sets `iat`.
- **Clocks.** `iat` comes from the IdP's clock and the cutoff from the
  management server's. They must be NTP-synchronized, like every other
  time-based check on tokens. If the IdP lags, the user's first sign-ins
  after the reset fail until its clock passes the cutoff (fail closed). If
  it leads, tokens minted up to the skew before the reset survive.
- **Token validation.** `auth.Manager.ValidateAndParseToken` reads the
  user row (there is no auth cache) and fails for a JWT revoked by the
  cutoff. Every caller therefore rejects it before acting on it: the HTTP
  middleware before bootstrapping the account or syncing JWT groups, and the
  gRPC server before resolving a peer login. A reset takes effect on the
  very next request, on every replica. A user that does not exist yet has
  no cutoff, so just-in-time provisioning is unchanged.
- **HTTP API.** A revoked token gets `401 token expired`, not the generic
  `401 token invalid` of a malformed or forged JWT. The dashboard signs the
  user in again on `token expired`, while on `token invalid` it only shows
  an error and leaves the page on a spinner; a manual run found exactly
  that before this choice. For the user, a reset ends the session the same
  way an expiry does. The token is never logged.
- **Personal access tokens are out of scope.** They take a separate
  middleware path and are not derived from the password, and service
  integrations rely on them surviving routine credential rotation. After a
  reset, nobody can mint a new PAT for the user without a session the cutoff
  did not revoke. A PAT the user (or someone holding the old password) had
  already created does survive the reset: only users with a role allowed to
  create PATs can have one, and when that account is suspected compromised,
  the operator deletes its PATs or deactivates it (ADR-0024 D7 deletes
  them).

### D4 — Reset order: password, refresh tokens, cutoff, peers, event

Under the local identity lock (ADR-0024 D9), after the existing ownership
preflight:

1. **Dex `UpdatePassword`.** First, so no session that starts after the
   cutoff can be opened with the old password. If the cutoff came first, a
   sign-in with the old password between the two steps would produce a
   token newer than the cutoff.
2. **Revoke the user's Dex refresh tokens.** Before the cutoff: Dex mints a
   fresh access token on refresh without asking for the password, so a
   refresh redeemed after the cutoff but before revocation would yield a
   token that postdates the cutoff and survives. After revocation, the only
   way to a new token is the new password.
3. **Set the cutoff** to the current time (D2). Every JWT issued so far is
   now rejected.
4. **Expire and disconnect the user's SSO peers**, through the same
   `expireAndUpdatePeers` path SCIM deprovisioning uses (extracted as
   `expireUserPeers`), including the re-sent disconnect for peers that were
   already expired. After the cutoff, so a device cannot log back in with a
   pre-reset JWT once it is logged out (D5).
5. **Activity event** (`UserLocalPasswordReset`), unchanged.

Every step is idempotent. A failure returns an error and leaves the user in
a state a retry of the same request converges from: the password may be old
or new (as in ADR-0024 D6), the cutoff only moves forward, and the peer
expiry re-sends its disconnects. No step needs the account write lock
(`AcquireWriteLockByUID(accountID)`), which the local-user lock discipline
forbids under the identity lock; `expireUserPeers` is the same code the
terminal cleanup already runs there.

### D5 — Peer login carries the JWT's iat and checks it in its transaction

`validateToken` returns the token's `iat` with the user ID, and
`types.PeerLogin.JWTIssuedAt` carries it to `LoginPeer`:

- **Existing peer.** `handleUserPeer` reads the user `FOR UPDATE` inside
  the login transaction, after locking the peer row, and rejects a revoked
  token there, before `handleExpiredPeer` can clear the expiry. It is an
  update lock, not a share lock, because re-logging an expired peer writes
  the user's last login to the same row: upgrading a share lock while a
  reset queues for the row is the classic upgrade deadlock.
- **New peer.** The registration transaction reads the user `FOR UPDATE`
  (the same transaction later writes the user's last login, so a share lock
  would invite an upgrade deadlock) and rejects a revoked token before
  inserting the peer.

Holding the user row in the login transaction orders it against a
concurrent reset: the reset's cutoff `UPDATE` waits for a login that read
the user first to commit, and the reset's peer expiry, which runs after the
cutoff, then sees that login's result and expires it again. A login that
reads the user after the cutoff committed is rejected. On SQLite the store
runs on a single connection, which serializes the two.

A revoked token normally fails earlier, in token validation (D3), which the
gRPC layer maps to `PermissionDenied`; so does the in-transaction check. The
client's reconnect loop treats `PermissionDenied` as "login needed" and
starts a new SSO flow, which yields a token newer than the cutoff. A store
failure while reading the cutoff is `Internal` (HTTP 500), which the peer
login retries, never a bad-token answer.

### D6 — Logins in flight: a bounded second pass

The cutoff compares a token's `iat`, the moment Dex **issues** it, not the
moment it **checked the password**. In the authorization-code flow, which
both the dashboard and the client use, those are minutes apart: Dex
(v2.45-v2.46) issues the code right after the password check, keeps it valid
for 30 minutes (hard-coded), and stamps the tokens with the time the code is
redeemed. A login that passed the old password just before a reset can
therefore redeem its code after the reset and get an access token newer than
the cutoff, and a refresh token the reset's revocation never saw. Dex has no
API to revoke pending codes. Dex v2.46 emits `auth_time`, which would be the
right field to compare, but tokens minted by a refresh carry it only with
Dex's experimental sessions feature, so it does not close the refresh path
on its own.

Each reset therefore enqueues its own passes in a `local_credential_rechecks`
table, one row per pass, so overlapping resets keep every deadline:

- **Sweep**, at reset time + 31 minutes (the code lifetime plus a minute of
  margin). If the user is still active, it cuts access exactly as the reset
  did: it revokes refresh tokens, moves the cutoff to the moment it runs
  (after the revocation, so a refresh in between cannot survive), and
  expires the user's peers.
- **Fence**, one minute after the sweep. The sweep has the same race as the
  reset: Dex may be signing a token, at that moment, for a request that
  passed its checks before the revocation. Such a request finishes within
  the minute, and none authenticated with the old password can start any
  more, since every old code has expired. The fence cuts access once more
  and enqueues nothing, which closes the window.

A loop on every replica (`OPENZRO_RESET_RECHECK_INTERVAL_SECONDS`, default
60 s) loads due passes in pages of 50, oldest first, until none is left. A
pass takes the user's local identity lock and re-reads its own row: one
another replica already ran is gone and does nothing, so a pass runs once
however many replicas loaded it. A user deactivated or deleted in the
meantime was already cut by that transition; its passes are discarded.

- **Guarantee.** Every session that existed when the reset returned is cut
  at once. A session minted from a login already in flight is cut within
  about 32 minutes plus one loop interval.
- **Cost.** Anyone who signs in during those 32 minutes, including the user
  with the new password, is signed out once more by the sweep, and again
  if they sign in during the fence's minute; devices re-authenticate.
  Resets are rare, so this is acceptable against an unbounded window.
- **Deactivation** remains the operation that cuts every path at once,
  including logins in flight: it blocks the user, which every request and
  peer login checks.
- **Later.** Comparing Dex's `auth_time` (v2.46+) instead of `iat` would
  close the window without extra passes, once refresh-minted tokens carry it.

### D7 — License posture

openZro-original. No upstream code from the AGPL-licensed directories was
consulted.

## Verification

- Store (`credentials_cutoff_test.go`), on SQLite, Postgres and MySQL:
  targeted set, sub-second truncation, monotonicity, NotFound inside and
  across accounts, and the stale-save regression for `SaveUser`,
  `SaveUsers`, `SaveUserLastLogin` and `SaveAccount`, including structs that
  carry an older cutoff.
- `types.User.JWTRevoked` table test: before, same second, next second, no
  `iat`, no cutoff. `Copy` keeps the cutoff without aliasing it.
- Claims extractor: `iat` surfaced from numeric and `json.Number` claims;
  absent or malformed leaves it zero.
- Token validation against a real store: a JWT issued before or in the
  cutoff second fails; a later one passes; a user that does not exist yet
  passes.
- Auth middleware: a revoked JWT gets 401 and never reaches the handler,
  the account bootstrap or the JWT-group sync; a later JWT, a user without
  a cutoff, and a PAT of a user with a cutoff pass.
- Manager: a reset sets the cutoff, login-expires and disconnects the user's
  peers, and leaves the user active and unblocked; a failed refresh-token
  revocation leaves no cutoff and a retry converges; existing reset and
  lifecycle tests are unchanged.
- Peer login: an old JWT cannot re-login an expired peer nor register a new
  one; a JWT without `iat` fails for a user with a cutoff; a JWT issued after
  the reset succeeds; users without a cutoff are unaffected. The gRPC token
  path returns the `iat`.
- Classification: a revoked JWT is gRPC `PermissionDenied` (one attempt), a
  store failure is `Internal` (retried) and HTTP 500, a bad signature stays
  `InvalidArgument` / 401.
- Concurrency (Postgres, MySQL): an expired peer's re-login paused while it
  holds the user row, a reset that cannot finish meanwhile, then both
  converge and the peer ends expired.
- Passes: a reset enqueues its sweep; the sweep cuts a token minted by a
  login in flight during the reset, revokes refresh tokens again, expires
  the peers, keeps the user active and enqueues one fence; the fence cuts a
  token minted while the sweep ran. Overlapping resets keep both deadlines.
  Three workers running the same pass revoke once and enqueue one fence.
  A deactivated user's passes are only discarded. A backlog is worked
  through in bounded pages. Queue store operations run on SQLite, Postgres
  and MySQL.
- Real Dex, in-flight login: the authorization-code login reaches the code
  with the old password, the password is reset, the code is redeemed
  afterwards; that token passes the reset's cutoff and the sweep cuts it
  and its refresh token.
- `SaveUserLastLogin` writes `last_login` only: a block committed right
  before its statement survives it.
- Real Dex (`TestLocalUserLifecycle_RealDex`): an access token minted by Dex
  before the reset, validated by openZro's validator and extractor, is
  revoked by the cutoff; one minted with the new password afterwards is not.
  Refresh tokens are still checked through Dex's API, not redeemed (Dex
  memory storage deadlock, see ADR-0024).
