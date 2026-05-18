// v2 columnar layout (ADR-0017 2026-05-18b). Replaces the v1 dagre
// pass: positions are derived directly from the backend's
// meta.column tag — Policy is always the middle pivot column. Pure
// (no React / no DOM) so it is unit-testable and memoisable.

import type {
  ControlCenterGraph,
  FocusType,
  NodeKind,
} from "@/interfaces/ControlCenter";

export const STAGE_W = 1500;
export const STAGE_H = 760;
const PAD_Y = 48;
const COL_X0 = 48;
const COL_GAP = 352;

export type ColumnId =
  | "focus"
  | "peers"
  | "policies"
  | "resources"
  | "groups";

// Display order of columns per focus tab. Policy is always the pivot;
// only User adds a Peers column; Network is the inverse fan-in.
const COLUMN_ORDER: Record<FocusType, ColumnId[]> = {
  peer: ["focus", "policies", "resources"],
  user: ["focus", "peers", "policies", "resources"],
  group: ["focus", "policies", "resources"],
  network: ["groups", "policies", "focus"],
};

const CARD_W: Record<ColumnId, number> = {
  focus: 280,
  peers: 240,
  policies: 260,
  resources: 260,
  groups: 240,
};

export const CARD_H: Record<NodeKind, number> = {
  focus: 58,
  user: 58,
  peer: 50,
  policy: 38,
  group: 50,
  route: 50,
  network_resource: 50,
  network: 58,
};

export interface CCFlowNodeData {
  kind: NodeKind;
  label: string;
  column: ColumnId;
  meta: Record<string, string>;
  switchable: boolean;
}

export interface CCFlowEdgeData {
  policyId: string;
  blocked: boolean;
  structural: boolean;
  label: string;
}

export interface LaidOutNode {
  id: string;
  position: { x: number; y: number };
  width: number;
  height: number;
  data: CCFlowNodeData;
}

export interface LaidOutEdge {
  id: string;
  source: string;
  target: string;
  data: CCFlowEdgeData;
}

export interface LaidOutGraph {
  nodes: LaidOutNode[];
  edges: LaidOutEdge[];
  // undirected adjacency for hover-isolation (two-hop transitive
  // reachability is derived from this in the canvas).
  adjacency: Record<string, string[]>;
  width: number;
  height: number;
}

function columnOf(
  node: { kind: NodeKind; meta?: Record<string, string> },
  order: ColumnId[],
): ColumnId {
  const tagged = node.meta?.column as ColumnId | undefined;
  if (tagged && order.includes(tagged)) return tagged;
  if (node.kind === "focus") return "focus";
  if (node.kind === "policy") return "policies";
  if (node.kind === "peer") return order.includes("peers") ? "peers" : "resources";
  return "resources";
}

// distributeY returns the vertical CENTRE of each of `count` items,
// evenly spread and block-centred in the stage.
function distributeY(count: number): number[] {
  if (count <= 0) return [];
  if (count === 1) return [STAGE_H / 2];
  const usable = STAGE_H - PAD_Y * 2;
  const step = usable / (count - 1);
  return Array.from({ length: count }, (_, i) => PAD_Y + step * i);
}

const cmp = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0);

// edgeKind: a User→Peer (or any policy-less) edge is identity
// ownership — drawn solid, not as an animated policy flow.
function structural(permitSource: string, policyId?: string): boolean {
  return !policyId && permitSource === "";
}

export function layoutGraph(graph: ControlCenterGraph): LaidOutGraph {
  const focus = graph.focus.type;
  const order = COLUMN_ORDER[focus] ?? COLUMN_ORDER.peer;

  const byColumn = new Map<ColumnId, typeof graph.nodes>();
  for (const n of graph.nodes) {
    const col = columnOf(n, order);
    const bucket = byColumn.get(col) ?? [];
    bucket.push(n);
    byColumn.set(col, bucket);
  }

  const nodes: LaidOutNode[] = [];
  order.forEach((col, slot) => {
    const bucket = (byColumn.get(col) ?? [])
      .slice()
      .sort((a, b) => cmp(a.label || a.id, b.label || b.id));
    const xLeft = COL_X0 + slot * COL_GAP;
    const ys = distributeY(bucket.length);
    bucket.forEach((n, i) => {
      const h = CARD_H[n.kind] ?? 50;
      nodes.push({
        id: n.id,
        position: { x: xLeft, y: ys[i] - h / 2 },
        width: CARD_W[col],
        height: h,
        data: {
          kind: n.kind,
          label: n.label || n.id,
          column: col,
          meta: n.meta ?? {},
          // peer/group nodes that are not the current focus are valid
          // foci → clicking re-centres the graph on them.
          switchable:
            n.kind !== "focus" && (n.kind === "peer" || n.kind === "group"),
        },
      });
    });
  });

  const adjacency: Record<string, string[]> = {};
  const link = (a: string, b: string) => {
    (adjacency[a] ??= []).push(b);
    (adjacency[b] ??= []).push(a);
  };

  const edges: LaidOutEdge[] = graph.edges.map((e, i) => {
    link(e.from, e.to);
    const blocked = e.state === "posture_blocked";
    const isStructural = structural(e.permitSource, e.policyId);
    const protoPorts = [
      e.protocol && e.protocol !== "all" ? e.protocol : "",
      e.ports && e.ports.length ? e.ports.join(",") : "",
    ]
      .filter(Boolean)
      .join("/");
    return {
      id: `e${i}-${e.from}-${e.to}-${e.state}`,
      source: e.from,
      target: e.to,
      data: {
        policyId: e.policyId ?? "",
        blocked,
        structural: isStructural,
        label: protoPorts,
      },
    };
  });

  return {
    nodes,
    edges,
    adjacency,
    width: STAGE_W,
    height: STAGE_H,
  };
}

const COLUMN_LABEL: Record<ColumnId, string> = {
  focus: "Focus",
  peers: "Peers",
  policies: "Policies",
  resources: "Resources",
  groups: "Groups",
};

export interface ColumnHeader {
  id: ColumnId;
  label: string;
  count: number;
  x: number;
  width: number;
}

export function columnHeaders(graph: ControlCenterGraph): ColumnHeader[] {
  const focus = graph.focus.type;
  const order = COLUMN_ORDER[focus] ?? COLUMN_ORDER.peer;
  const counts = new Map<ColumnId, number>();
  for (const n of graph.nodes) {
    const col = columnOf(n, order);
    counts.set(col, (counts.get(col) ?? 0) + 1);
  }
  return order.map((col, slot) => ({
    id: col,
    label: COLUMN_LABEL[col],
    count: counts.get(col) ?? 0,
    x: COL_X0 + slot * COL_GAP,
    width: CARD_W[col],
  }));
}

// reachableFrom does the two-hop-and-beyond transitive walk the hifi
// spec asks for: hovering a node reveals its full reachability tree
// (both directions, since the column graph is a DAG and an auditor
// wants "what touches this" as well as "what this touches").
export function reachableFrom(
  start: string,
  adjacency: Record<string, string[]>,
): Set<string> {
  const seen = new Set<string>([start]);
  const queue = [start];
  while (queue.length) {
    const cur = queue.shift() as string;
    for (const next of adjacency[cur] ?? []) {
      if (!seen.has(next)) {
        seen.add(next);
        queue.push(next);
      }
    }
  }
  return seen;
}
