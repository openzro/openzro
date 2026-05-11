"use client";

import * as PopoverPrimitive from "@radix-ui/react-popover";
import { cn } from "@utils/helpers";
import { cva, VariantProps } from "class-variance-authority";
import * as React from "react";

type PopoverVariants = VariantProps<typeof popoverVariants>;

// Variants paint with v2 tokens in both modes — the legacy
// `dark:border-nb-gray-800` token was a violet-tinted gray
// (rgb 64 62 96) that read as a stray "blue/violet" border around
// popovers when dark mode was active. oz2-border resolves to a
// subtle 10% violet rgba in dark, which sits flat against the
// elevated surface.
export const popoverVariants = cva([], {
  variants: {
    variant: {
      lighter: [
        "rounded-md border border-oz2-border bg-oz2-surface px-5 py-3 text-sm text-oz2-text shadow-oz2-md",
      ],
      dark: [
        "rounded-md border border-oz2-border bg-oz2-bg-elev px-5 py-3 text-sm text-oz2-text shadow-oz2-md",
      ],
    },
  },
});

const Popover = PopoverPrimitive.Root;

const PopoverTrigger = PopoverPrimitive.Trigger;

const PopoverContent = React.forwardRef<
  React.ElementRef<typeof PopoverPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof PopoverPrimitive.Content> &
    PopoverVariants
>(
  (
    {
      className,
      align = "center",
      sideOffset = 4,
      variant = "lighter",
      ...props
    },
    ref,
  ) => (
    <PopoverPrimitive.Portal>
      <PopoverPrimitive.Content
        ref={ref}
        align={align}
        sideOffset={sideOffset}
        className={cn(
          // Radix moves focus to Content on open; without an explicit
          // outline override the browser's default focus outline
          // (blue, square-cornered) wraps the popover and bleeds past
          // the rounded border. Tailwind's `outline-none` is actually
          // `outline: 2px solid transparent`, which doesn't reliably
          // suppress the user-agent ring on every browser/state. Use
          // arbitrary CSS `[outline:none]` so we write the real
          // `outline: none` directly. Inner controls own their own
          // focus styling.
          "z-50 overflow-hidden [outline:none] focus:[outline:none] focus-visible:[outline:none]",
          "animate-in fade-in-0 zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95",
          "data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2",
          popoverVariants({ variant }),
          className,
        )}
        {...props}
      />
    </PopoverPrimitive.Portal>
  ),
);
PopoverContent.displayName = PopoverPrimitive.Content.displayName;

export { Popover, PopoverContent, PopoverTrigger };
