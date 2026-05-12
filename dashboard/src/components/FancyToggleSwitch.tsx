"use client";

import { cn } from "@utils/helpers";
import { cva, VariantProps } from "class-variance-authority";
import React from "react";
import OzSwitch from "@/components/v2/OzSwitch";

// FancyToggleSwitch — v2 paint. Public API is unchanged so existing
// call sites (RouteModal, NetworkResourceModal, posture checks, etc.)
// keep working without a rename pass. Internally we drop the legacy
// Label / HelpText / ToggleSwitch trio and use OzSwitch + the
// oz2-* tokens that match the rest of the modal surfaces.

export const fancyToggleSwitchVariants = cva([], {
  variants: {
    variant: {
      default: ["px-5 py-4 border rounded-oz2-card"],
      blank: null,
    },
    state: {
      true: null,
      false: null,
    },
  },
  compoundVariants: [
    {
      variant: "default",
      state: true,
      className: ["border-oz2-border-strong bg-oz2-surface"],
    },
    {
      variant: "default",
      state: false,
      className: [
        "border-oz2-border bg-oz2-bg-sunken/40 hover:bg-oz2-bg-sunken/70",
      ],
    },
  ],
});

export type FancyToggleSwitchVariants = VariantProps<
  typeof fancyToggleSwitchVariants
>;

interface Props extends FancyToggleSwitchVariants {
  value: boolean;
  onChange: (value: boolean) => void;
  helpText?: React.ReactNode;
  label?: React.ReactNode;
  children?: React.ReactNode;
  disabled?: boolean;
  dataCy?: string;
  className?: string;
}

export default function FancyToggleSwitch({
  value,
  onChange,
  helpText,
  label,
  children,
  disabled = false,
  dataCy,
  className,
  variant = "default",
}: Readonly<Props>) {
  const handleToggle = () => {
    if (disabled) return;
    onChange(!value);
  };

  const handleKeyDown = (event: React.KeyboardEvent) => {
    if (disabled) return;
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      handleToggle();
    }
  };

  return (
    <div
      onClick={handleToggle}
      onKeyDown={handleKeyDown}
      tabIndex={-1}
      role={"switch"}
      aria-checked={value}
      className={cn(
        "relative z-[1] inline-block w-full cursor-pointer text-left transition-colors",
        disabled && "pointer-events-none opacity-50",
        fancyToggleSwitchVariants({ variant, state: value }),
        className,
      )}
    >
      <div className={"flex items-start justify-between gap-6"}>
        <div className={"min-w-0 flex-1"}>
          <div
            className={
              "inline-flex items-center gap-2 text-[12.5px] font-medium text-oz2-text-2"
            }
          >
            {label}
          </div>
          {helpText && (
            <div
              className={
                "mt-1 text-[12px] leading-[1.45] text-oz2-text-muted"
              }
            >
              {helpText}
            </div>
          )}
        </div>
        <div className={"mt-1 shrink-0"}>
          <OzSwitch
            checked={value}
            onCheckedChange={onChange}
            data-cy={dataCy}
            disabled={disabled}
          />
        </div>
      </div>
      <div>{children && value ? children : null}</div>
    </div>
  );
}
