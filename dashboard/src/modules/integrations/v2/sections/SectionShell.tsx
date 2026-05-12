"use client";

import React from "react";
import OzCard from "@/components/v2/OzCard";

// SectionShell — shared chrome around a Flow / Activity / MDM
// section. Wraps the section's intro paragraph + grid of cards.
// Add CTAs live in the V2 topbar slot via useV2TopbarRight at the
// individual section level (each section registers its own
// button; switching sub-tabs swaps the slot automatically).

interface Props {
  description: React.ReactNode;
  hint?: React.ReactNode;
  isLoading: boolean;
  isEmpty: boolean;
  emptyMessage: string;
  children: React.ReactNode;
}

export default function SectionShell({
  description,
  hint,
  isLoading,
  isEmpty,
  emptyMessage,
  children,
}: Props) {
  return (
    <div className="flex flex-col gap-4">
      <div className="text-[13.5px] leading-[1.55] text-oz2-text-muted">
        {description}
      </div>
      {hint && (
        <div className="text-[12.5px] leading-[1.5] text-oz2-text-faint">
          {hint}
        </div>
      )}

      {isLoading ? (
        <OzCard className="px-6 py-8 text-center text-[13px] text-oz2-text-muted">
          Loading…
        </OzCard>
      ) : isEmpty ? (
        <OzCard className="border-dashed px-6 py-10 text-center text-[13px] text-oz2-text-muted">
          {emptyMessage}
        </OzCard>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {children}
        </div>
      )}
    </div>
  );
}
