// v2 columnar layout (ADR-0017 2026-05-18b). Replaces the v1 dagre
// pass: positions are derived directly from the backend's
// meta.column tag — Policy is always the middle pivot column. Pure
// (no React / no DOM) so it is unit-testable and memoisable.

import type {
  ControlCenterGraph,
  FocusType,
  NodeKind,
} from "@/interfaces/ControlCenter";

// Columns span the MEASURED canvas (the view is full-bleed now), not
// a fixed stage: x is derived from the live container width so the
// graph uses 100% of the available area. HEADER_BAND/FOOTER_BAND are
// the chrome the canvas overlays at top (column labels) and bottom
// (legend + status).
export const HEADER_BAND = 40;
export const FOOTER_BAND = 44;
const PAD_X = 28;
const PAD_Y = 18;

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
  // DIRECTED adjacency for hover-isolation. Hovering a node lights
  // only the nodes on a path THROUGH it: its ancestors (followed via
  // adjIn, left-ward) and its descendants (via adjOut, right-ward).
  // A plain undirected closure would, on a dense graph, light the
  // whole component — hovering a resource must reveal only the peers
  // / policies / flows that reach THAT resource.
  adjOut: Record<string, string[]>;
  adjIn: Record<string, string[]>;
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

const ROW_GAP = 16;

// distributeY returns the vertical CENTRE of each of `count` items.
// Items sit at a fixed pitch (card height + gap) so a sparse column
// reads as a tight, vertically-centred stack — not two cards pinned
// to the top and bottom edges. Only when the natural stack is taller
// than the band does it compress to fit (dense columns still fill).
function distributeY(count: number, height: number, cardH: number): number[] {
  if (count <= 0) return [];
  const top = HEADER_BAND + PAD_Y + cardH / 2;
  const bottom = height - FOOTER_BAND - PAD_Y - cardH / 2;
  const mid = (top + bottom) / 2;
  if (count === 1) return [mid];
  if (bottom <= top) return Array.from({ length: count }, () => mid);

  const pitch = cardH + ROW_GAP;
  const naturalSpan = (count - 1) * pitch;
  const available = bottom - top;
  if (naturalSpan <= available) {
    const start = mid - naturalSpan / 2;
    return Array.from({ length: count }, (_, i) => start + i * pitch);
  }
  const step = available / (count - 1);
  return Array.from({ length: count }, (_, i) => top + step * i);
}

const cmp = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0);

// edgeKind: a User→Peer (or any policy-less) edge is identity
// ownership — drawn solid, not as an animated policy flow.
function structural(permitSource: string, policyId?: string): boolean {
  return !policyId && permitSource === "";
}

// columnSlots spreads the focus's columns evenly across the live
// width: each column gets an equal slot, card centred in it.
function columnSlots(
  order: ColumnId[],
  width: number,
): Record<ColumnId, { x: number; centerX: number; width: number }> {
  const usable = Math.max(width - PAD_X * 2, order.length * 200);
  const slotW = usable / order.length;
  const out = {} as Record<
    ColumnId,
    { x: number; centerX: number; width: number }
  >;
  order.forEach((col, slot) => {
    const centerX = PAD_X + slotW * (slot + 0.5);
    const cardW = Math.min(CARD_W[col], slotW - 32);
    out[col] = { x: centerX - cardW / 2, centerX, width: cardW };
  });
  return out;
}

export function layoutGraph(
  graph: ControlCenterGraph,
  width: number,
  height: number,
): LaidOutGraph {
  const focus = graph.focus.type;
  const order = COLUMN_ORDER[focus] ?? COLUMN_ORDER.peer;
  const slots = columnSlots(order, width);

  const byColumn = new Map<ColumnId, typeof graph.nodes>();
  for (const n of graph.nodes) {
    const col = columnOf(n, order);
    const bucket = byColumn.get(col) ?? [];
    bucket.push(n);
    byColumn.set(col, bucket);
  }

  const nodes: LaidOutNode[] = [];
  order.forEach((col) => {
    const bucket = (byColumn.get(col) ?? [])
      .slice()
      .sort((a, b) => cmp(a.label || a.id, b.label || b.id));
    const slotInfo = slots[col];
    const tallest = bucket.reduce(
      (m, n) => Math.max(m, CARD_H[n.kind] ?? 50),
      40,
    );
    const ys = distributeY(bucket.length, height, tallest);
    bucket.forEach((n, i) => {
      const h = CARD_H[n.kind] ?? 50;
      nodes.push({
        id: n.id,
        position: { x: slotInfo.x, y: ys[i] - h / 2 },
        width: slotInfo.width,
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

  const adjOut: Record<string, string[]> = {};
  const adjIn: Record<string, string[]> = {};
  const link = (from: string, to: string) => {
    (adjOut[from] ??= []).push(to);
    (adjIn[to] ??= []).push(from);
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

  return { nodes, edges, adjOut, adjIn, width, height };
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

export function columnHeaders(
  graph: ControlCenterGraph,
  width: number,
): ColumnHeader[] {
  const focus = graph.focus.type;
  const order = COLUMN_ORDER[focus] ?? COLUMN_ORDER.peer;
  const slots = columnSlots(order, width);
  const counts = new Map<ColumnId, number>();
  for (const n of graph.nodes) {
    const col = columnOf(n, order);
    counts.set(col, (counts.get(col) ?? 0) + 1);
  }
  return order.map((col) => ({
    id: col,
    label: COLUMN_LABEL[col],
    count: counts.get(col) ?? 0,
    x: slots[col].x,
    width: slots[col].width,
  }));
}

function cone(start: string, adj: Record<string, string[]>): Set<string> {
  const seen = new Set<string>([start]);
  const queue = [start];
  while (queue.length) {
    const cur = queue.shift() as string;
    for (const next of adj[cur] ?? []) {
      if (!seen.has(next)) {
        seen.add(next);
        queue.push(next);
      }
    }
  }
  return seen;
}

// reachableFrom lights only the nodes on a path THROUGH `start`: its
// ancestor cone (walked left-ward via adjIn) ∪ its descendant cone
// (right-ward via adjOut). On the resource end this is exactly "the
// peers, policies and flows that permit access to THIS resource" —
// it does NOT bleed into sibling branches the way an undirected
// closure would on a dense mesh (#39 v2 review — hover isolation).
export function reachableFrom(
  start: string,
  adjOut: Record<string, string[]>,
  adjIn: Record<string, string[]>,
): Set<string> {
  const lit = cone(start, adjIn);
  for (const id of cone(start, adjOut)) lit.add(id);
  return lit;
}
