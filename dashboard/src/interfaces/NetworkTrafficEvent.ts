// NetworkTrafficEvent mirrors the JSON shape returned by the
// management server's GET /api/network-traffic-events endpoint. The
// types follow the eventDTO in
// management/server/http/handlers/network_events: byte-typed fields
// arrive hex-encoded and are kept as strings on the client; if a
// component needs the raw bytes it decodes locally.
export interface NetworkTrafficEvent {
  event_id: string;
  flow_id: string;
  peer_id: string;
  is_initiator: boolean;
  occurred_at: string; // ISO 8601
  received_at: string;
  type: "start" | "end" | "drop" | "unknown";
  direction: "ingress" | "egress" | "unknown";
  protocol: number;
  source_ip: string;
  dest_ip: string;
  source_port?: number;
  dest_port?: number;
  icmp_type?: number;
  icmp_code?: number;
  rx_packets: number;
  tx_packets: number;
  rx_bytes: number;
  tx_bytes: number;
  rule_id?: string;
  source_resource_id?: string;
  dest_resource_id?: string;
}

// NetworkTrafficEventsResponse is the envelope returned by the list
// endpoint. `events` is always present (possibly empty); pagination
// metadata helps the table know whether more pages exist.
export interface NetworkTrafficEventsResponse {
  events: NetworkTrafficEvent[];
  limit: number;
  offset: number;
}
