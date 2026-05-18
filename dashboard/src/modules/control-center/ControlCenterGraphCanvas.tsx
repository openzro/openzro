"use client";

import Dagre from "@dagrejs/dagre";
import {
  Background,
  type Edge,
  MarkerType,
  type Node,
  Position,
  ReactFlow,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
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

const NODE_W = 190;
const NODE_H = 44;

const KIND_CLASS: Record<NodeKind, string> = {
  focus: "border-oz2-acc bg-oz2-acc/10 text-oz2-text font-semibold",
  peer: "border-oz2-border-strong bg-oz2-surface text-oz2-text",
  group: "border-oz2-border-strong bg-oz2-surface text-oz2-text",
  policy: "border-oz2-border-strong bg-oz2-bg-sunken text-oz2-text-muted",
  route: "border-oz2-border-strong bg-oz2-surface text-oz2-text",
  network_resource: "border-oz2-border-strong bg-oz2-surface text-oz2-text",
};

function edgeLabel(e: ControlCenterEdge): string {
  const src =
    e.permitSource === "policy"
      ? e.policyName || "policy"
      : e.permitSource;
  const proto = e.protocol && e.protocol !== "all" ? ` · ${e.protocol}` : "";
  const blocked = e.state === "posture_blocked" ? " · blocked" : "";
  return `${src}${proto}${blocked}`;
}

function layout(graph: ControlCenterGraph): {
  nodes: Node[];
  edges: Edge[];
} {
  const g = new Dagre.graphlib.Graph();
  g.setGraph({ rankdir: "LR", nodesep: 24, ranksep: 90 });
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
    return {
      id: n.id,
      position: { x: (p?.x ?? 0) - NODE_W / 2, y: (p?.y ?? 0) - NODE_H / 2 },
      data: { label: n.label || n.id },
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
      className: `rounded-oz2-input border px-3 py-2 text-xs ${
        KIND_CLASS[n.kind] ?? KIND_CLASS.peer
      }`,
      width: NODE_W,
      height: NODE_H,
    };
  });

  const edges: Edge[] = graph.edges.map((e, i) => {
    const blocked = e.state === "posture_blocked";
    return {
      id: `e${i}-${e.from}-${e.to}`,
      source: e.from,
      target: e.to,
      label: edgeLabel(e),
      labelShowBg: true,
      animated: blocked,
      style: blocked
        ? { stroke: "var(--ozv2-err)", strokeDasharray: "5 4" }
        : { stroke: "var(--ozv2-acc)" },
      markerEnd: { type: MarkerType.ArrowClosed },
      data: { policyId: e.policyId ?? "" },
    };
  });

  return { nodes, edges };
}

export default function ControlCenterGraphCanvas({
  graph,
  onEdgeClick,
}: {
  graph: ControlCenterGraph;
  onEdgeClick?: (policyId: string) => void;
}) {
  const { nodes, edges } = useMemo(() => layout(graph), [graph]);

  return (
    <div className="h-[68vh] w-full overflow-hidden rounded-oz2-card border border-oz2-border-strong">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        fitView
        nodesDraggable={false}
        nodesConnectable={false}
        edgesFocusable={!!onEdgeClick}
        proOptions={{ hideAttribution: true }}
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
