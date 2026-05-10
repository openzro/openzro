"use client";

import classNames from "classnames";
import React from "react";

// v2 topbar — 56px sticky bar living above content. Left: breadcrumb
// or page title. Right: actions, theme toggle, bell, avatar.
//
// Reference: design_handoff_openzro_dashboard/design/shell.jsx.

export interface OzTopbarProps {
  /**
   * Left slot. Typically a breadcrumb (chevron-separated) — last
   * segment in `text` color, ancestors in `text-muted`.
   */
  left?: React.ReactNode;
  /**
   * Right slot — page-specific actions (Save / Add peer / etc),
   * placed BEFORE the theme toggle + notifications + avatar block
   * which the consumer composes here too.
   */
  right?: React.ReactNode;
}

const OzTopbar = ({ left, right }: OzTopbarProps) => {
  return (
    <div className="flex h-full items-center justify-between gap-4 px-6">
      <div className="min-w-0 flex-1">{left}</div>
      <div className="flex shrink-0 items-center gap-3">{right}</div>
    </div>
  );
};

export default OzTopbar;

// ─── Helper: breadcrumb composer ────────────────────────────────────────────
// Common left-slot content. Render directly in Topbar's `left` prop
// to avoid passing a structured array.

export interface OzBreadcrumbSegment {
  label: string;
  onClick?: () => void;
}

export const OzBreadcrumb = ({
  segments,
}: {
  segments: OzBreadcrumbSegment[];
}) => {
  return (
    <ol className="flex items-center gap-1.5 text-[13px]">
      {segments.map((seg, i) => {
        const isLast = i === segments.length - 1;
        return (
          <React.Fragment key={`${seg.label}-${i}`}>
            <li
              className={classNames(
                isLast
                  ? "font-semibold text-oz2-text"
                  : "font-medium text-oz2-text-muted",
              )}
            >
              {seg.onClick ? (
                <button
                  type="button"
                  onClick={seg.onClick}
                  className="hover:text-oz2-text"
                >
                  {seg.label}
                </button>
              ) : (
                seg.label
              )}
            </li>
            {!isLast && (
              <li
                aria-hidden="true"
                className="select-none text-oz2-text-faint"
              >
                /
              </li>
            )}
          </React.Fragment>
        );
      })}
    </ol>
  );
};
