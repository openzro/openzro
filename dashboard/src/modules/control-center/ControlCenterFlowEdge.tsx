"use client";

import {
  BaseEdge,
  EdgeLabelRenderer,
  type EdgeProps,
} from "@xyflow/react";
import React from "react";
import type { CCFlowEdgeData } from "./controlCenterLayout";

// v2 topology edge. Cubic Bézier with horizontal-pull control points
// (the hifi handoff's bezierPath, curve 0.55). OWNER OVERRIDE of the
// brand palette (ADR-0017 2026-05-18b, sanctioned exception): edges
// are coloured by enforcement STATE — green = enforced (allowed),
// red = posture_blocked — not the design's violet gradient, because
// the audit signal is the whole point of this view. Policy edges get
// the animated "live traffic" dash; a structural User→Peer edge is
// solid.

const CURVE = 0.55;

function bezier(x1: number, y1: number, x2: number, y2: number): string {
  const dx = x2 - x1;
  const cx1 = x1 + dx * CURVE;
  const cx2 = x2 - dx * CURVE;
  return `M ${x1} ${y1} C ${cx1} ${y1}, ${cx2} ${y2}, ${x2} ${y2}`;
}

interface RenderEdgeData extends CCFlowEdgeData {
  dimmed?: boolean;
  emphasis?: boolean;
}

export function CCFlowEdge({
  sourceX,
  sourceY,
  targetX,
  targetY,
  data,
}: EdgeProps) {
  // xyflow types edge data as Record<string, unknown> — third-party
  // boundary (CLAUDE.md sanctioned `as` exception).
  const d = (data ?? {}) as unknown as RenderEdgeData;
  const path = bezier(sourceX, sourceY, targetX, targetY);
  const color = d.blocked ? "var(--ozv2-err)" : "var(--ozv2-ok)";
  const opacity = d.dimmed ? 0.07 : 1;
  const width = d.emphasis ? 2.2 : 1.6;

  const flowClass = d.structural ? "" : "oz-cc-flow";
  const dash = d.structural ? undefined : "2 8";

  const mx = (sourceX + targetX) / 2;
  const my = (sourceY + targetY) / 2;

  return (
    <>
      <BaseEdge
        path={path}
        className={flowClass}
        style={{
          stroke: color,
          strokeWidth: width,
          strokeDasharray: dash,
          opacity,
          transition: "opacity .25s, stroke-width .25s",
        }}
      />
      {d.emphasis && d.label && (
        <EdgeLabelRenderer>
          <div
            className="font-mono pointer-events-none absolute rounded bg-oz2-surface
              px-1.5 py-0.5 text-[10px] text-oz2-text-muted shadow-oz2-sm"
            style={{
              transform: `translate(-50%,-50%) translate(${mx}px,${my}px)`,
            }}
          >
            {d.label}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
}

export const CC_EDGE_TYPES = { cc: CCFlowEdge };
