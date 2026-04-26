// FlowExport mirrors the wire shape returned by
// /api/admin/flow-exports — sensitive fields (api_key, password,
// secret_key, header values) are NEVER on the wire. The `config`
// field carries the public projection for whatever Type is set.
export type FlowExportType = "elastic" | "s3" | "http";

export interface FlowExport {
  id: number;
  name: string;
  type: FlowExportType;
  enabled: boolean;
  config?: ElasticPublicConfig | S3PublicConfig | HTTPPublicConfig;
  created_at: string;
  updated_at: string;
}

export interface ElasticPublicConfig {
  url: string;
  index?: string;
  auth_mode?: "api_key" | "basic" | "";
  batch_size?: number;
  flush_interval?: number;
  buffer_size?: number;
}

export interface S3PublicConfig {
  bucket: string;
  region?: string;
  endpoint?: string;
  has_credentials: boolean;
  prefix?: string;
  flush_interval?: number;
  max_events_per_file?: number;
  buffer_size?: number;
}

export interface HTTPPublicConfig {
  url: string;
  header_names?: string[];
  timeout?: number;
  max_attempts?: number;
  initial_backoff?: number;
}

// FlowExportInput is what the create/update endpoint accepts. The
// per-type config blocks are optional; only the one matching `type`
// is used. Credentials go in plaintext on the wire (over HTTPS) and
// the server encrypts them at rest.
export interface FlowExportInput {
  name: string;
  type: FlowExportType;
  enabled?: boolean;
  elastic?: {
    url: string;
    index?: string;
    api_key?: string;
    username?: string;
    password?: string;
    batch_size?: number;
  };
  s3?: {
    bucket: string;
    region?: string;
    endpoint?: string;
    access_key?: string;
    secret_key?: string;
    prefix?: string;
  };
  http?: {
    url: string;
  };
}
