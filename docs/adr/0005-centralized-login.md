# ADR-0005: openZro-branded centralized login

- **Status**: Superseded by [ADR-0006 — Embed Dex as openZro's federated IdP](./0006-embed-dex.md)
- **Date**: 2026-04-28
- **Decision-makers**: openZro maintainers

> **Superseded note (2026-04-28).** The architecture proposed
> here was implemented across 18 commits (PR 1 through PR 8 of
> the original plan plus wiring) but the dashboard SPA never
> integrated with the new `/login` surface — the `@axa-fr/react-oidc`
> shell continued forcing legacy single-IdP redirects. Closing
> that gap required either a 400-line dashboard refactor or
> a 700-line broker implementation on the management server.
> Investigating the upstream NetBird Cloud architecture revealed
> they ship Dex (a CNCF OIDC provider) rather than building a
> broker. ADR-0006 documents the pivot to the same approach.
> This file is preserved unchanged below for historical context;
> the implementation it describes is being retired in stages
> tracked under ADR-0006.

## Context

The openZro management dashboard today delegates the login flow
entirely to whatever OIDC IdP the operator wired up — Zitadel
in the quickstart bundle, but it could be Keycloak, Auth0, Okta,
Authentik, JumpCloud, Microsoft Entra, Google Workspace, or any
OIDC-compliant provider. The user's first contact with openZro is
therefore the IdP's own login UI: Zitadel branding, the IdP's
domain in the URL bar, the IdP's password reset / MFA semantics.

Two consequences:

1. **Brand fragmentation.** Operators who self-host openZro can't
   present a coherent "openZro" experience to their internal users
   — the auth surface looks like Zitadel / Dex / Keycloak. End
   users perceive openZro as "the thing on the other side of the
   Zitadel login", which weakens the project's identity in the
   places (auth) where users spend the most attention.
2. **Single-IdP assumption.** The management server is wired to
   exactly one OIDC issuer at a time. Adding a second IdP (e.g.
   "let employees sign in via Entra and contractors via GitHub
   OAuth") requires standing up a federating IdP in front (Zitadel
   / Keycloak with multiple connectors) and pushing the complexity
   onto the operator.

The NetBird Cloud comparison surfaced these gaps. Their
`login.netbird.io` is a centralized router with social buttons
(Google, GitHub, Microsoft, Entra) and an email-first text input —
end users never see the underlying IdP. We can't replicate that
with the upstream-inherited single-IdP code path, but we can build
the equivalent for self-host.

## Decision

openZro will own the login surface end-to-end. Concretely:

1. **A new `/login` route in the management server**, served from
   the same domain operators already configure for the dashboard.
   The page is openZro-branded — the `openZro` wordmark, violet
   palette, dark / light theme — and shows the list of
   authentication providers the operator has configured.
2. **A new `AuthenticationProvider` resource in the management
   data model.** Operators configure providers through the
   dashboard (and the public API): name, OIDC issuer URL,
   `client_id`, `client_secret`, scopes, optional email-domain
   hint, optional brand label and logo URL. The encrypted-at-rest
   credential envelope is the same one used by `flow_exports` and
   `mdm.Store` (`flowExports.FieldEncrypt`).
3. **Multi-issuer JWT validation in the management auth layer.**
   The current code validates incoming JWTs against a single
   configured issuer; the new code validates against the set of
   trusted issuers derived from the configured `AuthenticationProvider`s
   plus a small set of standing global anchors (account-owner
   bootstrap session). Each provider's JWKs are fetched and
   cached in memory with the same TTL behavior as the existing
   single-issuer cache.
4. **OIDC PKCE flow handled by the management server.** The
   browser hits `/login` → picks a provider → redirects to the
   provider's `authorization_endpoint` → returns to
   `/auth/callback?provider=<id>&code=<code>` → management
   exchanges the code for tokens, validates the ID token,
   provisions the user record (or matches an existing one by
   IdP-`sub`), and issues a short-lived openZro session JWT plus
   a longer-lived refresh token. The dashboard uses those for
   subsequent API calls.
5. **A `/logout` endpoint** that invalidates the openZro session
   and (where the provider supports it) calls the upstream
   end-session endpoint via `id_token_hint`.

Backwards-compatible mode for existing deployments: when no
`AuthenticationProvider` rows exist, the management server falls
back to the single-IdP code path it has today. Operators upgrade
on their own schedule by configuring providers through the new
flow.

## Trade-offs considered

### Alternative 1: Theme the IdP

Zitadel and Keycloak both expose login-template overrides. We
could ship an `openzro-zitadel-theme/` directory and an
`openzro-keycloak-theme/` directory; operators apply them to
their IdP and the login page picks up the openZro brand.

- **Pro**: drastically lower code lift (~3–5 days of HTML/CSS
  per IdP). No security-sensitive changes to the auth path.
- **Pro**: the IdP keeps owning password reset, MFA, account
  management — none of which we want to re-implement.
- **Con**: still IdP-specific. A Keycloak operator gets one
  experience, a Zitadel operator gets another. Brand consistency
  across the openZro user base remains weak.
- **Con**: URL and admin surface still belong to the IdP. Users
  see `auth.openzro.example.com` (Zitadel) instead of
  `openzro.example.com/login`.
- **Con**: Doesn't help with multi-IdP. Operator still needs the
  IdP to federate, which is the original complexity.

### Alternative 2: Frontend-only router

The dashboard rendering layer alone owns the `/login` UI.
Operators configure providers in the frontend's environment and
each provider button initiates a standard OIDC PKCE flow against
that provider directly. Tokens land in the dashboard's
localStorage; backend continues to validate against a single
issuer.

- **Pro**: no backend code change.
- **Con**: backend can only trust tokens from one issuer, so the
  multi-IdP UX is fake — only one provider's tokens actually
  work end-to-end.
- **Con**: frontend OIDC client_secret handling is unsafe (PKCE
  helps but server-side flow is cleaner for confidential clients).

Rejected. Solves only the visual half of the problem.

### Alternative 3: openZro becomes a full OIDC server

Instead of federating to upstream IdPs, openZro becomes its own
OIDC provider end-to-end (own user database, own password store,
own MFA). Upstream IdPs become optional federation sources.

- **Pro**: total control. No federation hop.
- **Con**: ~3 months of work plus ongoing maintenance of
  password hashing, MFA, account recovery, audit log,
  rate-limit, brute-force defense. We'd be reimplementing
  Zitadel / Keycloak.
- **Con**: operators don't want yet another user database to
  manage. Today's deployments use existing IdPs deliberately.

Rejected as scope creep.

### Chosen direction

The chosen path (federation-based, openZro-owned login UI +
multi-issuer validation) sits between alternatives 1 and 3. We
get the brand surface and the multi-IdP UX without taking on
identity-store ownership. The security-sensitive piece is real
but bounded — validating tokens from N trusted issuers is the
same cryptographic surface as validating from one issuer, just
plural.

## Plan

Two waves. Each wave is independently shippable.

### V1 — single-page brand-owned login

The login surface, multi-issuer validation, and per-provider OIDC
flow. Scope:

1. **Schema + storage** (`management/server/auth/providers/`):
   - `AuthenticationProvider` GORM row with the same encrypted
     blob layout as `mdm.ProviderRow`.
   - `Store` with `Save` / `List` / `Get` / `Delete`,
     `Decrypt` for the live OIDC manager.
2. **Provider manager** (`management/server/auth/manager.go`):
   - Reads the `Store`, builds an `oidc.Provider` per row,
     caches JWKs.
   - `Refresh()` re-reads when an admin mutates a provider.
3. **HTTP handlers** (`management/server/http/handlers/auth/`):
   - `GET  /login` — server-rendered HTML with the brand and
     provider list. Uses an embedded Go template; no React build
     step. The dashboard's `/login` route is a thin fallback that
     redirects to the management `/login` URL.
   - `GET  /auth/start?provider=<id>` — initiates PKCE,
     302-redirects to the provider's `authorization_endpoint`.
     State cookie pins `provider`, `code_verifier`, return URL.
   - `GET  /auth/callback?provider=<id>&code=<code>&state=<s>` —
     validates state cookie, exchanges code, validates ID token,
     provisions / matches user, issues openZro session JWT.
   - `POST /auth/logout` — clears session, optional upstream
     end-session.
4. **Multi-issuer JWT validation** (`management/server/jwtclaims/`):
   - Replace single-issuer struct with a `MultiIssuerValidator`
     that holds N `oidc.Provider`s + a fallback to the legacy
     single-issuer config when no providers are configured.
5. **Admin API + dashboard UI** (`dashboard/src/modules/auth/`):
   - `/settings?tab=authentication` page lists configured
     providers, "Add provider" modal with form: name, type
     dropdown (`oidc-generic`, `google`, `github`, `microsoft`,
     `entra`), `client_id`, `client_secret`, scopes, brand label.
6. **Activity log codes** (`management/server/activity/codes.go`):
   - 95 = `authentication.provider.created`
   - 96 = `authentication.provider.updated`
   - 97 = `authentication.provider.deleted`
   - 98 = `authentication.session.granted` (issued at sign-in)
   - 99 = `authentication.session.revoked` (logout)

**Estimate**: 1.5–2 weeks of focused work for one engineer.

**Test surface**:
- Unit tests for the OIDC manager + multi-issuer validator (`oidc-mock`).
- Integration test against a real Zitadel + a real Keycloak (testcontainers).
- E2E Cypress: configure 2 providers, sign in via each, verify
  session cookie, log out.

### V2 — UX polish

- Email-domain routing: type `user@company.com` → "Continue"
  detects domain → button highlights the right provider.
- Custom-branded login (operator uploads their logo / colour for
  their tenant's login page).
- Rate-limit + brute-force defenses on `/auth/start` and
  `/auth/callback`.
- SSO discovery for OAuth-providers that publish enterprise
  metadata.

**Estimate**: 2–3 weeks after V1 lands.

## Consequences

### Brand
The login surface is openZro-branded. URL is the operator's
`openzro.example.com/login`, not the IdP's domain.

### Operator UX
Provider configuration moves from "configure your IdP outside
openZro" to "Settings → Authentication Providers" inside openZro.
Operators can mix providers (Entra for employees + GitHub for
contractors) without standing up a federating IdP first.

### Code complexity
Auth code becomes ~10× larger and security-sensitive. ADR is
mandatory for any future deviation; PRs touching this area
must include a regression test for every OIDC quirk we encounter
(Google's `email_verified` enforcement, GitHub's missing email
in default scope, Entra's `tid` claim handling, etc.).

### Backwards compatibility
No breaking change. Existing deployments without configured
`AuthenticationProvider` rows fall back to the legacy single-IdP
code path indefinitely. Operators can upgrade on their schedule.

### License posture
No new dependencies are needed beyond what `golang.org/x/oauth2`
+ `github.com/coreos/go-oidc` already cover, both Apache-2.0
which is compatible with openZro's BSD-3-Clause core. No license
contamination.

## References

- [OAuth 2.0 PKCE RFC 7636](https://datatracker.ietf.org/doc/html/rfc7636)
- [OpenID Connect Core 1.0](https://openid.net/specs/openid-connect-core-1_0.html)
- [`github.com/coreos/go-oidc`](https://github.com/coreos/go-oidc) — vetted multi-issuer client
- [Zitadel custom login templates](https://zitadel.com/docs/guides/manage/customize/branding) — the alternative we explicitly rejected, kept as fallback for operators who want it
- [ADR-0001: openZro foundation](./0001-openzro-foundation.md) — license posture, why we don't bundle GPL-licensed identity stacks
