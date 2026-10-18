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
- **HTTP API.** The auth middleware checks the cutoff on the user row it
  already reads fresh on every request (`GetUserFromUserAuth`; there is no
  auth cache), so a reset takes effect on the very next request, on every
  replica. A revoked token gets the same generic `401 token invalid` as any
  other invalid JWT, which makes the dashboard's OIDC client end the session.
  The token is never logged. The check runs after the account and JWT-group
  steps that precede the user read; those only re-apply what the token
  already proved at issuance.
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

- **Existing peer.** `handleUserPeer` already reads the user with a share
  lock inside the login transaction, after locking the peer row. It now
  rejects a revoked token there, before `handleExpiredPeer` can clear the
  expiry.
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

A rejected login is `status.Unauthorized`, which the gRPC layer maps to
`PermissionDenied`; the client already treats that as "login needed" and
starts a new SSO flow, which yields a token newer than the cutoff.

### D6 — License posture

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
- Auth middleware: a JWT issued before or in the cutoff second, or without
  `iat`, gets 401 and never reaches the handler; a later JWT, a user without
  a cutoff, and a PAT of a user with a cutoff pass.
- Manager: a reset sets the cutoff, login-expires and disconnects the user's
  peers, and leaves the user active and unblocked; a failed refresh-token
  revocation leaves no cutoff and a retry converges; existing reset and
  lifecycle tests are unchanged.
- Peer login: an old JWT cannot re-login an expired peer nor register a new
  one; a JWT without `iat` fails for a user with a cutoff; a JWT issued after
  the reset succeeds; users without a cutoff are unaffected. The gRPC token
  path returns the `iat`.
- Real Dex (`TestLocalUserLifecycle_RealDex`): an access token minted by Dex
  before the reset, validated by openZro's validator and extractor, is
  revoked by the cutoff; one minted with the new password afterwards is not.
  Refresh tokens are still checked through Dex's API, not redeemed (Dex
  memory storage deadlock, see ADR-0024).
