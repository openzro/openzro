export interface Account {
  id: string;
  domain: string;
  domain_category: string;
  created_at: string;
  created_by: string;
  settings: {
    extra: {
      peer_approval_enabled: boolean;
      network_traffic_logs_enabled?: boolean;
      network_traffic_packet_counter_enabled?: boolean;
      // Optional list of group IDs that scope traffic event capture.
      // When non-empty AND network_traffic_logs_enabled is true, only
      // peers whose groups intersect this list will capture and report
      // flow events. Empty / undefined = every peer reports.
      network_traffic_logs_groups?: string[];
      // Pre-fills the date filter on Flow Traffic. Recognised values:
      // "1h" | "6h" | "24h" | "7d" | "30d" | "all". Empty / "all" keeps
      // the no-filter behaviour.
      network_traffic_default_range?: string;
    };
    peer_login_expiration_enabled: boolean;
    peer_login_expiration: number;
    peer_inactivity_expiration_enabled: boolean;
    peer_inactivity_expiration: number;
    groups_propagation_enabled: boolean;
    jwt_groups_enabled: boolean;
    jwt_groups_claim_name: string;
    jwt_allow_groups: string[];
    regular_users_view_blocked: boolean;
    routing_peer_dns_resolution_enabled: boolean;
    dns_domain: string;
    lazy_connection_enabled: boolean;
    admission_enforcement_enabled?: boolean;
    admission_posture_checks?: string[];
    admission_exempt_groups?: string[];
    // Desktop client self-update directive (openZro #5). Empty target
    // version = no directive. force = silent install vs. offered for a
    // manual install. The *_groups / *_peers / *_percent fields scope
    // the directive to a subset SERVER-SIDE; the client never receives
    // them, only the resolved directive (or nothing). Precedence:
    // exclude beats everything incl. explicit peers; explicit peers
    // pierce the ring; target groups are ring-gated; empty groups AND
    // peers = whole fleet.
    client_update_target_version?: string;
    client_update_force?: boolean;
    client_update_target_groups?: string[];
    client_update_target_peers?: string[];
    client_update_exclude_groups?: string[];
    // 0..100 staged ring. undefined = no ring (everyone in scope); an
    // explicit 0 = nobody (fail-closed).
    client_update_rollout_percent?: number;
  };
}
