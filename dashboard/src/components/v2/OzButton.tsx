"use client";

import { cva, VariantProps } from "class-variance-authority";
import classNames from "classnames";
import React, { forwardRef } from "react";

// v2 button primitive — Notion/Arc-flavored redesign.
// Three variants only (default / primary / ghost), one size (h:34).
// Token spec: design_handoff_openzro_dashboard/design/tokens.css `.oz-btn*`.
//
// Coexists with the legacy Button.tsx (11 variants, larger surface).
// Migrate screens one by one by swapping `Button` → `OzButton`.

export const ozButtonVariants = cva(
  [
    // Base — handoff: 34px height, 10px radius, gap-2 (8px), 13px/500.
    "inline-flex h-[34px] items-center justify-center gap-2 whitespace-nowrap",
    "rounded-oz2-input border px-[14px] text-[15px] font-medium",
    "transition-all duration-150 ease-out",
    "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-oz2-acc focus-visible:ring-offset-2 focus-visible:ring-offset-oz2-bg",
    "disabled:cursor-not-allowed disabled:opacity-50",
  ],
  {
    variants: {
      variant: {
        default: [
          "border-oz2-border bg-oz2-surface text-oz2-text",
          "hover:bg-oz2-hover hover:border-oz2-border-strong",
        ],
        primary: [
          "border-transparent bg-oz2-acc text-oz2-text-on-acc shadow-oz2-acc",
          "hover:bg-oz2-acc-hover",
        ],
        ghost: [
          "border-transparent bg-transparent text-oz2-text-2",
          "hover:bg-oz2-hover hover:text-oz2-text",
        ],
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

export type OzButtonVariants = VariantProps<typeof ozButtonVariants>;

export interface OzButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    OzButtonVariants {}

const OzButton = forwardRef<HTMLButtonElement, OzButtonProps>(
  ({ variant, className, type = "button", ...props }, ref) => {
    return (
      <button
        type={type}
        ref={ref}
        className={classNames(ozButtonVariants({ variant }), className)}
        {...props}
      />
    );
  },
);

OzButton.displayName = "OzButton";

export default OzButton;
