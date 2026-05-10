"use client";

import classNames from "classnames";
import React, { forwardRef } from "react";

// OzInput — v2 paint single-line input. Mirrors the handoff's
// `.oz-input` shape (34px tall, 10px radius, gap-2, 12px x-padding,
// border-soft + surface) and embeds the native <input> bare so the
// caller can hand off any input HTML prop unchanged.
//
// Two optional slots: `prefix` (icon-sized, faint, left) and `suffix`
// (kbd badge / chevron / icon, right). Both render inside the same
// wrapper so they share the rounded border.
//
// Token spec: design_handoff_openzro_dashboard/design/tokens.css `.oz-input`.

export interface OzInputProps
  extends Omit<React.InputHTMLAttributes<HTMLInputElement>, "prefix"> {
  prefix?: React.ReactNode;
  suffix?: React.ReactNode;
  /** Override the wrapper className (border / bg / etc.). */
  wrapperClassName?: string;
  /** Render mono-spaced text in the inner input (e.g. tokens, IDs). */
  mono?: boolean;
}

const OzInput = forwardRef<HTMLInputElement, OzInputProps>(
  (
    {
      prefix,
      suffix,
      className,
      wrapperClassName,
      mono,
      readOnly,
      disabled,
      ...props
    },
    ref,
  ) => {
    return (
      <div
        className={classNames(
          "inline-flex h-[34px] w-full items-center gap-2 rounded-oz2-input border border-oz2-border px-3 text-[13px] text-oz2-text transition-colors",
          // Read-only sits on the sunken surface so it visually
          // reads as a snapshot, not an editable field.
          readOnly ? "bg-oz2-bg-sunken" : "bg-oz2-surface",
          // Hover/focus tinting only when the field is interactive.
          !disabled &&
            !readOnly &&
            "hover:border-oz2-border-strong focus-within:border-oz2-acc focus-within:ring-2 focus-within:ring-oz2-acc/30",
          disabled && "cursor-not-allowed opacity-60",
          wrapperClassName,
        )}
      >
        {prefix && (
          <span className="flex shrink-0 items-center text-oz2-text-faint">
            {prefix}
          </span>
        )}
        <input
          ref={ref}
          readOnly={readOnly}
          disabled={disabled}
          className={classNames(
            "h-full w-full border-0 bg-transparent text-[13px] text-inherit outline-none placeholder:text-oz2-text-faint disabled:cursor-not-allowed",
            mono && "font-mono",
            className,
          )}
          {...props}
        />
        {suffix && (
          <span className="flex shrink-0 items-center text-oz2-text-faint">
            {suffix}
          </span>
        )}
      </div>
    );
  },
);

OzInput.displayName = "OzInput";

export default OzInput;
