"use client";

import * as CheckboxPrimitive from "@radix-ui/react-checkbox";
import classNames from "classnames";
import { Check } from "lucide-react";
import * as React from "react";

// OzCheckbox — v2 paint over Radix Checkbox. Drop-in replacement for
// the legacy components/Checkbox.tsx with the same prop surface — the
// underlying Radix primitive keeps keyboard nav, indeterminate state,
// and accessibility unchanged. Only the visual changes: oz2-acc fill
// when checked, oz2-border outline at rest, oz2-acc focus ring.

const OzCheckbox = React.forwardRef<
  React.ElementRef<typeof CheckboxPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof CheckboxPrimitive.Root>
>(({ className, ...props }, ref) => (
  <CheckboxPrimitive.Root
    ref={ref}
    className={classNames(
      "peer grid h-[18px] w-[18px] shrink-0 place-items-center rounded-[5px] border transition-colors",
      "border-oz2-border-strong bg-oz2-surface text-oz2-text-on-acc",
      "hover:border-oz2-acc",
      "data-[state=checked]:border-oz2-acc data-[state=checked]:bg-oz2-acc",
      "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-oz2-acc/40 focus-visible:ring-offset-2 focus-visible:ring-offset-oz2-bg",
      "disabled:cursor-not-allowed disabled:opacity-50",
      className,
    )}
    {...props}
  >
    <CheckboxPrimitive.Indicator className="flex items-center justify-center">
      <Check size={12} strokeWidth={3} />
    </CheckboxPrimitive.Indicator>
  </CheckboxPrimitive.Root>
));
OzCheckbox.displayName = CheckboxPrimitive.Root.displayName;

export default OzCheckbox;
