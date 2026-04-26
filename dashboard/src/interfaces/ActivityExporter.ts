// ActivityExporter mirrors the wire shape returned by
// /api/admin/activity-exporters. The same shape is what the dashboard
// posts on create/update; secrets (api_key, password) are write-only
// — the public projection only carries enough for the UI to display
// "configured / not configured" states.
export type ActivityExporterType = "http" | "datadog" | "elastic";

export interface ActivityExporter {
  id: number;
  name: string;
  type: ActivityExporterType;
  enabled: boolean;
  template?: string;
  config?:
    | HTTPPublicConfig
    | DatadogPublicConfig
    | ElasticPublicConfig;
  created_at: string;
  updated_at: string;
}

export interface HTTPPublicConfig {
  url: string;
  header_names?: string[];
  timeout_ms?: number;
  max_attempts?: number;
  initial_backoff_ms?: number;
}

export interface DatadogPublicConfig {
  site?: string;
  url?: string;
  has_api_key: boolean;
  service?: string;
  source?: string;
  tags?: string;
  hostname?: string;
  batch_size?: number;
  flush_interval_ms?: number;
  buffer_size?: number;
}

export interface ElasticPublicConfig {
  url: string;
  index?: string;
  auth_mode?: "api_key" | "basic" | "";
  batch_size?: number;
  flush_interval_ms?: number;
  buffer_size?: number;
}

// ActivityExporterInput is the create/update body. Sensitive fields
// go in plaintext over HTTPS and the server encrypts them at rest.
// On edit, leave the secret field empty to keep the previously-saved
// value (the server never reads secrets back, so the dashboard sends
// "" to mean "unchanged").
export interface ActivityExporterInput {
  name: string;
  type: ActivityExporterType;
  enabled?: boolean;
  template?: string;
  http?: {
    url: string;
    headers?: Record<string, string>;
    timeout_ms?: number;
    max_attempts?: number;
    initial_backoff_ms?: number;
  };
  datadog?: {
    site?: string;
    url?: string;
    api_key?: string;
    service?: string;
    source?: string;
    tags?: string;
    hostname?: string;
    batch_size?: number;
    flush_interval_ms?: number;
    buffer_size?: number;
  };
  elastic?: {
    url: string;
    index?: string;
    api_key?: string;
    username?: string;
    password?: string;
    batch_size?: number;
    flush_interval_ms?: number;
    buffer_size?: number;
  };
}
