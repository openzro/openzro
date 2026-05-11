"use client";

import * as LabelPrimitive from "@radix-ui/react-label";
import classNames from "classnames";
import * as React from "react";

// OzLabel + helper text components — handoff Forms.html "Shared label
// vocabulary":
//   .label    — 12.5px / 500, --text-2
//   .req      — required mark in --err
//   .opt      — "opcional" mono 10.5px --text-faint
//   .help     — 12px --text-muted, line-height 1.45
//   .err      — 12px --err with inline alert icon
//
// Built on Radix LabelPrimitive so `htmlFor` ↔ form-control association
// works the same as the legacy `Label`.

export interface OzLabelProps
  extends React.ComponentPropsWithoutRef<typeof LabelPrimitive.Root> {
  required?: boolean;
  optional?: boolean;
  /** Optional helper rendered next to the label (e.g. inline help icon). */
  trailing?: React.ReactNode;
}

const OzLabel = React.forwardRef<
  React.ElementRef<typeof LabelPrimitive.Root>,
  OzLabelProps
>(({ className, required, optional, trailing, children, ...props }, ref) => (
  <LabelPrimitive.Root
    ref={ref}
    className={classNames(
      "mb-1 inline-flex select-none items-center gap-1.5 text-[12.5px] font-medium leading-none text-oz2-text-2",
      "peer-disabled:cursor-not-allowed peer-disabled:opacity-70",
      className,
    )}
    {...props}
  >
    <span className="inline-flex items-center gap-1.5">
      {children}
      {required && (
        <span aria-hidden="true" className="text-oz2-err">
          *
        </span>
      )}
      {optional && (
        <span className="font-mono text-[10.5px] uppercase tracking-[0.05em] text-oz2-text-faint">
          opcional
        </span>
      )}
    </span>
    {trailing && (
      <span className="ml-auto inline-flex items-center text-oz2-text-faint">
        {trailing}
      </span>
    )}
  </LabelPrimitive.Root>
));
OzLabel.displayName = "OzLabel";

// One-line guidance below a field. Use under inputs / textareas / selects.
export function OzHelpText({
  className,
  ...props
}: React.HTMLAttributes<HTMLParagraphElement>) {
  return (
    <p
      className={classNames(
        "text-[12px] leading-[1.45] text-oz2-text-muted",
        className,
      )}
      {...props}
    />
  );
}

// Validation message that replaces OzHelpText when the field is invalid.
// Caller can include an inline icon as children alongside the message.
export function OzErrorText({
  className,
  ...props
}: React.HTMLAttributes<HTMLParagraphElement>) {
  return (
    <p
      className={classNames(
        "inline-flex items-center gap-1.5 text-[12px] leading-[1.45] text-oz2-err",
        className,
      )}
      {...props}
    />
  );
}

export default OzLabel;
