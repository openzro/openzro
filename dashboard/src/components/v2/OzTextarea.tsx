"use client";

import classNames from "classnames";
import React, { forwardRef } from "react";

// OzTextarea — v2 paint multiline input. Handoff Forms.html §02:
// vertical resize only, min-height 96px, 10px radius, same padding /
// border as the input. Optional `maxCount` renders a right-aligned
// `current / max` counter in mono, flipping to --err when over.
//
// Token spec: design_handoff_openzro_dashboard/design/tokens.css `.oz-input`.

export interface OzTextareaProps
  extends React.TextareaHTMLAttributes<HTMLTextAreaElement> {
  /** Error message; non-empty switches the border to oz2-err and shows the message below. */
  error?: string;
  /** Max char count; renders `count / max` mono counter under the field. */
  maxCount?: number;
}

const OzTextarea = forwardRef<HTMLTextAreaElement, OzTextareaProps>(
  (
    {
      className,
      readOnly,
      disabled,
      error,
      maxCount,
      value,
      defaultValue,
      ...props
    },
    ref,
  ) => {
    const hasError = !!error && error.length > 0;
    const currentLength = typeof value === "string"
      ? value.length
      : typeof defaultValue === "string"
        ? defaultValue.length
        : 0;
    const over = typeof maxCount === "number" && currentLength > maxCount;

    return (
      <div className="flex w-full flex-col gap-1">
        <textarea
          ref={ref}
          readOnly={readOnly}
          disabled={disabled}
          value={value}
          defaultValue={defaultValue}
          aria-invalid={hasError || undefined}
          className={classNames(
            "block min-h-[96px] w-full resize-y rounded-oz2-input border px-3 py-2.5 text-[13px] leading-[1.5] text-oz2-text outline-none transition-colors",
            "placeholder:text-oz2-text-faint",
            hasError ? "border-oz2-err" : "border-oz2-border",
            readOnly ? "bg-oz2-bg-sunken" : "bg-oz2-surface",
            !disabled &&
              !readOnly &&
              !hasError &&
              "hover:border-oz2-border-strong focus:border-oz2-acc focus:ring-2 focus:ring-oz2-acc/30",
            !disabled &&
              !readOnly &&
              hasError &&
              "focus:ring-2 focus:ring-oz2-err/30",
            disabled && "cursor-not-allowed opacity-60",
            className,
          )}
          {...props}
        />
        {(hasError || typeof maxCount === "number") && (
          <div className="flex items-center justify-between gap-2">
            {hasError ? (
              <p className="text-[11.5px] leading-[1.45] text-oz2-err">
                {error}
              </p>
            ) : (
              <span />
            )}
            {typeof maxCount === "number" && (
              <span
                className={classNames(
                  "font-mono text-[11px] tabular-nums",
                  over ? "text-oz2-err" : "text-oz2-text-faint",
                )}
              >
                {currentLength} / {maxCount}
              </span>
            )}
          </div>
        )}
      </div>
    );
  },
);

OzTextarea.displayName = "OzTextarea";

export default OzTextarea;
