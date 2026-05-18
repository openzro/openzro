// Wire contract for the Control Center access-graph endpoint
// (GET /api/control-center/{view}/{id}). Hand-authored to mirror the
// Go GraphDTO exactly — that endpoint is intentionally omitted from
// openapi.yml (consistent with flow_exports/network_events), so the
// backend pins this shape via management/server/controlcenter
// contract_test.go and this file is its TS counterpart. Keep the two
// in lockstep (see ADR-0017 + project memory).

export type FocusType = "peer" | "group";

export type NodeKind =
  | "focus"
  | "policy"
  | "group"
  | "peer"
  | "route"
  | "network_resource";

export type EdgeState = "enforced" | "posture_blocked";

// router_local: the focus IS the router serving the route (gateway
// reach), distinct from route_default_permit (ADR-0017 amendment
// 2026-05-17). All three values must be handled.
export type PermitSource = "policy" | "route_default_permit" | "router_local";

export type EdgeDirection = "in" | "out" | "bidirectional";

export interface ControlCenterFocus {
  type: FocusType;
  id: string;
}

export interface ControlCenterNode {
  id: string;
  kind: NodeKind;
  label: string;
  meta?: Record<string, string>;
}

export interface ControlCenterEdge {
  from: string;
  to: string;
  permitSource: PermitSource;
  // present iff permitSource === "policy" (the clickable chip target)
  policyId?: string;
  policyName?: string;
  protocol: string;
  ports?: string[];
  sourceRanges?: string[];
  direction: EdgeDirection;
  state: EdgeState;
  // group focus: meta.reachedBy = "k of n members"; posture_blocked:
  // meta.postureCheck / postureCheckId / postureCheckType / postureReason
  meta?: Record<string, string>;
}

export interface ControlCenterGraph {
  focus: ControlCenterFocus;
  nodes: ControlCenterNode[];
  edges: ControlCenterEdge[];
}
