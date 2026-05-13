// MDMProvider mirrors the wire shape returned by
// /api/admin/mdm-providers. Sensitive fields (client_secret,
// api_token, api_secret) are NEVER on the wire — the public
// projection only carries enough for the UI to display
// "configured / not configured" states.
export type MDMProviderType =
  | "intune"
  | "sentinelone"
  | "huntress"
  | "crowdstrike";

export interface MDMProvider {
  id: number;
  name: string;
  type: MDMProviderType;
  enabled: boolean;
  config?:
    | IntunePublicConfig
    | SentinelOnePublicConfig
    | HuntressPublicConfig
    | CrowdStrikePublicConfig;
  created_at: string;
  updated_at: string;
}

export interface IntunePublicConfig {
  tenant_id: string;
  client_id: string;
  authority?: string;
  // strict_compliance opt-in: when true, devices reported by Intune as
  // `inGracePeriod` are treated as non-compliant. Default false keeps
  // the permissive behaviour (grace period counts as compliant).
  strict_compliance?: boolean;
  has_client_secret: boolean;
}

export interface SentinelOnePublicConfig {
  management_url: string;
  has_api_token: boolean;
}

export interface HuntressPublicConfig {
  has_credentials: boolean;
}

// CrowdStrike Falcon clouds. Tenants are pinned to one region; the
// OAuth client minted in that region only works against its home
// cloud, so the operator must pick the right value here.
export type CrowdStrikeCloud =
  | "us-1"
  | "us-2"
  | "eu-1"
  | "us-gov-1"
  | "us-gov-2";

export interface CrowdStrikePublicConfig {
  cloud: CrowdStrikeCloud | "";
  client_id: string;
  has_client_secret: boolean;
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
    strict_compliance?: boolean;
  };
  sentinelone?: {
    management_url: string;
    api_token?: string;
  };
  huntress?: {
    api_key?: string;
    api_secret?: string;
  };
  crowdstrike?: {
    cloud?: CrowdStrikeCloud | "";
    client_id: string;
    client_secret?: string;
  };
}
