"use client";

import classNames from "classnames";
import React from "react";

// OzSettingsField — labeled row for /settings/*. Grid 200px / 1fr:
// label + hint on the left, input/select/children on the right.
// Visual mirror of the handoff's S5Field (screens-5.jsx). Field
// stacks vertically below ~640px so dense settings pages stay
// legible on narrow viewports.

interface Props {
  label: React.ReactNode;
  /** Optional helper text below the label (small, faint). */
  hint?: React.ReactNode;
  /** Right-side control(s). */
  children: React.ReactNode;
  className?: string;
  /**
   * `htmlFor` to wire the label to a specific control. Optional —
   * many settings rows render composite controls (input + select)
   * where labelling the wrapper is more useful than any one input.
   */
  htmlFor?: string;
}

export default function OzSettingsField({
  label,
  hint,
  children,
  htmlFor,
  className,
}: Readonly<Props>) {
  return (
    <div
      className={classNames(
        "grid grid-cols-1 items-start gap-3 sm:grid-cols-[200px_minmax(0,1fr)] sm:gap-[14px]",
        className,
      )}
    >
      <div>
        <label
          htmlFor={htmlFor}
          className="block text-[13px] font-medium text-oz2-text-2"
        >
          {label}
        </label>
        {hint && (
          <p className="mt-[3px] text-[11.5px] leading-[1.45] text-oz2-text-faint">
            {hint}
          </p>
        )}
      </div>
      <div className="min-w-0">{children}</div>
    </div>
  );
}
