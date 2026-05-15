import { Group } from "@/interfaces/Group";

export interface Network {
  id: string;
  name: string;
  description?: string;
  resources?: string[];
  policies?: string[];
  routers?: string[];
  routing_peers_count?: number;
}

export interface NetworkRouter {
  id: string;
  peer?: string;
  peer_groups?: string[];
  metric: number;
  masquerade: boolean;
  enabled: boolean;
}

export interface NetworkResource {
  id: string;
  name: string;
  description?: string;
  address: string;
  groups?: string[] | Group[];
  type?: "domain" | "host" | "subnet";
  enabled: boolean;
  // resolved_addresses: only present for type=domain — the distinct
  // destination IPs observed in flow events over the last 24h,
  // aggregated across every peer that has resolved the domain. The
  // management server itself does not do DNS — each peer agent
  // resolves locally and reports the resolved IP alongside the
  // resource ID in its flow events; this field exposes that
  // aggregate so the UI can display "currently resolves to ..."
  // without inferring from observed traffic post-hoc. Absent /
  // undefined for host and subnet resources.
  resolved_addresses?: string[];
}
