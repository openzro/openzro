"use client";

import { Handle, type NodeProps,Position } from "@xyflow/react";
import { ChevronDown, Server, Shield, Users } from "lucide-react";
import React from "react";
import { OSLogo } from "@/modules/peers/PeerOSCell";
import type { CCFlowNodeData } from "./controlCenterLayout";

// v2 topology kind-cards (ADR-0017 2026-05-18b / hifi handoff). One
// info-dense card per node kind, themed via oz2-* tokens so light and
// dark both work. xyflow injects width/height from the layout pass;
// the card fills its node box.

interface RenderData extends CCFlowNodeData {
  dimmed?: boolean;
  emphasis?: boolean;
}

function initials(label: string): string {
  const parts = label.trim().split(/[\s@._-]+/).filter(Boolean);
  if (parts.length === 0) return "?";
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[1][0]).toUpperCase();
}

const SHELL_BASE =
  "group relative flex h-full w-full items-center rounded-[10px] " +
  "border bg-oz2-surface shadow-oz2-sm transition-all duration-200";

function shellClass(d: RenderData): string {
  const tone = d.emphasis
    ? "border-oz2-acc ring-[3px] ring-oz2-acc-soft -translate-y-px shadow-oz2-md"
    : "border-oz2-border hover:border-oz2-acc hover:ring-[3px] " +
      "hover:ring-oz2-acc-soft hover:-translate-y-px hover:shadow-oz2-md";
  const dim = d.dimmed ? "opacity-25" : "opacity-100";
  // focus card is the picker affordance, peer/group target nodes are
  // re-focus affordances → both read as clickable.
  const click =
    d.switchable || d.column === "focus" ? "cursor-pointer" : "";
  return `${SHELL_BASE} ${tone} ${dim} ${click}`;
}

function Handles() {
  return (
    <>
      <Handle
        type="target"
        position={Position.Left}
        className="!h-1 !w-1 !border-0 !bg-transparent"
        isConnectable={false}
      />
      <Handle
        type="source"
        position={Position.Right}
        className="!h-1 !w-1 !border-0 !bg-transparent"
        isConnectable={false}
      />
    </>
  );
}

const AVATAR =
  "flex shrink-0 items-center justify-center rounded-lg bg-gradient-to-br " +
  "from-oz2-acc to-openzro-900 font-bold text-white";
const TILE =
  "flex shrink-0 items-center justify-center rounded-md " +
  "border border-oz2-border-soft bg-oz2-bg-soft text-oz2-text-2";

function FocusCard({ d }: { d: RenderData }) {
  const sub = d.meta.email || d.meta.ip || d.meta.sub || "";
  return (
    <div className={`${shellClass(d)} px-3.5 py-3`}>
      <Handles />
      <div className={`${AVATAR} mr-3 h-[34px] w-[34px] text-sm`}>
        {initials(d.label)}
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-[13.5px] font-semibold text-oz2-text">
          {d.label}
        </div>
        {sub && (
          <div className="font-mono mt-0.5 truncate text-[11px] text-oz2-text-muted">
            {sub}
          </div>
        )}
      </div>
      <ChevronDown className="ml-2 h-4 w-4 shrink-0 text-oz2-text-faint" />
    </div>
  );
}

function PeerCard({ d }: { d: RenderData }) {
  return (
    <div className={`${shellClass(d)} gap-2.5 px-3 py-2`}>
      <Handles />
      <div className={`${TILE} relative h-[26px] w-[26px]`}>
        <OSLogo os={d.meta.os || "linux"} />
        <span
          className="absolute -bottom-0.5 -right-0.5 h-[9px] w-[9px] rounded-full
            border-2 border-oz2-surface bg-oz2-dot-on"
        />
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-[12.5px] font-medium text-oz2-text">
          {d.label}
        </div>
        {d.meta.ip && (
          <div className="font-mono mt-0.5 truncate text-[11px] text-oz2-text-muted">
            {d.meta.ip}
          </div>
        )}
      </div>
    </div>
  );
}

function PolicyCard({ d }: { d: RenderData }) {
  return (
    <div className={`${shellClass(d)} gap-2 px-2.5 py-1.5`}>
      <Handles />
      <span className="relative flex h-[7px] w-[7px] shrink-0">
        <span className="absolute inline-flex h-full w-full rounded-full bg-oz2-acc opacity-30" />
        <span className="relative inline-flex h-[7px] w-[7px] rounded-full bg-oz2-acc" />
      </span>
      <span className="min-w-0 flex-1 truncate text-[11.5px] font-medium text-oz2-text">
        {d.label}
      </span>
      {d.meta.port && (
        <span
          className="font-mono shrink-0 rounded-[5px] border border-oz2-border-soft
            bg-oz2-bg-sunken px-1.5 py-0.5 text-[9.5px] uppercase text-oz2-text-muted"
        >
          {d.meta.port}
        </span>
      )}
    </div>
  );
}

function ResourceCard({ d }: { d: RenderData }) {
  const isPeerKind = d.meta.resourceKind === "peer" || d.kind === "group";
  const Icon = isPeerKind ? (d.kind === "group" ? Users : Shield) : Server;
  return (
    <div className={`${shellClass(d)} gap-2.5 px-3 py-2`}>
      <Handles />
      <div className={`${TILE} h-[26px] w-[26px]`}>
        <Icon className="h-3.5 w-3.5" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-[12.5px] font-medium text-oz2-text">
          {d.label}
        </div>
        {d.meta.sub && (
          <div className="font-mono mt-0.5 truncate text-[10.5px] text-oz2-text-muted">
            {d.meta.sub}
          </div>
        )}
      </div>
    </div>
  );
}

export function CCNode({ data }: NodeProps) {
  // xyflow types node data as Record<string, unknown>; this is the
  // third-party boundary (CLAUDE.md sanctioned `as` exception).
  const d = data as unknown as RenderData;
  if (d.kind === "focus") {
    // network focus = the resource sitting on the right; render it as
    // a resource card so the inverse fan-in reads correctly.
    if (d.column === "focus" && (d.meta.resourceKind || d.meta.sub) && !d.meta.email)
      return <ResourceCard d={d} />;
    return <FocusCard d={d} />;
  }
  if (d.kind === "policy") return <PolicyCard d={d} />;
  if (d.kind === "peer" || d.kind === "user") return <PeerCard d={d} />;
  return <ResourceCard d={d} />;
}

export const CC_NODE_TYPES = { cc: CCNode };
