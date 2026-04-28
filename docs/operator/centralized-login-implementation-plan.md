# Implementation plan: openZro-branded centralized login

> **Superseded note (2026-04-28).** This plan implemented
> ADR-0005, which is itself superseded by [ADR-0006 — Embed
> Dex](../adr/0006-embed-dex.md). The retirement of the work
> described below is tracked under ADR-0006's stages 4-5.
> The text is preserved for historical context.

Companion to [ADR-0005](../adr/0005-centralized-login.md). This
file tracks the actual landing PRs.

## Branch + PR layout

Single feature branch `feat/centralized-login`. Inside it, **eight
incremental PRs** that can be reviewed and merged top-down:

| # | Scope | LoC est. | Lands first |
|---|---|---|---|
| 1 | `auth/providers/` GORM model + Store + Decrypt — schema only | ~250 | ✅ |
| 2 | `auth/providers/` OIDC manager (build live `oidc.Provider`s, JWK cache, `Refresh()`) | ~300 | depends on 1 |
| 3 | Multi-issuer JWT validator replacing single-issuer in `jwtclaims/` | ~400 | depends on 2 |
| 4 | HTTP handlers `/auth/start` and `/auth/callback` (PKCE, state cookie) | ~350 | depends on 3 |
| 5 | Server-rendered `/login` template + brand assets | ~200 | depends on 4 |
| 6 | Admin REST API: `POST/GET/PUT/DELETE /admin/auth-providers` | ~250 | depends on 1, 2 |
| 7 | Dashboard UI: Settings → Authentication Providers (list + modal) | ~400 | depends on 6 |
| 8 | Activity codes 95–99 + audit emit on every auth event | ~100 | depends on 4 |

Total: **~2200 LoC** plus tests. Each PR carries its own table-driven
unit tests.

## File tree (after V1 lands)

```
management/
  server/
    auth/
      providers/
        model.go              # AuthenticationProvider, encrypted shapes
        store.go              # CRUD + encryption envelope
        manager.go            # OIDC provider cache, JWK refresh
        flow.go               # PKCE: state cookie, code exchange
        flow_test.go
        store_test.go
        manager_test.go
      session.go              # openZro session JWT issue / verify
      session_test.go
    jwtclaims/
      multi_issuer_validator.go    # NEW
      multi_issuer_validator_test.go
      validator.go                  # legacy single-issuer (kept for fallback)
    http/
      handlers/
        auth/
          login_handler.go     # GET /login (server-rendered HTML)
          start_handler.go     # GET /auth/start
          callback_handler.go  # GET /auth/callback
          logout_handler.go    # POST /auth/logout
          provider_handler.go  # POST/GET/PUT/DELETE /admin/auth-providers
          ...handlers_test.go
        templates/
          login.tmpl            # Go html/template, openZro brand
    activity/
      codes.go                  # add 95..99
docs/
  adr/0005-centralized-login.md
  operator/
    centralized-login-implementation-plan.md   # this file
    configure-authentication-providers.md      # operator-facing how-to (V1.5)
dashboard/
  src/
    modules/
      auth/
        AuthenticationProvidersTab.tsx
        AuthenticationProviderModal.tsx
        ProviderTypeSelect.tsx
    interfaces/
      AuthenticationProvider.ts
    app/(dashboard)/settings/page.tsx          # add tab
    app/(unauth)/login/page.tsx                # fallback redirector
```

## Database schema (PR 1)

```go
// auth/providers/model.go
package providers

import "time"

// ProviderType tags well-known OIDC providers so the dashboard can
// render the right brand + pre-fill scopes / endpoints. `oidc-generic`
// is the catch-all for anything compliant.
type ProviderType string

const (
    TypeGeneric    ProviderType = "oidc-generic"
    TypeGoogle     ProviderType = "google"
    TypeGitHub     ProviderType = "github"
    TypeMicrosoft  ProviderType = "microsoft"
    TypeEntraID    ProviderType = "entra-id"
    TypeOkta       ProviderType = "okta"
    TypeKeycloak   ProviderType = "keycloak"
    TypeAuthentik  ProviderType = "authentik"
    TypeZitadel    ProviderType = "zitadel"
)

type AuthenticationProvider struct {
    ID             uint64       `gorm:"primaryKey;autoIncrement"`
    Name           string       `gorm:"size:128;not null"`
    Type           ProviderType `gorm:"size:32;not null;index"`
    Enabled        bool         `gorm:"not null;default:true"`
    PublicConfig   []byte       `gorm:"type:bytea"`  // brand label, scopes, issuer URL
    ConfigCipher   []byte       `gorm:"type:bytea;not null"`  // client_secret + sensitive
    BrandLabel     string       `gorm:"size:128"`     // shown on the login button
    BrandLogoURL   string       `gorm:"size:512"`     // optional
    EmailDomainHint string      `gorm:"size:128"`     // V2: route by email domain
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

func (AuthenticationProvider) TableName() string { return "authentication_providers" }

// PublicView is what the /login page can read without decryption —
// no secrets, no issuer-internal fields.
type PublicView struct {
    ID           uint64       `json:"id"`
    Name         string       `json:"name"`
    Type         ProviderType `json:"type"`
    Enabled      bool         `json:"enabled"`
    BrandLabel   string       `json:"brand_label"`
    BrandLogoURL string       `json:"brand_logo_url,omitempty"`
}
```

## Multi-issuer validator (PR 3)

Today: `management/server/jwtclaims/jwtValidator.go` accepts a single
issuer URL and a single JWKs URL. New shape:

```go
type MultiIssuerValidator struct {
    // Each provider's Verifier, keyed by issuer URL. Population is
    // refreshed by ProviderManager.Refresh().
    verifiers map[string]*oidc.IDTokenVerifier
    legacy    *jwtValidator  // fallback for deployments without
                              // any AuthenticationProvider row.
}

func (v *MultiIssuerValidator) ValidateAndParse(
    ctx context.Context, token string,
) (jwt.MapClaims, error) {
    // Extract `iss` claim from the unverified header so we know
    // which Verifier to use without trying every key.
    iss, err := unsafeIssuer(token)
    if err != nil { return nil, err }
    verifier, ok := v.verifiers[iss]
    if !ok {
        if v.legacy != nil { return v.legacy.ValidateAndParse(ctx, token) }
        return nil, fmt.Errorf("unknown issuer %q", iss)
    }
    idTok, err := verifier.Verify(ctx, token)
    if err != nil { return nil, err }
    var claims jwt.MapClaims
    if err := idTok.Claims(&claims); err != nil { return nil, err }
    return claims, nil
}
```

Unit tests sign tokens with a generated keypair, expose JWKs from a
test HTTP server, and assert validation succeeds for known issuers
and fails (with a clear error) for unknown ones. **Every PR after #3
must add a regression test for any OIDC quirk encountered during
implementation.**

## PKCE flow (PR 4)

State cookie (HttpOnly, SameSite=Lax, 5-minute expiry):

```json
{
  "provider_id": 7,
  "code_verifier": "<43-128 char random>",
  "return_to": "/peers",
  "issued_at": 1714291234
}
```

`/auth/start?provider=7&return_to=/peers`:

1. Look up `AuthenticationProvider` by ID.
2. Generate `code_verifier` (43–128 cryptographically random chars).
3. Compute `code_challenge = SHA256(code_verifier)` (base64-url-no-pad).
4. Build the `authorization_endpoint` URL with the provider's
   `client_id`, `redirect_uri = our /auth/callback`, scopes, state,
   `code_challenge`, `code_challenge_method=S256`.
5. Set state cookie. Redirect.

`/auth/callback?provider=7&code=...&state=...`:

1. Read state cookie. Verify expiry, provider match.
2. POST to `token_endpoint` with `code`, `code_verifier`,
   `client_id`, `client_secret`, `redirect_uri`.
3. Verify the returned ID token via `MultiIssuerValidator`.
4. Look up / create the user in the openZro user table by
   `(iss, sub)`.
5. Issue an openZro session JWT (signed with the management's
   own key, not the upstream's). 1-hour expiry. Refresh token in
   HttpOnly cookie, 30-day expiry.
6. 302 to `state.return_to` (or `/peers` default).

## Provider configuration UX (PR 7)

Settings → **Authentication Providers** tab. List view:

| Name | Type | Enabled | Last sign-in | Actions |
|---|---|---|---|---|
| Acme Entra | Entra ID | ✓ | 2 minutes ago | Edit / Disable |
| GitHub for contractors | GitHub | ✓ | 1 day ago | Edit / Disable |
| Legacy Keycloak | Keycloak | ✗ | (disabled) | Enable / Delete |

"Add provider" modal:

1. Pick type (`Generic OIDC`, `Google`, `GitHub`, `Microsoft`,
   `Entra ID`, `Okta`, `Keycloak`, `Authentik`, `Zitadel`).
2. Form pre-fills the `issuer_url` and default scopes for the picked
   type. User overrides if needed.
3. Required fields: `Name` (free text), `Client ID`, `Client Secret`.
4. Optional: `Brand label` (defaults to provider type), `Logo URL`,
   `Scopes` (default per type), `Email domain hint` (V2).
5. **Test connection** button calls `POST /admin/auth-providers/test`
   server-side: tries OIDC discovery against the issuer, fetches
   JWKs, verifies the response shape. Surfaces any failure right
   in the modal before save.
6. Save → triggers `ProviderManager.Refresh()` server-side so the
   new provider is live without a restart.

## Activity codes (PR 8)

Append to `management/server/activity/codes.go`:

```go
const (
    AuthProviderCreated  Activity = 95
    AuthProviderUpdated  Activity = 96
    AuthProviderDeleted  Activity = 97
    AuthSessionGranted   Activity = 98
    AuthSessionRevoked   Activity = 99
)
```

Every PR that touches an auth path also emits the matching activity
event with `IdpUserId`, `Provider`, `IpAddress`, `UserAgent`.
Audit-stream consumers see auth events in the same firehose as
peer / policy / admission events — no parallel pipeline.

## Tests

**Unit (Go)**
- `auth/providers/store_test.go` — encrypt round-trip, public-view
  redaction.
- `auth/providers/manager_test.go` — JWK fetch + cache, refresh
  path, missing-provider error.
- `jwtclaims/multi_issuer_validator_test.go` — validates against
  N issuers, rejects unknown, falls back to legacy when configured.
- `auth/providers/flow_test.go` — PKCE state-cookie round-trip,
  code-verifier ↔ code-challenge consistency, expired-state
  rejection.

**Integration (testcontainers)**
- Real Zitadel container, real Keycloak container. Configure both
  as providers, sign in via each, assert the openZro session JWT
  is issued and accepted by `/api/peers`.

**E2E (Cypress)**
- `cypress/e2e/auth-providers.cy.ts`: configure 2 providers via
  the dashboard, sign out, sign in via each, verify role +
  groups propagate from IdP claims.

## Migration story

For deployments that already use Zitadel via the quickstart
bundle:

1. Upgrade `openzro-mgmt` to the V1 release.
2. Existing flow keeps working — no `AuthenticationProvider`
   rows means the legacy single-IdP path is still active.
3. Operator opens Settings → Authentication Providers → Add
   provider → picks `Zitadel`, enters their Zitadel issuer URL +
   client ID + secret. Saves.
4. The login flow now points at `/login` and serves the openZro
   brand. The Zitadel button is the only one shown until the
   operator adds others.
5. Operator deletes the legacy single-IdP env vars when ready;
   the multi-issuer validator already picks up the configured
   provider.

Zero downtime, zero forced cutover.

## Risks called out

- **Auth is security-critical.** Plan to land each PR with a
  reviewer who has OAuth/OIDC experience. The maintainers list a
  reviewer on every auth-touching PR.
- **Refresh token semantics get tricky** when the upstream IdP
  rotates its signing keys mid-flight. Cache invalidation hooks
  in `ProviderManager.Refresh()` plus a 24h JWK cache TTL handle
  the common case; edge cases land as regression tests.
- **CSRF on the state cookie**. Solved by `SameSite=Lax` and
  binding `state` to provider ID + return URL inside the cookie
  payload (signed). PR 4 has tests for replay + cross-provider.

## V2 (after V1 lands cleanly)

Sequenced in `docs/operator/centralized-login-v2.md` once V1 ships.
High-level: email-domain routing, per-tenant theming, rate-limit /
brute-force, SSO discovery, account-recovery hooks.
