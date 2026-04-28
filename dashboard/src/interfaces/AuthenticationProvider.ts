// Wire shape of a Dex connector, as proxied by management at
// /api/admin/auth-providers. The `config` field is an opaque
// JSON blob whose shape depends on `type` — see ConnectorConfig
// below for the per-type schemas the dashboard form composes.

// `keycloak` and `okta` are UI-only labels — Dex's storage doesn't have
// dedicated connectors for them; both are persisted as `oidc`. We keep
// the visual distinction by sniffing the issuer URL on round-trip
// (see inferConnectorType below) and by mapping back to `oidc` on save
// (see dexConnectorType). The trade-off is documented in ADR-0006.
export type ConnectorType =
  | "google"
  | "github"
  | "microsoft"
  | "keycloak"
  | "okta"
  | "oidc"
  | "ldap"
  | "saml";

export interface AuthenticationProvider {
  id: string;
  type: ConnectorType | string; // string fallback for types we don't model yet
  name: string;
  config: unknown; // shape varies by type — narrow at the call site
}

// Per-type config shapes for the OAuth-style connectors the
// dashboard form supports today. LDAP / SAML have heavy nested
// shapes (userSearch, groupSearch, attribute mappings) — the
// modal currently surfaces them as raw JSON for advanced
// operators; structured forms land later.

export interface GoogleConfig {
  clientID: string;
  clientSecret: string;
  redirectURI: string;
  hostedDomains?: string[];
}

export interface GitHubConfig {
  clientID: string;
  clientSecret: string;
  redirectURI: string;
  orgs?: { name: string; teams?: string[] }[];
  loadAllGroups?: boolean;
  useLoginAsID?: boolean;
}

export interface MicrosoftConfig {
  clientID: string;
  clientSecret: string;
  redirectURI: string;
  tenant?: string;
  groups?: string[];
}

export interface OIDCConfig {
  issuer: string;
  clientID: string;
  clientSecret: string;
  redirectURI: string;
  scopes?: string[];
  insecureSkipEmailVerified?: boolean;
}

export type ConnectorConfig =
  | GoogleConfig
  | GitHubConfig
  | MicrosoftConfig
  | OIDCConfig;

// Wire shape for create / update. The dashboard composes Config
// from the type-specific form, then sends as `config` (JSON
// object — the management handler forwards as raw bytes to Dex).
export interface AuthenticationProviderInput {
  id: string;
  type: ConnectorType;
  name: string;
  config: ConnectorConfig | Record<string, unknown>;
}

// Display metadata for the type dropdown. Order here drives the
// order the operator sees in the modal.
export interface ConnectorTypeMeta {
  value: ConnectorType;
  label: string;
  description: string;
}

export const CONNECTOR_TYPES: ConnectorTypeMeta[] = [
  {
    value: "google",
    label: "Google",
    description: "Google Workspace / consumer Google accounts",
  },
  {
    value: "github",
    label: "GitHub",
    description: "GitHub OAuth (organisations + personal accounts)",
  },
  {
    value: "microsoft",
    label: "Microsoft Entra ID",
    description: "Microsoft Entra ID / Azure AD (multi-tenant or per-tenant)",
  },
  {
    value: "keycloak",
    label: "Keycloak",
    description:
      "Self-hosted Keycloak realm. Issuer is realm-scoped, e.g. https://kc.example.com/realms/master.",
  },
  {
    value: "okta",
    label: "Okta",
    description:
      "Okta Workforce / Customer Identity. Issuer is your Okta domain, e.g. https://your-tenant.okta.com.",
  },
  {
    value: "oidc",
    label: "Generic OIDC",
    description:
      "Any other OIDC-compliant IdP (Auth0, Authentik, Zitadel, Google IDaaS, …).",
  },
];

// Callback URL the **upstream IdP** (Keycloak, Okta, Google, …)
// redirects to after authenticating the user. The OIDC flow goes:
// dashboard → Dex → upstream IdP → Dex → dashboard. The redirect
// at the connector level lands on Dex's own /callback, NOT the
// dashboard's — Dex is the one with the connector state and code
// verifier; it then re-issues its own auth code to the dashboard.
// This must match what the operator whitelists in their IdP.
//
// Computed from the dashboard's authority config (which already
// points at Dex's issuer URL — `http://localhost:5556` in dev,
// `https://<domain>/dex` in production).
export function defaultRedirectURI(authority: string): string {
  if (!authority) return "";
  return `${authority.replace(/\/+$/, "")}/callback`;
}

// Dex collapses Keycloak/Okta into the generic `oidc` connector. We
// preserve the UI label by sniffing the issuer URL on round-trip:
//   - `*.okta.com` / `*.oktapreview.com` → "okta"
//   - any path containing `/realms/`        → "keycloak"
//   - everything else                       → "oidc"
// Native types (google/github/microsoft/ldap/saml) pass through.
export function inferConnectorType(
  type: ConnectorType | string,
  config: unknown,
): ConnectorType | string {
  if (type !== "oidc") return type;

  const issuer =
    config && typeof config === "object" && "issuer" in config &&
    typeof (config as { issuer?: unknown }).issuer === "string"
      ? (config as { issuer: string }).issuer.trim()
      : "";
  if (!issuer) return "oidc";

  let url: URL;
  try {
    url = new URL(issuer);
  } catch {
    return "oidc";
  }

  const host = url.host.toLowerCase();
  if (host.endsWith(".okta.com") || host.endsWith(".oktapreview.com")) {
    return "okta";
  }
  if (/\/realms\//.test(url.pathname)) {
    return "keycloak";
  }
  return "oidc";
}

// Map a UI-side ConnectorType back to what Dex actually persists.
// The UI offers Keycloak/Okta as separate dropdown entries for clarity,
// but Dex's storage only knows the generic `oidc` connector for both.
export function dexConnectorType(t: ConnectorType): ConnectorType {
  if (t === "keycloak" || t === "okta") return "oidc";
  return t;
}

// Issuer URL placeholder per UI type. Helps operators get the shape
// right on first try (the URL pattern is what powers inferConnectorType
// on the round-trip).
export function issuerPlaceholder(t: ConnectorType): string {
  switch (t) {
    case "keycloak":
      return "https://kc.example.com/realms/master";
    case "okta":
      return "https://your-tenant.okta.com";
    default:
      return "https://idp.example.com";
  }
}
