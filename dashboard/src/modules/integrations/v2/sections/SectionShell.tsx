"use client";

import { PlusCircle } from "lucide-react";
import React from "react";
import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";

// SectionShell — shared chrome around a Flow / Activity / MDM
// section. Wraps the section's intro paragraph + add CTA + grid
// of cards. Empty state and loading state are rendered inline.

interface Props {
  description: React.ReactNode;
  hint?: React.ReactNode;
  /** Add-button label (e.g. "Add destination", "Add provider"). */
  addLabel: string;
  /** Disabled state for the add CTA. */
  addDisabled?: boolean;
  onAdd: () => void;
  isLoading: boolean;
  isEmpty: boolean;
  emptyMessage: string;
  children: React.ReactNode;
}

export default function SectionShell({
  description,
  hint,
  addLabel,
  addDisabled,
  onAdd,
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

      <div className="flex justify-end">
        <OzButton
          variant="primary"
          type="button"
          onClick={onAdd}
          disabled={addDisabled}
        >
          <PlusCircle size={14} />
          {addLabel}
        </OzButton>
      </div>

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
