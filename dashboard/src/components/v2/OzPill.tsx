"use client";

import { cva, VariantProps } from "class-variance-authority";
import classNames from "classnames";
import React, { forwardRef } from "react";

// v2 pill primitive — used for status badges, group labels, type
// callouts. Token spec: design_handoff_openzro_dashboard/design/tokens.css
// `.oz-pill*`.
//
// Sizing: 11px mono, padding 3×9, radius 99 (full).
// Variants drop the border on colored states so the fill carries the
// status signal cleanly.

export const ozPillVariants = cva(
  [
    "inline-flex items-center gap-1.5 rounded-full border",
    "px-[9px] py-[3px] font-mono text-[11px] font-medium tracking-tight",
  ],
  {
    variants: {
      variant: {
        default: "border-oz2-border bg-oz2-bg-soft text-oz2-text-2",
        acc:     "border-transparent bg-oz2-acc-soft text-oz2-acc-text",
        ok:      "border-transparent bg-oz2-ok-bg text-oz2-ok",
        warn:    "border-transparent bg-oz2-warn-bg text-oz2-warn",
        err:     "border-transparent bg-oz2-err-bg text-oz2-err",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

export type OzPillVariants = VariantProps<typeof ozPillVariants>;

export interface OzPillProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    OzPillVariants {}

const OzPill = forwardRef<HTMLSpanElement, OzPillProps>(
  ({ variant, className, ...props }, ref) => {
    return (
      <span
        ref={ref}
        className={classNames(ozPillVariants({ variant }), className)}
        {...props}
      />
    );
  },
);

OzPill.displayName = "OzPill";

export default OzPill;
