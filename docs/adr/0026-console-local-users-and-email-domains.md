# ADR-0026 — Console management of local users and the email-domain allowlist

## Status

**Proposed.** Extends [ADR-0024](./0024-scim-provisioned-local-users.md)
(SCIM-provisioned local users) and [ADR-0025](./0025-allowed-email-domains.md)
(email-domain allowlist), which left a REST and dashboard surface out of
scope.

## Context

ADR-0024 v1 manages local users only over SCIM, for an identity-governance
system. ADR-0025 exposes `allowed_email_domains` only on the accounts API.
Operators also need to do both from the openZro console: an administrator
creates or resets a local user without an IGA, deactivates or deletes one,
and maintains the allowlist from the settings page.

Everything the console needs already exists as reviewed sagas in the
management server: fail-closed creation (`SCIMCreateLocalUser`), the
isolated password reset (`SCIMResetLocalPassword`), and the terminal
deactivation/delete with converging cleanup (`terminateLocalUser`, already
reached by the REST `DELETE /api/users/{id}`). The REST list, however,
shows every user record, including pending reservations and tombstones,
and it does not say which users are local.

## Decision

### D1 — REST endpoints reuse the ADR-0024 sagas unchanged

| Endpoint | Behavior |
|---|---|
| `POST /api/users/local` `{email, name, password, auto_groups}` | The ADR-0024 create saga, with the same validation, allowlist, policy and opt-in gate. Returns the user. |
| `POST /api/users/{id}/password` `{password}` | The isolated reset (D6): ownership check, then revoke refresh tokens. |
| `POST /api/users/{id}/deactivate` | The terminal deactivation (D7). It is permanent. |
| `DELETE /api/users/{id}` | Unchanged: tombstones a local user (D7). |
| `GET /api/users/local/config` | `{creation_enabled}`, so the console can show or hide the create action. |

No saga logic is duplicated: the handlers translate the body and call the
same manager methods, so locks, fencing, ownership checks and error mapping
are identical to SCIM. Permissions are the same Users-module checks the
sagas already perform. Creation stays behind `OPENZRO_SCIM_LOCAL_USERS_ENABLED`
(which gates creation only, ADR-0024 D10) and Dex on Postgres.

### D2 — The REST user model says which users are local

`User.local_credential_state` (`active` / `deactivated`; omitted for other
users) lets the console show a badge and choose actions. Pending
reservations and tombstones are not users from an operator's point of view,
so `GET /api/users` omits them, matching their SCIM visibility (ADR-0024 D3).

### D3 — Console behavior

- **Team → Users:** a *Create local user* action (only when
  `creation_enabled`), and on a local user's row *Reset password* and
  *Deactivate*. Deactivate and Delete use a confirmation that states the
  action is permanent. The block toggle is not offered for local users: the
  store refuses it (ADR-0024 D3), and deactivation is the terminal path.
- **Settings → Authentication:** an *Allowed email domains* card that edits
  `allowed_email_domains`, with the ADR-0025 normalization and validation
  performed server-side.

### D4 — License posture

openZro-original. No upstream dashboard or management code from after the
relicense is consulted.

## Verification

- Handler tests for each endpoint: request mapping, error mapping, and that
  hidden users are omitted from the list and `local_credential_state` is
  present.
- Dashboard: Cypress specs for create, reset, deactivate and the allowlist
  card, plus a manual run against the local dev stack (`make dev.dashboard`).
