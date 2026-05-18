"use client";

import "@xyflow/react/dist/style.css";
import { type Edge, type Node, ReactFlow } from "@xyflow/react";
import { RefreshCw } from "lucide-react";
import React, { useMemo, useState } from "react";
import type {
  ControlCenterGraph,
  FocusType,
} from "@/interfaces/ControlCenter";
import { CC_EDGE_TYPES } from "./ControlCenterFlowEdge";
import {
  type ColumnHeader,
  columnHeaders,
  layoutGraph,
  reachableFrom,
  STAGE_H,
  STAGE_W,
} from "./controlCenterLayout";
import { CC_NODE_TYPES } from "./ControlCenterNodes";

// ADR-0017 2026-05-18b — the v2 columnar canvas. Replaces the v1
// dagre reach graph: a fixed 1500×760 stage, four focus tabs,
// Policy always the middle pivot column, edges coloured by
// enforcement state (green = enforced, red = posture_blocked —
// owner-sanctioned brand exception). The stage is pan/zoom-locked;
// the wrapper scrolls horizontally on narrow viewports, matching the
// hifi handoff.

const HEADER_H = 56;
const FOOTER_H = 44;

function ColumnHeaders({ cols }: { cols: ColumnHeader[] }) {
  return (
    <div className="pointer-events-none absolute left-0 top-0 h-14 w-full">
      {cols.map((c) => (
        <div
          key={c.id}
          className="absolute flex items-center gap-2"
          style={{ left: c.x, top: 20, width: c.width }}
        >
          <span className="h-1.5 w-1.5 rounded-full bg-oz2-acc opacity-60" />
          <span className="font-mono text-[10.5px] uppercase tracking-wide text-oz2-text-faint">
            {c.label}
          </span>
          <span
            className="font-mono rounded border border-oz2-border-soft bg-oz2-bg-soft
              px-1.5 py-px text-[9.5px] text-oz2-text-muted"
          >
            {c.count}
          </span>
        </div>
      ))}
    </div>
  );
}

function Legend() {
  const Item = ({ cls, label }: { cls: string; label: string }) => (
    <span className="flex items-center gap-1.5">
      <span className={`h-0.5 w-5 rounded-full ${cls}`} />
      <span>{label}</span>
    </span>
  );
  return (
    <div className="flex items-center gap-4 text-[11px] text-oz2-text-muted">
      <Item cls="bg-oz2-ok" label="Enforced" />
      <Item cls="bg-oz2-err" label="Posture-blocked" />
      <span className="flex items-center gap-1.5">
        <span className="h-0.5 w-5 rounded-full border-t border-dashed border-oz2-text-faint" />
        <span>Policy flow</span>
      </span>
    </div>
  );
}

export default function ControlCenterGraphCanvas({
  graph,
  onEdgeClick,
  onFocusNode,
  onRefresh,
}: {
  graph: ControlCenterGraph;
  onEdgeClick?: (policyId: string) => void;
  onFocusNode?: (view: FocusType, id: string) => void;
  onRefresh?: () => void;
}) {
  const laid = useMemo(() => layoutGraph(graph), [graph]);
  const headers = useMemo(() => columnHeaders(graph), [graph]);
  const [hovered, setHovered] = useState<string | null>(null);

  const lit = useMemo(
    () => (hovered ? reachableFrom(hovered, laid.adjacency) : null),
    [hovered, laid.adjacency],
  );

  const nodes: Node[] = useMemo(
    () =>
      laid.nodes.map((n) => ({
        id: n.id,
        type: "cc",
        position: n.position,
        draggable: false,
        selectable: !!n.data.switchable,
        focusable: true,
        ariaLabel: `${n.data.kind}: ${n.data.label}`,
        style: { width: n.width, height: n.height },
        data: {
          ...n.data,
          dimmed: !!lit && !lit.has(n.id),
          emphasis: !!lit && lit.has(n.id),
        },
      })),
    [laid.nodes, lit],
  );

  const edges: Edge[] = useMemo(
    () =>
      laid.edges.map((e) => {
        const inLit = !!lit && lit.has(e.source) && lit.has(e.target);
        return {
          id: e.id,
          source: e.source,
          target: e.target,
          type: "cc",
          data: {
            ...e.data,
            dimmed: !!lit && !inLit,
            emphasis: inLit,
          },
        };
      }),
    [laid.edges, lit],
  );

  return (
    <div
      className="oz-cc-scroll relative w-full overflow-auto rounded-oz2-card
        border border-oz2-border-strong bg-oz2-bg"
      style={{ height: "72vh" }}
    >
      <div
        className="relative"
        style={{ width: STAGE_W, height: STAGE_H + HEADER_H + FOOTER_H }}
      >
        <div className="oz-cc-grid pointer-events-none absolute inset-0" />
        <span
          className="font-mono absolute right-4 top-3 z-10 rounded-md bg-oz2-acc-soft
            px-2 py-0.5 text-[10px] uppercase text-oz2-acc-text"
        >
          Beta
        </span>
        <ColumnHeaders cols={headers} />

        <div
          className="absolute left-0"
          style={{ top: HEADER_H, width: STAGE_W, height: STAGE_H }}
        >
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={CC_NODE_TYPES}
            edgeTypes={CC_EDGE_TYPES}
            fitView={false}
            defaultViewport={{ x: 0, y: 0, zoom: 1 }}
            nodesDraggable={false}
            nodesConnectable={false}
            elementsSelectable={!!onEdgeClick}
            panOnDrag={false}
            panOnScroll={false}
            zoomOnScroll={false}
            zoomOnPinch={false}
            zoomOnDoubleClick={false}
            preventScrolling={false}
            proOptions={{ hideAttribution: true }}
            onNodeMouseEnter={(_, n) => setHovered(n.id)}
            onNodeMouseLeave={() => setHovered(null)}
            onNodeClick={(_, node) => {
              const d = node.data as {
                kind?: string;
                switchable?: boolean;
              };
              if (
                onFocusNode &&
                d.switchable &&
                (d.kind === "peer" || d.kind === "group")
              ) {
                onFocusNode(d.kind as FocusType, node.id);
              }
            }}
            onEdgeClick={(_, edge) => {
              const pid = (edge.data as { policyId?: string } | undefined)
                ?.policyId;
              if (pid && onEdgeClick) onEdgeClick(pid);
            }}
          />
        </div>

        <div
          className="sticky bottom-0 left-0 flex items-center justify-between
            border-t border-oz2-border bg-oz2-surface/80 px-4 backdrop-blur-md"
          style={{ height: FOOTER_H }}
        >
          <Legend />
          <div className="flex items-center gap-3 text-[11px] text-oz2-text-muted">
            <span>
              {graph.edges.length} connection
              {graph.edges.length === 1 ? "" : "s"}
            </span>
            {onRefresh && (
              <button
                type="button"
                onClick={onRefresh}
                className="flex items-center gap-1.5 rounded-md border border-oz2-border
                  px-2 py-1 text-oz2-text-2 transition-colors hover:bg-oz2-hover"
              >
                <RefreshCw className="h-3 w-3" />
                Refresh
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
