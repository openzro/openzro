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
        {...props}
        className={cn(
          // outline-0 sets outline-width: 0px which kills the ring
          // regardless of outline-style / color (browser default
          // `:focus-visible { outline: ... }` only paints if width
          // is nonzero).
          "z-50 overflow-hidden outline-0 focus:outline-0 focus-visible:outline-0 focus-within:outline-0",
          "animate-in fade-in-0 zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95",
          "data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2",
          popoverVariants({ variant }),
          className,
        )}
        // Inline `outline: none` — Tailwind's `outline-none` is
        // actually `outline: 2px solid transparent` and the arbitrary
        // `[outline:none]` route was still letting the user-agent
        // focus ring paint through in some browser/state combos.
        // Inline style wins regardless of cascade or twMerge order
        // and writes a literal `outline: none`.
        style={{ outline: "none", ...(props.style ?? {}) }}
      />
    </PopoverPrimitive.Portal>
  ),
);
PopoverContent.displayName = PopoverPrimitive.Content.displayName;

export { Popover, PopoverContent, PopoverTrigger };
