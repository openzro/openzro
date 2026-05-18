"use client";

import "@xyflow/react/dist/style.css";
import { type Edge, type Node, ReactFlow } from "@xyflow/react";
import { RefreshCw, Search } from "lucide-react";
import React, { useEffect, useMemo, useRef, useState } from "react";
import type {
  ControlCenterGraph,
  FocusType,
} from "@/interfaces/ControlCenter";
import { CC_EDGE_TYPES } from "./ControlCenterFlowEdge";
import {
  type ColumnHeader,
  columnHeaders,
  FOOTER_BAND,
  HEADER_BAND,
  layoutGraph,
  reachableFrom,
} from "./controlCenterLayout";
import { CC_NODE_TYPES } from "./ControlCenterNodes";

// ADR-0017 2026-05-18b — the v2 columnar canvas. Full-bleed: the
// stage spans the live container (ResizeObserver-measured), Policy
// is always the middle pivot column, edges are coloured by
// enforcement state (green = enforced, red = posture_blocked —
// owner-sanctioned brand exception). Column labels sit in a top
// band, legend + status in a bottom footer; both are overlays so
// the graph itself uses 100% of the area.

function ColumnHeaders({ cols }: { cols: ColumnHeader[] }) {
  return (
    <div
      className="pointer-events-none absolute inset-x-0 top-0 z-10"
      style={{ height: HEADER_BAND }}
    >
      {cols.map((c) => (
        <div
          key={c.id}
          className="absolute flex items-center justify-center gap-2"
          style={{ left: c.x, top: 12, width: c.width }}
        >
          <span className="h-1.5 w-1.5 rounded-full bg-oz2-acc" />
          <span className="text-[11px] font-semibold uppercase tracking-wide text-oz2-text-2">
            {c.label}
          </span>
          <span
            className="font-mono rounded border border-oz2-border-soft bg-oz2-bg-soft
              px-1.5 py-px text-[10px] text-oz2-text-muted"
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
      {/* "Policy-permitted", not "Enforced": v2 is a policy
          topology, green means the policy grants it and posture
          doesn't block — not a live reachability claim (ADR-0017
          2026-05-18c). */}
      <Item cls="bg-oz2-ok" label="Policy-permitted" />
      <Item cls="bg-oz2-err" label="Posture-blocked" />
      <span className="flex items-center gap-1.5">
        <span className="h-0.5 w-5 rounded-full border-t border-dashed border-oz2-text-faint" />
        <span>Policy flow</span>
      </span>
    </div>
  );
}

// FocusPicker is the inline selector anchored to the focus card
// (NetBird pattern, owner-confirmed 2026-05-18): a search field over
// a scrollable list. It opens against the card — NOT the header — so
// the affordance is where the click is.
function FocusPicker({
  x,
  y,
  cardH,
  width,
  stageH,
  options,
  currentId,
  onPick,
  onClose,
}: {
  x: number;
  y: number;
  cardH: number;
  width: number;
  stageH: number;
  options: { id: string; label: string }[];
  currentId: string;
  onPick: (id: string) => void;
  onClose: () => void;
}) {
  const [q, setQ] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  useEffect(() => inputRef.current?.focus(), []);

  const filtered = q
    ? options.filter((o) =>
        o.label.toLowerCase().includes(q.toLowerCase()),
      )
    : options;

  // Anchor below the card; flip above when the lower half wouldn't
  // fit a usable list.
  const openUp = y + cardH > stageH * 0.55;
  const pos = openUp
    ? { left: x, bottom: stageH - y + 6 }
    : { left: x, top: y + cardH + 6 };

  return (
    <>
      <div
        className="absolute inset-0 z-30"
        onClick={onClose}
        aria-hidden
      />
      <div
        className="oz-cc-scroll absolute z-40 flex max-h-[300px] flex-col
          overflow-hidden rounded-oz2-input border border-oz2-border
          bg-oz2-surface shadow-oz2-lg"
        style={{ ...pos, width: Math.max(width, 240) }}
      >
        <div className="flex items-center gap-2 border-b border-oz2-border-soft px-3 py-2">
          <Search className="h-3.5 w-3.5 shrink-0 text-oz2-text-faint" />
          <input
            ref={inputRef}
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search…"
            className="w-full bg-transparent text-xs text-oz2-text
              placeholder:text-oz2-text-faint focus:outline-none"
          />
        </div>
        <ul className="oz-cc-scroll min-h-0 flex-1 overflow-auto py-1">
          {filtered.length === 0 && (
            <li className="px-3 py-2 text-xs text-oz2-text-faint">
              No matches
            </li>
          )}
          {filtered.map((o) => (
            <li key={o.id}>
              <button
                type="button"
                onClick={() => onPick(o.id)}
                className={`flex w-full items-center truncate px-3 py-1.5
                  text-left text-xs transition-colors hover:bg-oz2-hover ${
                    o.id === currentId
                      ? "bg-oz2-acc-soft text-oz2-acc-text"
                      : "text-oz2-text-2"
                  }`}
              >
                {o.label}
              </button>
            </li>
          ))}
        </ul>
      </div>
    </>
  );
}

export default function ControlCenterGraphCanvas({
  graph,
  onEdgeClick,
  onFocusNode,
  onRefresh,
  focusOptions,
  focusId,
  onPickFocus,
}: {
  graph: ControlCenterGraph;
  onEdgeClick?: (policyId: string) => void;
  onFocusNode?: (view: FocusType, id: string) => void;
  onRefresh?: () => void;
  focusOptions: { id: string; label: string }[];
  focusId: string;
  onPickFocus: (id: string) => void;
}) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ w: 0, h: 0 });
  const [hovered, setHovered] = useState<string | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);

  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => {
      const r = entry.contentRect;
      setSize({ w: Math.round(r.width), h: Math.round(r.height) });
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const ready = size.w > 0 && size.h > 0;
  const laid = useMemo(
    () => (ready ? layoutGraph(graph, size.w, size.h) : null),
    [graph, ready, size.w, size.h],
  );
  const headers = useMemo(
    () => (ready ? columnHeaders(graph, size.w) : []),
    [graph, ready, size.w],
  );
  const lit = useMemo(
    () =>
      hovered && laid
        ? reachableFrom(hovered, laid.adjOut, laid.adjIn)
        : null,
    [hovered, laid],
  );
  const focusNode = useMemo(
    () => laid?.nodes.find((n) => n.data.column === "focus") ?? null,
    [laid],
  );

  const nodes: Node[] = useMemo(
    () =>
      (laid?.nodes ?? []).map((n) => ({
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
    [laid, lit],
  );

  const edges: Edge[] = useMemo(
    () =>
      (laid?.edges ?? []).map((e) => {
        const inLit = !!lit && lit.has(e.source) && lit.has(e.target);
        return {
          id: e.id,
          source: e.source,
          target: e.target,
          type: "cc",
          data: { ...e.data, dimmed: !!lit && !inLit, emphasis: inLit },
        };
      }),
    [laid, lit],
  );

  return (
    // No own border/bg: the parent card shell (ControlCenterView)
    // owns the frame and the top tab bar; this just fills the region
    // below it.
    <div ref={wrapRef} className="relative h-full w-full overflow-hidden">
      <div className="oz-cc-grid pointer-events-none absolute inset-0" />

      {ready && <ColumnHeaders cols={headers} />}

      <div className="absolute inset-0">
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
              column?: string;
              entityId?: string;
            };
            // The focus card is the picker affordance: clicking it
            // (or its chevron) opens an inline selector anchored to
            // the card to swap the inspected entity.
            if (d.kind === "focus" || d.column === "focus") {
              setPickerOpen((v) => !v);
              return;
            }
            // A policy card opens that policy in the editor (same
            // round-trip as clicking a policy-backed edge).
            if (d.kind === "policy" && onEdgeClick) {
              onEdgeClick(node.id.replace(/^policy:/, ""));
              return;
            }
            // A network-resource leaf re-focuses the Networks tab on
            // that resource ("who else can reach this?") — symmetric
            // with the peer/group re-focus. d.entityId is the raw
            // resource id (the "nr:" node-id prefix is already
            // stripped in the layout); /control-center/network/<id>
            // and the picker both key on that raw id.
            if (
              onFocusNode &&
              d.kind === "network_resource" &&
              d.column !== "focus" &&
              d.entityId
            ) {
              onFocusNode("network", d.entityId);
              return;
            }
            if (
              onFocusNode &&
              d.switchable &&
              d.entityId &&
              (d.kind === "peer" || d.kind === "group")
            ) {
              // d.entityId is the raw account-entity id; node.id is
              // prefixed for groups ("group:<gid>") / resources, which
              // the focus endpoint + picker do NOT understand —
              // passing node.id 404'd or fell back to the default
              // focus (#39 v2 review).
              onFocusNode(d.kind as FocusType, d.entityId);
            }
          }}
          onEdgeClick={(_, edge) => {
            const pid = (edge.data as { policyId?: string } | undefined)
              ?.policyId;
            if (pid && onEdgeClick) onEdgeClick(pid);
          }}
        />
      </div>

      {pickerOpen && focusNode && (
        <FocusPicker
          x={focusNode.position.x}
          y={focusNode.position.y}
          cardH={focusNode.height}
          width={focusNode.width}
          stageH={size.h}
          options={focusOptions}
          currentId={focusId}
          onPick={(id) => {
            setPickerOpen(false);
            onPickFocus(id);
          }}
          onClose={() => setPickerOpen(false)}
        />
      )}

      <div
        className="absolute inset-x-0 bottom-0 z-10 flex items-center
          justify-between border-t border-oz2-border bg-oz2-surface/80 px-4
          backdrop-blur-md"
        style={{ height: FOOTER_BAND }}
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
  );
}
