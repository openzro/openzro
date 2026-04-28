# ADR-0006: Embed Dex as openZro's federated IdP

- **Status**: Accepted
- **Date**: 2026-04-28
- **Supersedes**: [ADR-0005 — openZro-branded centralized login](./0005-centralized-login.md)
- **Decision-makers**: openZro maintainers

## Context

ADR-0005 set out to give openZro a branded, multi-IdP login surface
end-to-end: schema, OIDC manager, MultiIssuerValidator,
PKCE handlers, /login + /setup wizards, admin REST API, dashboard
tab. We landed ~5,500 lines of Go + TypeScript across 18 commits.
Backend tested and clean.

Two practical findings forced a re-evaluation:

1. **The dashboard SPA never integrated.** The work shipped a
   working `/login` server-rendered surface and a working
   `oz_session` cookie path through the API middleware. It did
   NOT replace `@axa-fr/react-oidc` in the dashboard. Net
   result: a user signing in through `/login` ends up with a
   cookie the API trusts, but the React shell still sees no
   localStorage token, calls `useOidc().login()`, and bounces
   back to the legacy single-IdP flow. The new surface is
   reachable but functionally a dead end for browser users.
   Closing that gap requires either a large refactor of the
   dashboard auth (~400-500 lines, ~10 files) or a complete
   OIDC-broker implementation on the management server (~700
   lines: discovery, JWKs, /oauth/authorize, /oauth/token,
   /oauth/userinfo, /oauth/end_session, RS256 keypair
   management, authcode store).

2. **NetBird v0.62 already solved this — using Dex.** Per the
   [v0.62 announcement](https://netbird.io/knowledge-hub/local-users-simplified-idp)
   and the [DeepWiki architecture overview](https://deepwiki.com/netbirdio/netbird/3.6-identity-provider-integration),
   NetBird embeds [Dex](https://github.com/dexidp/dex) — a
   CNCF-maintained Go OIDC provider, Apache-2.0 licensed — as
   the management's auth front-end. Dex handles the
   user-facing login UI, federates to upstream IdPs through
   its built-in connectors (Google, GitHub, Microsoft, LDAP,
   SAML, OIDC-generic, …), and exposes a single OIDC surface
   to the dashboard SPA. NetBird does NOT operate its own
   broker code; the dashboard talks to Dex like it would talk
   to any OIDC IdP.

Dex is a mature project (10.7k GitHub stars, active, CNCF
sandbox graduate, used in production by Kubernetes auth, Argo
CD, etc.). Its licence is Apache-2.0 — compatible with our
BSD-3-Clause core (see [ADR-0001 §3.1](./0001-openzro-foundation.md)).

We have two viable paths from here:

**Path A** — Finish the broker we started. Implement the missing
~700 lines, ship a self-contained openZro IdP. Pros: zero new
runtime dependencies; total control. Cons: we reinvent code Dex
already battle-tested; we maintain a security-sensitive surface
forever; our connector catalogue is bounded by what we
implement, not what Dex's connector ecosystem covers.

**Path B** — Adopt Dex like NetBird. Drop the broker pieces of
ADR-0005, add Dex to the management bundle, point the dashboard
at it. Pros: federation comes for free with Dex's connector
catalogue; theming via Dex's template system; CNCF-grade
maintenance; ~50 lines of integration glue instead of ~700
lines of broker code; matches the upstream NetBird architecture
without copying any of their AGPL code. Cons: a new runtime
dependency in the management stack; operators have to grok
Dex's `config.yaml` format for the corner cases.

## Decision

**Path B.** openZro will adopt Dex as the federated IdP that
sits in front of the management API.

The architecture becomes:

```
                    Dashboard SPA
                       │ @axa-fr/react-oidc
                       │ (PKCE, authority=https://<openZro>/dex)
                       ▼
                     Dex
                       │ federation via configured connectors
                       ▼
       ┌───────────┬─────────────┬──────────┐
       │           │             │          │
     Google     GitHub        Entra ID    LDAP / SAML / …
```

Concrete changes:

1. **Dex container** ships in the quickstart docker-compose
   bundle alongside management/dashboard/signal/relay/coturn.
   Image: `ghcr.io/dexidp/dex:<pinned-version>` (Apache-2.0).
   Mounted config from `infrastructure_files/dex-config.yaml`,
   templated by `configure.sh` from the operator's `setup.env`.
2. **Reverse-proxy** routes `/dex/*` to the Dex container.
   Both nginx and traefik variants updated. Dex's OIDC
   discovery URL becomes `https://<OPENZRO_DOMAIN>/dex/.well-known/openid-configuration`.
3. **Dashboard config** points `OPENZRO_AUTH_AUTHORITY` at Dex
   (`https://<OPENZRO_DOMAIN>/dex`). One env var change. The
   `@axa-fr/react-oidc` configuration the dashboard already
   uses works unchanged — Dex IS an OIDC IdP from its point of
   view.
4. **Management JWT validation** continues through the existing
   `MultiIssuerValidator`. Dex is registered as the trusted
   issuer (its `issuer` claim equals the discovery URL above).
   For deployments still running with a legacy direct-IdP
   (Zitadel/Keycloak/Auth0/Okta), the validator's fallback path
   accepts those tokens unchanged — same backwards-compat
   posture as ADR-0005's V1 plan.
5. **Greenfield bootstrap** continues to need a starting
   credential. Dex's `staticPasswords` block in `config.yaml`
   gives operators a built-in admin login (email + bcrypt
   password) for the very first sign-in. After they configure
   their preferred connector, they remove the static password.
   This replaces the `/setup` wizard from ADR-0005.

## What we keep from ADR-0005

The work landed under ADR-0005 is not entirely thrown away.
Components that survive:

- **`management/server/auth/jwt/multi_issuer_validator.go`** —
  generic primitive, validates Dex's tokens AND any legacy
  upstream IdP tokens that may still be in use. Stays as the
  default validator path.
- **`management/server/auth/manager.go` change** — `auth.NewManager`
  taking a multi-issuer source stays. The provider registry it
  consults shifts from the `AuthenticationProvider` table to a
  static list of trusted Dex/legacy issuers. Same shape, smaller
  source of truth.

## What we retire from ADR-0005

Components that get reverted (or shrunk to stubs marked
deprecated):

- `management/server/auth/providers/` — `AuthenticationProvider`
  GORM model, Store, OIDC provider Manager. Replaced by Dex's
  `config.yaml` connector blocks.
- `management/server/http/handlers/auth/` — `/login`, `/auth/start`,
  `/auth/callback`, `/auth/logout`, `/setup`, login template,
  setup template, bootstrap token store, state-cookie sealer,
  session service. Dex serves the OIDC surface and its own
  login UI.
- `management/server/http/handlers/auth_providers/` — admin
  REST API CRUD for AuthenticationProvider rows. Operators
  configure providers through Dex's `config.yaml` (file edits
  + restart), not through a dashboard form.
- `dashboard/src/modules/auth-providers/` — the Settings tab.
  Dex doesn't have an admin API; configuration is file-based.
  Dropping the tab altogether is honest; misleading UI is worse
  than no UI.
- `dashboard/src/interfaces/AuthenticationProvider.ts` — wire
  types for the admin API. Removed alongside the tab.
- The cookie bridge in `auth_middleware` — without `/auth/callback`
  minting `oz_session`, no cookie ever exists. Drop the bridge
  branch; middleware reverts to Bearer-header-only as before
  ADR-0005.
- Activity codes 95-99 — `AuthProvider*` codes become dead
  letters. Mark as reserved (don't reuse the IDs) and drop the
  emit calls.

The git history of ADR-0005 commits stays intact for posterity.
The retirement is performed as a series of reverts, not a
history rewrite.

## Trade-offs considered

### Alternative 1: Finish the openZro broker (the 700-line plan)
- Pro: no new dependencies, total control of every byte that
  authenticates an openZro user.
- Pro: closest to ADR-0005's original intent.
- Con: re-implements a security-sensitive protocol that Dex
  does correctly. Vercel-grade attention to JWT signing,
  authcode storage, PKCE verification, key rotation, logout
  semantics — every shortcut bites.
- Con: bounded connector catalogue. Adding LDAP support means
  writing an LDAP client. Dex has it.

### Alternative 2: Stop here, document caveats
- Pro: zero further work, the backend pieces of ADR-0005 stay
  on the shelf as "preview".
- Con: the gap is real and operators will discover it. Better
  to ship a working flow than a bandaged-up "preview".

### Alternative 3: Theme the existing IdP (Zitadel)
- Pro: Zitadel is already in the quickstart bundle.
- Con: rejected by ADR-0005 itself, for reasons that still
  apply — IdP-specific themes, URL still shows the IdP,
  operator who picked Keycloak gets a different experience.
  Dex themes are uniform across all openZro deployments.

### Alternative 4: Adopt Dex (chosen)
- Pro: standard pattern, used by Kubernetes auth, Argo CD,
  NetBird itself.
- Pro: ~50 lines of integration glue.
- Pro: connector ecosystem inherited (LDAP, SAML, OIDC, OAuth2
  + 15 named providers).
- Pro: theming via Dex's template engine — uniform openZro
  brand without per-IdP work.
- Con: new runtime dependency. Mitigated: Dex is single-binary
  Go, ~30MB, configured by one YAML file, no DB requirement
  (uses memory or kubernetes/postgres optionally). The
  operational footprint is small.

## Plan

Five sequential stages, each independently committable:

### Stage 1 — Dev stack swap (small, isolated)
- `deploy/dev-idp.compose.yml`: replace Zitadel with Dex.
- `deploy/dev-idp/dex.config.yaml.tmpl`: Dex config seeded
  with one static password admin user for first sign-in
  (configurable email; default `admin@openzro.dev`).
- `deploy/dev-idp/provision.sh`: rewritten — much simpler
  for Dex (just template the YAML and start the container).
- `deploy/dev-mgmt/management.json.tmpl`: point the
  management's legacy `AuthIssuer` / `AuthAudience` at Dex's
  `issuer` URL.
- Validate: `make dev.dashboard` brings up Dex + management
  + dashboard, contributor signs in via Dex's static password.

### Stage 2 — Production quickstart bundle
- `infrastructure_files/docker-compose.yml.tmpl`: add Dex
  service. Reuse the same encryption-key pattern as
  flow_exports for any signing material Dex needs.
- `infrastructure_files/nginx.tmpl.conf`: add `/dex/`
  location proxying to the Dex container.
- `infrastructure_files/docker-compose.yml.tmpl.traefik`:
  add Dex Traefik labels.
- `infrastructure_files/dex.config.yaml.tmpl`: production
  Dex config. Connector blocks commented-out as templates
  the operator un-comments.
- `infrastructure_files/configure.sh`: render the Dex YAML
  from `setup.env` variables.
- `infrastructure_files/setup.env.example`: document new
  variables (`OPENZRO_DEX_ISSUER` derived from
  `OPENZRO_DOMAIN`, optional connector secrets).

### Stage 3 — Dashboard pointer
- `dashboard/docker/entrypoint.sh` (or equivalent env wiring):
  set `OPENZRO_AUTH_AUTHORITY=https://<OPENZRO_DOMAIN>/dex`,
  `OPENZRO_AUTH_CLIENT_ID=openzro-dashboard`. The dashboard
  itself doesn't change — `@axa-fr/react-oidc` already uses
  these env vars.

### Stage 4 — Retire ADR-0005 code
A single batch revert (or a series of focused commits) removing
the components listed in "What we retire" above. Tests removed
alongside their code. Build, vet, lint, run the entire suite —
must stay green.

### Stage 5 — Housekeeping
- `Makefile`: drop `dev.management.up.bootstrap` and
  `dev.management.reset`. Drop `OPENZRO_BASE_URL` /
  `OPENZRO_ENABLE_BOOTSTRAP` from the management invocation.
  Restore the simpler form.
- `docs/security/advisories.md`: no change — the Next.js bump
  triage stays factual regardless of the auth pivot.
- `docs/operator/centralized-login-implementation-plan.md`:
  mark superseded, point at this ADR.
- `docs/operator/dex-setup.md`: new, replaces the old
  implementation plan. How to add a Google connector, LDAP
  connector, etc.

## Consequences

### Brand
The login page is Dex's, themed openZro. The URL is
`https://<OPENZRO_DOMAIN>/dex/auth/...`. Acceptable: the host
is still the operator's, the path-prefix `/dex` makes the
delegation visible but the brand template wraps the surface.

### Operator UX
Provider configuration moves from "Settings → Authentication
Providers" (ADR-0005's vision) to "edit `dex.config.yaml`,
restart Dex". Less point-and-click; more declarative + GitOps-
friendly. NetBird's documentation patterns suggest this is
acceptable for the user base.

### Code complexity
Net reduction of ~5,000 lines after stages 4 and 5 land. The
small Dex integration adds ~50 lines + ~100 lines of YAML
templating. The management server keeps the
`MultiIssuerValidator` primitive but loses everything else.

### Backwards compatibility
Existing deployments running on a legacy single-IdP keep
working — `MultiIssuerValidator` falls back to the legacy
`*Validator` for any token whose `iss` doesn't match a known
issuer. Deployments that previously had `AuthenticationProvider`
rows configured see those rows AutoMigrate-orphaned in the DB
on upgrade. Stage 4 includes an `IF EXISTS` drop for the
table.

### License posture
Apache-2.0 (Dex) + BSD-3-Clause (openZro core) + Apache-2.0
already-vendored (`go-oidc`, `oauth2`). No copyleft contamination.

## References

- [Dex GitHub repository](https://github.com/dexidp/dex)
- [Dex documentation](https://dexidp.io/docs/)
- [NetBird v0.62 announcement: Built-in Local Users + Optional IdP Integration](https://netbird.io/knowledge-hub/local-users-simplified-idp)
- [DeepWiki: NetBird Identity Provider Integration architecture](https://deepwiki.com/netbirdio/netbird/3.6-identity-provider-integration)
- [ADR-0005 — superseded](./0005-centralized-login.md)
- [ADR-0001 §3.1 — clean-room reimplementation policy](./0001-openzro-foundation.md)
