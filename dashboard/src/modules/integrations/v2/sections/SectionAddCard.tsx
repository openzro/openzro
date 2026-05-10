"use client";

import { PlusCircle } from "lucide-react";
import React from "react";

// SectionAddCard — trailing dashed card that sits at the end of
// each section's grid as a contextual "create" affordance.
// Mirrors the dimensions of IntegrationCard so grid alignment
// holds; dashed border + ghost paint signals it's the create
// affordance instead of an existing instance.
//
// Click triggers the same openCreate flow the topbar Add button
// uses — both entry points share the same modal mount in the
// owning section.

interface Props {
  /** Big label (e.g. "Add destination", "Add exporter"). */
  label: string;
  /** Small description below the label. */
  description: string;
  onClick: () => void;
}

export default function SectionAddCard({ label, description, onClick }: Props) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="group flex min-h-[120px] flex-col items-center justify-center gap-2 rounded-oz2-card border-2 border-dashed border-oz2-border bg-transparent p-4 text-oz2-text-muted transition-colors hover:border-oz2-acc hover:bg-oz2-acc-soft hover:text-oz2-acc-text"
    >
      <span
        aria-hidden
        className="grid h-10 w-10 place-items-center rounded-[10px] border border-dashed border-oz2-border-strong text-oz2-text-faint transition-colors group-hover:border-oz2-acc group-hover:text-oz2-acc-text"
      >
        <PlusCircle size={18} />
      </span>
      <span className="text-[13.5px] font-medium">{label}</span>
      <span className="text-[11.5px] text-oz2-text-faint">{description}</span>
    </button>
  );
}
