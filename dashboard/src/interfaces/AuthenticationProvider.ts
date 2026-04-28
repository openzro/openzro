// Wire shape of a Dex connector, as proxied by management at
// /api/admin/auth-providers. The `config` field is an opaque
// JSON blob whose shape depends on `type` — see ConnectorConfig
// below for the per-type schemas the dashboard form composes.

export type ConnectorType =
  | "google"
  | "github"
  | "microsoft"
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
    value: "oidc",
    label: "Generic OIDC",
    description:
      "Any OIDC-compliant IdP (Okta, Auth0, Keycloak, Authentik, Zitadel, …)",
  },
];

// Default redirect URI Dex expects for every connector. Same
// path regardless of connector type; the operator's IdP must
// whitelist it. Computed from window.location.origin at form
// render time.
export function defaultRedirectURI(): string {
  if (typeof window === "undefined") return "";
  return `${window.location.origin}/dex/callback`;
}
