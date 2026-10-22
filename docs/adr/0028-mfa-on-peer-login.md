# ADR-0028 — Second factor on peer login

## Status

**Accepted.** Implemented in openzro#223, #225, #227, #228, #229, #230,
#231, #232 and #235.

## Context

With "Require 2FA for local accounts" on, openZro's TOTP protects only the
dashboard and the HTTP API. Registering a device takes a different path, the
gRPC `Login`:

- `validateToken` passes on only the user ID and `iat` of the Dex JWT;
- `LoginPeer` registers the peer, or re-activates a login-expired one.

That JWT comes from Dex after email and password alone. So for a local user, a
leaked password is enough to bring up a tunnel, however the 2FA settings are
set. A security review of the account sign-up process reported this. It was
then confirmed in the code.

The local users at risk are mostly third parties: contractors and partners on
machines openZro's operators do not manage, so MDM or device posture cannot
help.

Dex v2.46 ships its own MFA, but it is experimental (behind
`DEX_SESSIONS_ENABLED`). A review found issues that were reported privately
to the Dex maintainers. openZro does not depend on it, and it is not
considered further here.

This ADR records how the second factor is added to peer login using openZro's
own TOTP (the `user_mfa` layer behind the dashboard challenge). It also
records the operational rules that make it safe to switch on.

## Decision

### D1 — The device login asks for the TOTP through an approval page

A peer login that needs a second factor is refused with a structured error
carrying an approval link. The client opens the link in the browser: the
dashboard page `/mfa/peer-login`, which needs no dashboard session. The page
shows the device and asks for the authenticator code. The daemon retries the
login with jitter and backoff, and it succeeds once the attempt is approved.

The second factor is required:

- when a user registers a **new** peer;
- when a user **re-activates a login-expired** peer.

A background reconnect of a valid peer (no JWT) is not asked. With peer login
expiration on, the factor is therefore asked again every period.

Supported clients: the daemon, the desktop app and the CLI on Linux, Windows
and macOS. Mobile is out of scope for now (see D10).

### D2 — Where the gate runs

The gate runs in the `LoginPeer` paths. That way an approval is consumed in
the same transaction that creates or re-activates the peer:

- `addPeer`, for user-owned peers;
- `handleUserPeer`, only when the peer's login has expired.

`validateToken` passes the token's connector ID on, in a new
`PeerLogin.ConnectorID` field.

The factor is required when both of these hold:

- `resolveMFAEnforcement(connectorID, settings)` says so. That is the rule the
  dashboard gate already uses: connector `local` follows `MFAEnforceLocal`,
  any other follows `MFAEnforceFederated`.
- The new account setting `MFAEnforcePeerLogin` is on. It defaults to false,
  and a `PUT` that omits it keeps the stored value.

If the MFA subsystem is unavailable (no key, no signer) while the factor is
required, the login fails closed with `Internal`, as `MFAGateForRequest`
does.

### D3 — Approval records

The table is `peer_login_mfa_approvals`, in the main store.

**Fields:**
- ID, account, user, the peer's WireGuard key;
- a display snapshot: hostname, OS, observed IP, geolocation, and whether the
  peer is new;
- created, expires, deadline, approved, consumed and denied times.

**Lifetime.** A record has two times, both fixed at creation:

- `expires_at`, initially creation + 5 minutes, which may move later;
- `deadline`, creation + 7 minutes, which never moves.

`expires_at` never passes `deadline`, and the approval token (D4) expires
exactly at `deadline`. So the token is valid for as long as the record can
be.

**Key.** A record is keyed by (account, user, WireGuard key). This is sound
because the `Login` body is encrypted to the peer's key: only the holder of
that private key can present it.

**Indexes:**
- `(account_id, user_id, wg_pub_key, expires_at)` for the per-key lookup;
- `(user_id, expires_at)` for the per-user cap and the earliest-expiry query
  behind `retry_after`;
- `(expires_at)` for the batched sweep, so no replica scans the whole table.

**Creation** runs in its own transaction, after the login transaction has
rolled back on `errPeerLoginMFAPending`. It locks the user row, then:

- reuses a pending record for the same key;
- returns `MFA_DENIED` if that key has a denied record that has not expired;
- enforces at most **5 unexpired records per user**, in any state, including
  denied and consumed ones;
- inserts the record.

**Consumption** happens in the login transaction, with one conditional
update. It counts only when exactly one row changed:

```sql
UPDATE ... SET consumed_at = now
WHERE id = ? AND account_id = ? AND user_id = ? AND wg_pub_key = ?
  AND approved_at IS NOT NULL AND denied_at IS NULL
  AND consumed_at IS NULL AND expires_at > now
```

**States.** Only three transitions are ever written, each as a conditional
update:

- `pending → approved`
- `pending → denied`
- `approved → consumed`

`denied` and `consumed` are final. "Expired" is **derived** from
`expires_at <= now`, never written, and a sweep deletes old records in
batches.

- Approval sets `expires_at = min(max(expires_at, now + 2 min), deadline)`.
  Approval is accepted only while the record is unexpired, so before
  creation + 5 minutes. The extension therefore always gets its 2 minutes,
  and the cap is never cut short. Boundary tests:
  - approval just before the initial expiry;
  - a retry of that approval after its response was lost, near the end of
    the extension.
- Approving an approved, unconsumed, unexpired record again returns 200
  without a new code check, since the first response may have been lost.
- Approving a consumed, denied or expired record returns 412.

**Lock order:** `users` row → `peer_login_mfa_approvals` → `user_mfa`. This
order applies to creation, approval, denial, and the 2FA reset, password
reset and user deletion paths that clear a user's records. The login keeps its
existing order (`users` before the approval row). When approval races a reset
or a deletion, one side loses cleanly.

**Serialization with settings saves.** A login decides from the account
settings: whether its login expired, whether the gate applies (D2), and
whether a new device needs peer approval (D9). A save that switches any of
these on must not be undone by a login that read the settings before it. So
once the save returns, no login decided under the old settings may still
commit.

The account row, which holds the settings, cannot order this:
- `SaveAccount` deletes and re-inserts it;
- many writers lock it after groups or users.

Any placement of a login lock on that row inverts some existing order.

The boundary is a second table, `peer_login_gates`, with one row per account.
The row is created on first lock and deleted with the account. Only two kinds
of transaction take it, each as its first lock:

- **A registration**, by a user or a setup key, or **a login with a JWT**,
  takes it with a share lock.
- **`UpdateAccountSettings`** takes it with an exclusive lock.

After the share lock, the login reads the settings without locking the
account row, and every decision in that transaction uses that read:

- expiration;
- peer approval (`PreparePeer`, with the auto groups of the user it locked);
- the gate.

A save therefore either commits before that read, or waits for the login to
commit and returns after it. Logins share the row, so they do not wait on
each other. Nothing else takes the row, so it adds no edge to the existing
lock order. The row is looked up before it is inserted: on MySQL, an upsert of
an existing row would lock it exclusively on every login.

### D4 — The approval token and the page

The existing MFA token signer gains the purpose `mfa_peer_login`. The token is
bound to the approval ID and the user. It expires at the record's `deadline`
(D3), never earlier. So the idempotent approval retry works for the whole time
the record can be valid. When a login reuses a pending record, the client gets
a new token with that record's `deadline`.

The token travels only in the URL **fragment** (`#t=`), never in the query.
No server, daemon or CLI log contains it, and neither does the typed error's
`Error()` string. Tests enforce this.

The page and its endpoints exist to approve or refuse one login:

- `GET /api/mfa/peer-login`, `POST /api/mfa/peer-login/approve {code}` and
  `POST /api/mfa/peer-login/deny`. All three bypass the JWT middleware and
  send `Cache-Control: no-store` and `Referrer-Policy: no-referrer`.
- The page reads the token from the fragment, then clears it from the
  address bar.
- It shows:
  - the hostname and OS, labelled as reported by the device;
  - a short fingerprint of the WireGuard key;
  - the IP the server observed, with its location;
  - the time of the attempt;
  - the account, and the user's email partly masked (`a***@example.com`),
    since anyone holding the link can open the page.
- It takes a TOTP or a backup code, and has a **"Not me"** button that denies
  the attempt.

**Guessing the code.** Someone with a stolen password can legitimately obtain
an approval token. The page would then be an online oracle for a six-digit
code, and the per-user record cap does not limit guesses against a single
record. So `approve` checks the code exactly as the dashboard challenge does,
with the same shared verifier:

- **One transaction.** It runs with the `user_mfa` row locked (D7), so
  concurrent attempts on any replica are serialized.
- **The same lockout.** It uses the user's `failed_attempts` and
  `locked_until`: 5 consecutive failures lock the user for 15 minutes, for
  the dashboard challenge and the approval page alike.
- **Single-use backup codes.** A matching backup code is removed in the same
  transaction.
- **When locked.** `approve` returns **423 Locked** with `locked_until`,
  checks no code, and counts no attempt. The page says when to try again.

The lockout can be triggered only by someone who already holds the user's
password, which is the same exposure the dashboard challenge has today.

A test runs concurrent wrong attempts across two replicas. The failures must
be counted exactly, the lock must engage at the fifth, and no attempt may be
verified while locked.

### D5 — Errors returned to the client

The errors are built in `Login`, before the generic error mapping, as gRPC
statuses with `errdetails.ErrorInfo`. There are four reasons:

| Reason | gRPC code | Metadata |
|---|---|---|
| `MFA_REQUIRED` | `FailedPrecondition` | `dashboard_base_url`, `approval_path`, `approval_token`, `expires_in` |
| `MFA_DENIED` | `FailedPrecondition` | `retry_after` |
| `MFA_ENROLLMENT_REQUIRED` | `FailedPrecondition` | `dashboard_base_url`, `enroll_path` |
| `MFA_RATE_LIMITED` | `ResourceExhausted` | `retry_after` |

- **Building the link.** The client joins `base + path`. Only
  `MFA_REQUIRED` carries a token, which goes in the fragment. No reason
  carries the token twice.
- **No record for two reasons.** `MFA_ENROLLMENT_REQUIRED` and
  `MFA_RATE_LIMITED` create no record.
- **`retry_after`** is deterministic: the time to the earliest unexpired
  `expires_at` among the records holding the limit (or the key's own denied
  record), rounded up to whole seconds and never negative.
- **Stopping.** On `MFA_DENIED` and `MFA_RATE_LIMITED`, the client stops
  retrying and says when to try again.
- **Ending the flow.** On those two and on `MFA_ENROLLMENT_REQUIRED`, the
  daemon ends the flow and drops the JWT immediately.

**Where `dashboard_base_url` comes from.** It is the optional
`HttpConfig.DashboardURL`, overridable by environment. It must be `https://`;
plain `http://` is accepted only for loopback (localhost, 127.0.0.0/8, ::1),
because the page carries a bearer token and a TOTP code. Any other value makes
the management server refuse to start. When it is not set, the client falls
back to its profile's admin URL.

### D6 — Daemon flow state

`WaitSSOLogin` returns an opaque `flow_id` with the approval URL, and
`WaitMFAApproval(flow_id)` requires it. A `flow_id`:

- has at least 128 bits from `crypto/rand`;
- is single-use;
- is bound to the profile, the management URL and the SSO flow that created
  it;
- accepts a single active waiter; a second is refused.

At most 4 flows are pending in the daemon; past that, the oldest is dropped.
The JWT lives only in memory with its flow. It is erased:

- on success, cancellation, replacement or expiry;
- by a periodic reaper, which also covers the UI or CLI going away.

Polling starts at 3 s, with jitter and backoff up to 10 s.

### D7 — TOTP replay protection

`user_mfa.last_used_step` records the time step of the last accepted code.
Codes at or below it are refused (RFC 6238 §5.2), including a future step that
was accepted within the clock-skew window. This applies to the dashboard
challenge and to the approval page.

**The check is atomic.** Verification runs in a transaction that holds the
`user_mfa` row lock (`FOR UPDATE`) from reading `last_used_step` to writing
the accepted step. The same transaction resets `failed_attempts` and, for a
backup code, removes it. That way two replicas, or two pages, cannot both
accept the same code.

**Enrollment counts as a use.** `MFAFinishEnrollment` stores the step of the
code that completed enrollment as `last_used_step`. Otherwise that same code
could pass the dashboard challenge, or approve a peer, within its 30-second
window.

Tests:
- a previous, the same and a future step, at the 30-second boundary;
- the same code submitted concurrently from two replicas, where exactly one
  is accepted;
- the enrollment code refused right after enrollment.

### D8 — Admin visibility and the 2FA reset

- **Visibility.** Admins see each user's 2FA state and lockout in the users
  list. They also see the count of active users without 2FA, split
  local/federated by the same connector rule as D2. The count leaves out
  service users, blocked users, and pending, deactivated or tombstoned local
  users.
- **Reset.** An admin with a verified 2FA session can reset a user's 2FA.
  The reset is **atomic and idempotent**, with **at most one audit event**:
  `user.mfa.reset` for an admin resetting someone else, `user.mfa.disable`
  for users clearing their own. It is one transaction, in D3's lock order:
  1. lock the `users` row;
  2. delete the user's approval records;
  3. delete the `user_mfa` row; whether a row was affected decides the
     audit;
  4. commit, then audit, and only if step 3 removed a row.

  The approval records are cleared whether or not a 2FA row existed. A
  failure at any step rolls back both deletes, so a retry does the whole
  reset again, and an approved record can never outlive the reset. Of
  concurrent resets on any number of replicas, exactly one removes the row
  and audits it. Resetting a user without 2FA changes no 2FA and is not
  audited. Tests cover concurrent resets and a failure between the two
  deletes followed by a retry.

  Until the approval table exists, the reset is the single conditional
  `DELETE` of step 3 (openzro#225).
- **Audit on the approval page.** A denial through the link is recorded with
  the **system** initiator and a "denied through the approval link" note. It
  is not attributed to the user: holding the link proves the user's TOTP,
  not their session. Required, approved and failed attempts are recorded the
  same way.

### D9 — Peer approval stays on for third parties

Peer approval (the `IntegratedValidator` check) can exempt groups; the API
field is `peer_approval_exempt_groups`. With internal staff in exempt groups,
every new device of a third party waits for an admin. It is part of this
design, not a temporary measure, because 2FA enrollment is self-service:

- A third party created, or whose 2FA was reset, after activation has no 2FA
  yet. At their next login they get `MFA_ENROLLMENT_REQUIRED` and set up 2FA
  in the dashboard. Someone holding only their password could do the same
  and approve their own device. Peer approval is what stops that.
- **Accepted risk.** Peer approval protects only users outside the exempt
  groups. An internal user who is created or reset after activation can
  still enroll a TOTP with their password alone and register a device
  without approval. Third parties are the scope of this ADR; this residual
  risk for exempt users is accepted, not solved.

Removing a group from the exempt list does not un-approve peers it had
already let through.

### D10 — Activation order (mandatory)

`MFAEnforcePeerLogin` may be switched on only in this order:

1. **Peer approval** on, with internal groups exempt (D9).
2. **No subject user without 2FA.** The management server enforces this.
   - The account settings `PUT` returns **412** when the resulting state has
     `MFAEnforcePeerLogin` on and some active user subject to the 2FA rule
     has no 2FA. The check runs when `MFAEnforcePeerLogin` becomes true, when
     `MFAEnforceLocal` or `MFAEnforceFederated` becomes true with the gate
     already on, and when several of these change in one `PUT`.
   - The check uses the same count as D8. It is an activation guardrail, not
     a permanent invariant: later user creation and 2FA resets stay allowed,
     and D9 covers them.
3. **Every management replica upgraded.** An older replica would accept the
   login without the second factor.
4. **The third parties' desktop and CLI clients upgraded.** Accounts with
   mobile users do not switch the setting on until mobile is supported: the
   gate is server-side and would lock mobile out.
   - There is no "client supports it" flag, since a client could forge it.
   - With the setting on, older clients are unsupported. They get a generic
     `FailedPrecondition` ("2FA approval required; update openZro") with no
     link in the message, so no token ends up in their logs.
5. Only then, switch `MFAEnforcePeerLogin` on.

To roll back, switch the setting **off before** bringing an older replica
back.

### D11 — Consent phishing

An attacker with a stolen password could send the victim an approval link and
hope they approve it. The design narrows this:

- the page states what is being approved: a device login, with the device's
  reported name and OS, the observed IP and location, and the time;
- the "Not me" button denies the attempt, and the key stays denied until the
  record expires;
- a record can be approved only within 5 minutes of the attempt, and neither
  it nor its token outlives the 7-minute deadline;
- per-user creation is capped (D3);
- the page and its endpoints send no referrer.

Peer approval (D9) remains the backstop for third parties.

### D12 — Observability

Through the existing OpenTelemetry metrics:

- the count of polls;
- the time to approval;
- the count of expired and denied approvals;
- errors.

Metric attributes stay bounded: outcome, reason, and endpoint. No account,
user or peer identifiers go on metrics, since they would grow without bound
as accounts accumulate. When the account matters, the audit events, logs
and traces carry it.

Only the replica whose sweep `DELETE` removed a record emits its expiry event
and metric.

## Consequences

- A leaked third-party password no longer brings up a tunnel by itself. With
  the gate on:
  - a **new** third-party device needs **both** the user's TOTP (D2) **and**
    an admin's peer approval (D9);
  - **re-activating** a login-expired peer that was already approved needs
    the user's TOTP.

  That is why self-service enrollment after a reset is acceptable for third
  parties. Someone holding only the password can enroll a TOTP, but the new
  device still waits for an admin.
- Device registration gains a browser step and up to a few seconds of
  polling.
- Old clients and mobile cannot log in to accounts that switch the setting
  on. D10 makes that an explicit operator decision.
- Two new tables and one new column (`last_used_step`):
  - the approval records, swept in batches;
  - `peer_login_gates`, one lock row per account.

  Every registration and every login with a JWT takes a share lock on that
  row first, and a settings save waits for the logins in flight.
- The approval page is reachable by anyone who holds a link. Its exposure is
  bounded by:
  - the token's binding to one record and one user;
  - the 5-minute approval window, and the 7-minute deadline on the token and
    the record;
  - the per-user cap;
  - the masked email and no-referrer.

## Alternatives considered

- **Dex's built-in MFA.** Rejected: experimental, and not ready to depend on
  (see Context).
- **Prompting for the code in the client itself.** Rejected: every client
  would carry the TOTP UI and handle the code. The approval page keeps the
  code in one place, shared with the dashboard challenge.
- **A "client supports MFA" flag in the login request.** Rejected: a client
  can claim it, so it cannot gate anything. Server-side enforcement plus the
  activation order is used instead.
- **Requiring the factor on every reconnect.** Rejected: it would interrupt
  background reconnects for no gain over login expiration, which already
  bounds the session.

## Implementation

The work lands as separate PRs. Nothing changes for peer login until the
gate (item 8) lands and an operator switches it on; item 4 also tightens the
existing dashboard challenge.

1. **Peer approval exemptions** (openzro#223).
2. **Admin visibility and the 2FA reset** (openzro#225).
3. **This ADR.**
4. **TOTP replay protection** (D7).
5. **Server base:** the table, the endpoints, the sweep and `DashboardURL`
   (D3, D4).
6. **The dashboard page** (D4).
7. **Clients:** the daemon, the CLI and the desktop app (D5, D6).
8. **The gate, the setting and the activation check** (D2, D10, D12).
