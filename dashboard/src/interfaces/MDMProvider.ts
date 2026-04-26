// MDMProvider mirrors the wire shape returned by
// /api/admin/mdm-providers. Sensitive fields (client_secret,
// api_token, api_secret) are NEVER on the wire — the public
// projection only carries enough for the UI to display
// "configured / not configured" states.
export type MDMProviderType = "intune" | "sentinelone" | "huntress";

export interface MDMProvider {
  id: number;
  name: string;
  type: MDMProviderType;
  enabled: boolean;
  config?: IntunePublicConfig | SentinelOnePublicConfig | HuntressPublicConfig;
  created_at: string;
  updated_at: string;
}

export interface IntunePublicConfig {
  tenant_id: string;
  client_id: string;
  authority?: string;
  has_client_secret: boolean;
}

export interface SentinelOnePublicConfig {
  management_url: string;
  has_api_token: boolean;
}

export interface HuntressPublicConfig {
  has_credentials: boolean;
}

// MDMProviderInput is the create/update body — sensitive fields go
// in plaintext over HTTPS and the server encrypts them at rest.
export interface MDMProviderInput {
  name: string;
  type: MDMProviderType;
  enabled?: boolean;
  intune?: {
    tenant_id: string;
    client_id: string;
    client_secret?: string;
    authority?: string;
  };
  sentinelone?: {
    management_url: string;
    api_token?: string;
  };
  huntress?: {
    api_key?: string;
    api_secret?: string;
  };
}
