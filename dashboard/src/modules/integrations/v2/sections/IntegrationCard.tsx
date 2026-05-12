"use client";

import { Pencil, Trash2 } from "lucide-react";
import React from "react";
import OzCard from "@/components/v2/OzCard";

// IntegrationCard — shared visual primitive for Flow Exports /
// Activity Streamer / MDM-EDR sections. Mirrors the handoff
// IntegrationsScreen card pattern (40px gradient logo, name +
// status dot, sub-line, action row) without buying into the
// "Connect / Manage" marketplace metaphor — our destinations
// already exist when the card renders, so the actions are
// Manage (edit) + Delete.

interface Props {
  // Gradient color base for the 40×40 logo square.
  color: string;
  // Logo glyph — usually a brand letter or icon.
  logo: React.ReactNode;
  // Primary line — the destination's user-given `name`.
  name: string;
  // Type label (e.g. "Elastic", "Datadog", "S3").
  typeLabel: string;
  // Endpoint / target hint (mono, muted).
  endpoint?: string;
  // Tail of the right column — usually a "Template: custom" or
  // similar small annotation. Optional.
  meta?: React.ReactNode;
  enabled: boolean;
  onEdit: () => void;
  onDelete: () => void;
}

export default function IntegrationCard({
  color,
  logo,
  name,
  typeLabel,
  endpoint,
  meta,
  enabled,
  onEdit,
  onDelete,
}: Props) {
  return (
    <OzCard className="flex items-start gap-3 p-4">
      <span
        aria-hidden
        className="grid h-10 w-10 flex-shrink-0 place-items-center rounded-[10px] font-mono text-[16px] font-bold text-white shadow-oz2-sm"
        style={{
          background: `linear-gradient(135deg, ${color}, color-mix(in oklab, ${color} 60%, #000))`,
        }}
      >
        {logo}
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-[14px] font-semibold text-oz2-text">
            {name}
          </span>
          <span
            aria-hidden
            className={
              "h-1.5 w-1.5 shrink-0 rounded-full " +
              (enabled ? "bg-oz2-ok" : "bg-oz2-text-faint")
            }
            title={enabled ? "Enabled" : "Disabled"}
          />
        </div>
        <div className="text-[12px] text-oz2-text-muted">
          {typeLabel}
          {endpoint && (
            <>
              <span className="mx-1.5 text-oz2-text-faint">·</span>
              <span className="font-mono text-[11px] text-oz2-text-faint">
                {endpoint}
              </span>
            </>
          )}
        </div>
        {meta}
        <div className="mt-2 flex items-center gap-1.5">
          <button
            type="button"
            onClick={onEdit}
            className="inline-flex h-7 items-center gap-1.5 rounded-[8px] border border-oz2-border bg-transparent px-2.5 text-[12.5px] font-medium text-oz2-text-2 transition-colors hover:border-oz2-border-strong hover:bg-oz2-hover"
          >
            <Pencil size={12} />
            Manage
          </button>
          <button
            type="button"
            onClick={onDelete}
            aria-label={`Delete ${name}`}
            className="grid h-7 w-7 place-items-center rounded-[8px] border border-oz2-border bg-transparent text-oz2-text-faint transition-colors hover:border-oz2-err hover:bg-oz2-err-bg hover:text-oz2-err"
          >
            <Trash2 size={13} />
          </button>
        </div>
      </div>
    </OzCard>
  );
}
