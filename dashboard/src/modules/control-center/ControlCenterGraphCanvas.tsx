"use client";

import "@xyflow/react/dist/style.css";
import Dagre from "@dagrejs/dagre";
import {
  Background,
  BaseEdge,
  type Edge,
  EdgeLabelRenderer,
  type EdgeProps,
  Handle,
  MarkerType,
  type Node,
  type NodeProps,
  Position,
  ReactFlow,
} from "@xyflow/react";
import {
  Crosshair,
  Database,
  type LucideIcon,
  Network,
  ScrollText,
  Server,
  Users,
} from "lucide-react";
import React, { useMemo } from "react";
import {
  ControlCenterEdge,
  ControlCenterGraph,
  NodeKind,
} from "@/interfaces/ControlCenter";

// ADR-0017 Phase 2, P4 — the xyflow canvas. Read-only; dagre lays the
// graph out left→right. Posture-blocked edges are visually distinct
// from enforced (never collapsed into "no edge"). Each edge is
// labelled with its permit source (+ policy / protocol). Clicking a
// policy-backed edge (P5) opens the policy editor.

const NODE_W = 208;
const NODE_H = 58;

const KIND_ICON: Record<NodeKind, LucideIcon> = {
  focus: Crosshair,
  peer: Server,
  group: Users,
  policy: ScrollText,
  route: Network,
  network_resource: Database,
};

const KIND_LABEL: Record<NodeKind, string> = {
  focus: "Focus",
  peer: "Peer",
  group: "Group",
  policy: "Policy",
  route: "Route",
  network_resource: "Network resource",
};

// Info-dense kind card (ADR-0017 visual pass, owner-chosen). Violet
// accent only (brand: no non-violet accents). Focus is emphasised;
// peer/group are clickable foci (cursor cue). Secondary line = the
// peer/route IP/CIDR (meta.ip, or the label already is the CIDR for
// routes) falling back to the humanised kind.
function CCNode({ data }: NodeProps) {
  const d = data as {
    label: string;
    kind: NodeKind;
    secondary: string;
    switchable: boolean;
  };
  const Icon = KIND_ICON[d.kind] ?? Server;
  const isFocus = d.kind === "focus";

  return (
    <div
      style={{ width: NODE_W, height: NODE_H }}
      className={`flex items-center gap-2 overflow-hidden rounded-oz2-input border bg-oz2-surface pr-3 ${
        isFocus
          ? "border-oz2-acc ring-1 ring-oz2-acc"
          : "border-oz2-border-strong"
      } ${d.switchable ? "cursor-pointer" : ""}`}
    >
      <Handle
        type="target"
        position={Position.Left}
        isConnectable={false}
        className="!h-1.5 !w-1.5 !min-w-0 !border-0 !bg-oz2-border-strong"
      />
      <span
        className={`flex h-full w-9 shrink-0 items-center justify-center ${
          isFocus
            ? "bg-oz2-acc/15 text-oz2-acc"
            : "bg-oz2-bg-sunken text-oz2-text-muted"
        }`}
        aria-hidden
      >
        <Icon size={15} />
      </span>
      <span className="flex min-w-0 flex-col">
        <span
          className={`truncate text-xs ${
            isFocus ? "font-semibold" : ""
          } text-oz2-text`}
        >
          {d.label}
        </span>
        <span className="truncate text-[10px] uppercase tracking-wide text-oz2-text-faint">
          {d.secondary}
        </span>
      </span>
      <Handle
        type="source"
        position={Position.Right}
        isConnectable={false}
        className="!h-1.5 !w-1.5 !min-w-0 !border-0 !bg-oz2-border-strong"
      />
    </div>
  );
}

function edgeLabel(e: ControlCenterEdge): string {
  const src =
    e.permitSource === "policy"
      ? e.policyName || "policy"
      : e.permitSource;
  // protocol/ports on the edge (ADR-0017 Phase 3): e.g. "tcp/22,443",
  // "tcp", "22,443", or nothing for an all-protocol no-port rule.
  const protoPorts = [
    e.protocol && e.protocol !== "all" ? e.protocol : "",
    e.ports && e.ports.length ? e.ports.join(",") : "",
  ]
    .filter(Boolean)
    .join("/");
  const proto = protoPorts ? ` · ${protoPorts}` : "";
  const blocked = e.state === "posture_blocked" ? " · blocked" : "";
  return `${src}${proto}${blocked}`;
}

function layout(graph: ControlCenterGraph): {
  nodes: Node[];
  edges: Edge[];
} {
  const g = new Dagre.graphlib.Graph();
  // Generous rank spacing so the edge label pills sit in the gap
  // between columns instead of overlapping the cards.
  g.setGraph({ rankdir: "LR", nodesep: 40, ranksep: 170 });
  g.setDefaultEdgeLabel(() => ({}));

  for (const n of graph.nodes) {
    g.setNode(n.id, { width: NODE_W, height: NODE_H });
  }
  for (const e of graph.edges) {
    g.setEdge(e.from, e.to);
  }
  Dagre.layout(g);

  const nodes: Node[] = graph.nodes.map((n) => {
    const p = g.node(n.id);
    // peer/group nodes are valid v1 foci → clicking one re-centres
    // the graph on it (Phase 3).
    const switchable = n.kind === "peer" || n.kind === "group";
    // Secondary line: peer/focus carry meta.ip; routes/resources put
    // their CIDR in the label already, so fall back to the kind.
    const secondary = n.meta?.ip || KIND_LABEL[n.kind] || "";
    return {
      id: n.id,
      type: "cc",
      position: { x: (p?.x ?? 0) - NODE_W / 2, y: (p?.y ?? 0) - NODE_H / 2 },
      data: { label: n.label || n.id, kind: n.kind, secondary, switchable },
      width: NODE_W,
      height: NODE_H,
    };
  });

  // Phase 1 deliberately preserves distinct relations with the same
  // from/to (router_local vs route_default_permit, multi-cause
  // posture_blocked, multi-policy). Number each within its (from,to)
  // group so the custom edge fans them onto separate tracks instead
  // of overlapping (#51 F3).
  const parCount: Record<string, number> = {};
  for (const e of graph.edges) {
    const k = `${e.from}\u0000${e.to}`;
    parCount[k] = (parCount[k] ?? 0) + 1;
  }
  const parSeen: Record<string, number> = {};

  const edges: Edge[] = graph.edges.map((e, i) => {
    const blocked = e.state === "posture_blocked";
    const k = `${e.from}\u0000${e.to}`;
    const parIndex = parSeen[k] ?? 0;
    parSeen[k] = parIndex + 1;
    return {
      id: `e${i}-${e.from}-${e.to}`,
      source: e.from,
      target: e.to,
      type: "parallel",
      style: blocked
        ? { stroke: "var(--ozv2-err)", strokeDasharray: "5 4" }
        : { stroke: "var(--ozv2-acc)" },
      markerEnd: { type: MarkerType.ArrowClosed },
      data: {
        policyId: e.policyId ?? "",
        label: edgeLabel(e),
        blocked,
        parIndex,
        parTotal: parCount[k],
      },
    };
  });

  return { nodes, edges };
}

// ParallelEdge fans edges that share a (source,target) onto separate
// curved tracks (perpendicular offset by parallel index, centred on
// zero) with the label on the same offset, so the parallel relations
// the backend preserves are not visually collapsed (#51 F3).
const PAR_GAP = 26;

function ParallelEdge({
  sourceX,
  sourceY,
  targetX,
  targetY,
  markerEnd,
  style,
  data,
}: EdgeProps) {
  const d = (data ?? {}) as {
    label?: string;
    blocked?: boolean;
    parIndex?: number;
    parTotal?: number;
  };
  const idx = d.parIndex ?? 0;
  const total = d.parTotal ?? 1;
  const offset = (idx - (total - 1) / 2) * PAR_GAP;

  const dx = targetX - sourceX;
  const dy = targetY - sourceY;
  const len = Math.hypot(dx, dy) || 1;
  const nx = -dy / len;
  const ny = dx / len;
  const mx = (sourceX + targetX) / 2 + nx * offset;
  const my = (sourceY + targetY) / 2 + ny * offset;
  const path = `M ${sourceX},${sourceY} Q ${mx},${my} ${targetX},${targetY}`;

  return (
    <>
      <BaseEdge path={path} markerEnd={markerEnd} style={style} />
      <EdgeLabelRenderer>
        <div
          className={`pointer-events-none absolute truncate rounded-full border bg-oz2-surface px-2 py-0.5 text-[10px] ${
            d.blocked
              ? "border-oz2-err/40 text-oz2-err"
              : "border-oz2-border-strong text-oz2-text-muted"
          }`}
          style={{
            transform: `translate(-50%,-50%) translate(${mx}px,${my}px)`,
            maxWidth: 150,
          }}
          title={d.label}
        >
          {d.label}
        </div>
      </EdgeLabelRenderer>
    </>
  );
}

export default function ControlCenterGraphCanvas({
  graph,
  onEdgeClick,
  onFocusNode,
}: {
  graph: ControlCenterGraph;
  onEdgeClick?: (policyId: string) => void;
  onFocusNode?: (view: "peer" | "group", id: string) => void;
}) {
  const { nodes, edges } = useMemo(() => layout(graph), [graph]);
  const edgeTypes = useMemo(() => ({ parallel: ParallelEdge }), []);
  const nodeTypes = useMemo(() => ({ cc: CCNode }), []);

  return (
    <div className="h-[68vh] w-full overflow-hidden rounded-oz2-card border border-oz2-border-strong">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        fitView
        nodesDraggable={false}
        nodesConnectable={false}
        edgesFocusable={!!onEdgeClick}
        proOptions={{ hideAttribution: true }}
        onNodeClick={(_, node) => {
          const kind = (node.data as { kind?: string } | undefined)?.kind;
          // Only peer/group nodes are valid v1 foci; clicking the
          // current focus or a route/resource/policy node is a no-op.
          if (onFocusNode && (kind === "peer" || kind === "group")) {
            onFocusNode(kind, node.id);
          }
        }}
        onEdgeClick={(_, edge) => {
          const pid = (edge.data as { policyId?: string } | undefined)
            ?.policyId;
          if (pid && onEdgeClick) onEdgeClick(pid);
        }}
      >
        <Background />
      </ReactFlow>
    </div>
  );
}
