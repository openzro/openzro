// AuthenticationProvider mirrors the wire shape returned by
// /api/admin/auth-providers. Sensitive fields (client_secret)
// are NEVER on the wire — the public projection only carries
// has_client_secret so the dashboard can render
// "configured / not configured" states.

export type AuthenticationProviderType =
  | "oidc-generic"
  | "google"
  | "github"
  | "microsoft"
  | "entra-id"
  | "okta"
  | "keycloak"
  | "authentik"
  | "zitadel";

export interface AuthenticationProvider {
  id: number;
  name: string;
  type: AuthenticationProviderType;
  enabled: boolean;
  brand_label: string;
  brand_logo_url?: string;
  email_domain_hint?: string;
  config?: AuthenticationProviderPublicConfig;
  created_at: string;
  updated_at: string;
}

// AuthenticationProviderPublicConfig is the redacted projection —
// same shape as the Go-side providers.PublicConfig. Never carries
// client_secret; has_client_secret tells the modal whether to
// render "leave blank to keep" or "set a secret".
export interface AuthenticationProviderPublicConfig {
  issuer_url: string;
  client_id: string;
  scopes?: string[];
  has_client_secret: boolean;
  authorization_endpoint?: string;
  token_endpoint?: string;
  userinfo_endpoint?: string;
  jwks_url?: string;
}

// AuthenticationProviderInput is the wire shape for POST/PUT
// against /admin/auth-providers. client_secret is optional on
// edit so an operator can keep the existing secret by leaving
// the field blank — the backend's Save validates that the
// secret IS present on create.
export interface AuthenticationProviderInput {
  name: string;
  type: AuthenticationProviderType;
  enabled?: boolean;
  brand_label?: string;
  brand_logo_url?: string;
  email_domain_hint?: string;
  config: {
    issuer_url: string;
    client_id: string;
    client_secret?: string;
    scopes?: string[];
    authorization_endpoint?: string;
    token_endpoint?: string;
    userinfo_endpoint?: string;
    jwks_url?: string;
  };
}

// ProviderTypeMeta carries display + form-prefill data for the
// dashboard's "Add provider" modal: brand label default,
// per-type issuer URL placeholder, default scope set.
export interface ProviderTypeMeta {
  value: AuthenticationProviderType;
  label: string;
  description: string;
  issuerPlaceholder: string;
  defaultScopes: string[];
  // experimental: the backend's callback path doesn't yet
  // support this provider end-to-end. Today this is only
  // GitHub — its OAuth2-only flow needs a userinfo round-trip
  // that lands in a follow-up. The modal warns the operator.
  experimental: boolean;
}

export const PROVIDER_TYPES: ProviderTypeMeta[] = [
  {
    value: "oidc-generic",
    label: "Generic OIDC",
    description: "Any OIDC-compliant identity provider",
    issuerPlaceholder: "https://idp.example.com",
    defaultScopes: ["openid", "profile", "email"],
    experimental: false,
  },
  {
    value: "google",
    label: "Google",
    description: "Google Workspace / consumer Google accounts",
    issuerPlaceholder: "https://accounts.google.com",
    defaultScopes: ["openid", "profile", "email"],
    experimental: false,
  },
  {
    value: "microsoft",
    label: "Microsoft",
    description: "Microsoft personal accounts (multi-tenant)",
    issuerPlaceholder: "https://login.microsoftonline.com/common/v2.0",
    defaultScopes: ["openid", "profile", "email"],
    experimental: false,
  },
  {
    value: "entra-id",
    label: "Microsoft Entra ID",
    description: "Azure AD / Entra ID for organisations",
    issuerPlaceholder:
      "https://login.microsoftonline.com/<tenant-id>/v2.0",
    defaultScopes: ["openid", "profile", "email"],
    experimental: false,
  },
  {
    value: "okta",
    label: "Okta",
    description: "Okta Workforce / Customer Identity",
    issuerPlaceholder: "https://<your-domain>.okta.com/oauth2/default",
    defaultScopes: ["openid", "profile", "email"],
    experimental: false,
  },
  {
    value: "keycloak",
    label: "Keycloak",
    description: "Self-hosted Keycloak realm",
    issuerPlaceholder: "https://<keycloak-host>/realms/<realm>",
    defaultScopes: ["openid", "profile", "email"],
    experimental: false,
  },
  {
    value: "authentik",
    label: "Authentik",
    description: "Self-hosted Authentik provider",
    issuerPlaceholder: "https://<authentik-host>/application/o/<slug>/",
    defaultScopes: ["openid", "profile", "email"],
    experimental: false,
  },
  {
    value: "zitadel",
    label: "Zitadel",
    description: "Zitadel cloud or self-hosted",
    issuerPlaceholder: "https://<instance>.zitadel.cloud",
    defaultScopes: ["openid", "profile", "email"],
    experimental: false,
  },
  {
    value: "github",
    label: "GitHub",
    description:
      "GitHub OAuth (organisations / contractors). OAuth2-only — userinfo flow.",
    issuerPlaceholder: "https://github.com",
    defaultScopes: ["read:user", "user:email"],
    experimental: true,
  },
];

export function providerTypeMeta(
  t: AuthenticationProviderType,
): ProviderTypeMeta {
  return PROVIDER_TYPES.find((p) => p.value === t) ?? PROVIDER_TYPES[0];
}
