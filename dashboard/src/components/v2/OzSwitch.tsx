"use client";

import * as SwitchPrimitives from "@radix-ui/react-switch";
import { cva, VariantProps } from "class-variance-authority";
import classNames from "classnames";
import * as React from "react";

// v2 switch primitive — handoff Forms.html §07.
// Default: 36×20 track, 16×16 thumb, translates 16px on.
// SM:      28×16 track, 12×12 thumb, translates 12px on.
// Track off = --oz2-border-strong, on = --oz2-acc. Thumb is white
// with a subtle drop shadow. Disabled = 50% opacity.

const trackVariants = cva(
  [
    "peer inline-flex shrink-0 cursor-pointer items-center rounded-full",
    "transition-colors duration-150",
    "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-oz2-acc focus-visible:ring-offset-2 focus-visible:ring-offset-oz2-bg",
    "disabled:cursor-not-allowed disabled:opacity-50",
    "bg-oz2-border-strong data-[state=checked]:bg-oz2-acc",
  ],
  {
    variants: {
      size: {
        default: "h-5 w-9",
        sm: "h-4 w-7",
      },
    },
    defaultVariants: {
      size: "default",
    },
  },
);

const thumbVariants = cva(
  [
    "pointer-events-none block rounded-full bg-white ring-0",
    "transition-transform duration-150",
    "shadow-[0_1px_2px_rgba(0,0,0,0.18)]",
    "data-[state=unchecked]:translate-x-0.5",
  ],
  {
    variants: {
      size: {
        default: "h-4 w-4 data-[state=checked]:translate-x-[18px]",
        sm: "h-3 w-3 data-[state=checked]:translate-x-[14px]",
      },
    },
    defaultVariants: {
      size: "default",
    },
  },
);

type SwitchVariants = VariantProps<typeof trackVariants>;

export interface OzSwitchProps
  extends React.ComponentPropsWithoutRef<typeof SwitchPrimitives.Root>,
    SwitchVariants {}

const OzSwitch = React.forwardRef<
  React.ElementRef<typeof SwitchPrimitives.Root>,
  OzSwitchProps
>(({ className, size = "default", onClick, ...props }, ref) => (
  <SwitchPrimitives.Root
    ref={ref}
    className={classNames(trackVariants({ size }), className)}
    onClick={(e) => {
      // Row-level click handlers (accordion expand) shouldn't fire
      // when the user is toggling the switch.
      e.stopPropagation();
      onClick?.(e);
    }}
    {...props}
  >
    <SwitchPrimitives.Thumb className={thumbVariants({ size })} />
  </SwitchPrimitives.Root>
));
OzSwitch.displayName = "OzSwitch";

export default OzSwitch;
