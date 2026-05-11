"use client";

import * as SelectPrimitive from "@radix-ui/react-select";
import classNames from "classnames";
import { Check, ChevronDown } from "lucide-react";
import * as React from "react";

// OzSelect — v2 paint over Radix Select primitives. Drop-in
// replacement for the legacy components/Select.tsx with the same
// export surface: Root + Value + Trigger + Content + Item + Group +
// Label + Separator. Only the trigger/content/item styling changes —
// the underlying primitives stay Radix so keyboard nav,
// disabled-handling, and portal semantics are unchanged.
//
// Trigger matches OzInput dimensions (34px tall, 10px radius, surface
// fill, border-soft outline) so a form row with one input + one select
// reads as a single horizontal band.

const OzSelect = SelectPrimitive.Root;
const OzSelectGroup = SelectPrimitive.Group;
const OzSelectValue = SelectPrimitive.Value;

const OzSelectTrigger = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Trigger>
>(({ className, children, ...props }, ref) => (
  <SelectPrimitive.Trigger
    ref={ref}
    className={classNames(
      "inline-flex h-[34px] w-full items-center justify-between gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 text-[13px] text-oz2-text transition-colors",
      "hover:border-oz2-border-strong",
      // focus-visible (not focus) — Radix Select keeps focus on the
      // trigger while the popover is open, so `focus:` left a sticky
      // violet ring around the trigger after every mouse click. The
      // keyboard-only ring is preserved via focus-visible.
      "outline-none focus:outline-none focus-visible:outline-none focus-visible:border-oz2-acc focus-visible:ring-2 focus-visible:ring-oz2-acc/30",
      "disabled:cursor-not-allowed disabled:opacity-60",
      "data-[placeholder]:text-oz2-text-faint",
      className,
    )}
    {...props}
  >
    {children}
    <SelectPrimitive.Icon asChild>
      <ChevronDown className="h-4 w-4 text-oz2-text-faint" />
    </SelectPrimitive.Icon>
  </SelectPrimitive.Trigger>
));
OzSelectTrigger.displayName = SelectPrimitive.Trigger.displayName;

const OzSelectContent = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Content>
>(({ className, children, position = "popper", ...props }, ref) => (
  <SelectPrimitive.Portal>
    <SelectPrimitive.Content
      ref={ref}
      className={classNames(
        // Radix moves focus to Content on open; without arbitrary
        // `[outline:none]` (real `outline: none`) the browser's
        // default focus outline (blue, square-cornered) bleeds past
        // the rounded card. Tailwind's `outline-none` is actually
        // `outline: 2px solid transparent` and doesn't reliably
        // suppress the user-agent ring on every browser/state.
        "relative z-50 min-w-[8rem] overflow-hidden rounded-oz2-card border border-oz2-border bg-oz2-bg-elev text-oz2-text shadow-oz2-md [outline:none] focus:[outline:none] focus-visible:[outline:none]",
        "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2",
        position === "popper" &&
          "data-[side=bottom]:translate-y-1 data-[side=left]:-translate-x-1 data-[side=right]:translate-x-1 data-[side=top]:-translate-y-1",
        className,
      )}
      position={position}
      {...props}
    >
      <SelectPrimitive.Viewport
        className={classNames(
          "p-1",
          position === "popper" &&
            "h-[var(--radix-select-trigger-height)] w-full min-w-[var(--radix-select-trigger-width)]",
        )}
      >
        {children}
      </SelectPrimitive.Viewport>
    </SelectPrimitive.Content>
  </SelectPrimitive.Portal>
));
OzSelectContent.displayName = SelectPrimitive.Content.displayName;

const OzSelectLabel = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Label>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Label>
>(({ className, ...props }, ref) => (
  <SelectPrimitive.Label
    ref={ref}
    className={classNames(
      "px-3 py-1.5 font-mono text-[10.5px] uppercase tracking-[0.06em] text-oz2-text-faint",
      className,
    )}
    {...props}
  />
));
OzSelectLabel.displayName = SelectPrimitive.Label.displayName;

const OzSelectItem = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Item>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Item>
>(({ className, children, ...props }, ref) => (
  <SelectPrimitive.Item
    ref={ref}
    className={classNames(
      "relative flex w-full cursor-pointer select-none items-center rounded-[6px] py-1.5 pl-8 pr-3 text-[13px] text-oz2-text outline-none transition-colors",
      "focus:bg-oz2-hover focus:text-oz2-text",
      "data-[state=checked]:text-oz2-acc-text",
      "data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
      className,
    )}
    {...props}
  >
    <span className="absolute left-2 flex h-3.5 w-3.5 items-center justify-center">
      <SelectPrimitive.ItemIndicator>
        <Check className="h-3.5 w-3.5" />
      </SelectPrimitive.ItemIndicator>
    </span>
    <SelectPrimitive.ItemText>{children}</SelectPrimitive.ItemText>
  </SelectPrimitive.Item>
));
OzSelectItem.displayName = SelectPrimitive.Item.displayName;

const OzSelectSeparator = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Separator>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Separator>
>(({ className, ...props }, ref) => (
  <SelectPrimitive.Separator
    ref={ref}
    className={classNames("-mx-1 my-1 h-px bg-oz2-border-soft", className)}
    {...props}
  />
));
OzSelectSeparator.displayName = SelectPrimitive.Separator.displayName;

export {
  OzSelect,
  OzSelectContent,
  OzSelectGroup,
  OzSelectItem,
  OzSelectLabel,
  OzSelectSeparator,
  OzSelectTrigger,
  OzSelectValue,
};
