"use client";

import { cn } from "@utils/helpers";
import React, { forwardRef } from "react";

// ButtonGroup — v2 paint. Used as a segmented-toggle (Route Type
// picker in RouteModal, table-filter pills in PeersTable, etc.).
// Call sites pass `variant="tertiary"` for the selected segment and
// `variant="secondary"` for the rest. We treat any value other than
// "tertiary" as unselected — the legacy palette had several aliases
// (secondary, secondaryLighter, dropdown, white) that all rendered
// the same in this context.

type Props = {
  children: React.ReactNode;
  disabled?: boolean;
  className?: string;
};

function ButtonGroup({ children, disabled, className }: Props) {
  return (
    <div
      className={cn(
        "inline-flex shrink-0 items-center justify-center overflow-hidden rounded-oz2-input border border-oz2-border",
        disabled && "opacity-50",
        className,
      )}
    >
      {children}
    </div>
  );
}

interface ButtonGroupButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: string;
}

const ButtonGroupButton = forwardRef<HTMLButtonElement, ButtonGroupButtonProps>(
  (
    {
      className,
      variant,
      disabled,
      children,
      ...props
    }: ButtonGroupButtonProps,
    ref: React.ForwardedRef<HTMLButtonElement>,
  ) => {
    const selected = variant === "tertiary";
    return (
      <button
        ref={ref}
        type="button"
        disabled={disabled}
        className={cn(
          "inline-flex h-[36px] items-center justify-center gap-2 px-4 text-[13px] font-medium transition-colors",
          "border-l border-oz2-border first:border-l-0",
          "focus-visible:outline-none focus-visible:relative focus-visible:z-10 focus-visible:ring-2 focus-visible:ring-oz2-acc/40",
          "disabled:cursor-not-allowed disabled:opacity-50",
          selected
            ? "bg-oz2-acc-soft text-oz2-acc-text"
            : "bg-oz2-surface text-oz2-text-muted hover:bg-oz2-hover hover:text-oz2-text",
          className,
        )}
        {...props}
      >
        {children}
      </button>
    );
  },
);

ButtonGroupButton.displayName = "ButtonGroupButton";

ButtonGroup.Button = ButtonGroupButton;

export default ButtonGroup;
