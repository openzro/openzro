"use client";

import classNames from "classnames";
import React from "react";

// OzSettingsCard — section block for /settings/*. Header carries title
// + optional sub on the left and a free-form slot on the right (for an
// Add CTA or similar); body stacks children vertically with 14px gaps.
// `danger` variant flips the border + title to the err palette so
// destructive sections (Danger Zone) read as a distinct gravity zone.
//
// Visual mirror of the handoff's S5Card (screens-5.jsx).

interface Props {
  title: React.ReactNode;
  sub?: React.ReactNode;
  /** Right-aligned slot in the header (typically a CTA button). */
  right?: React.ReactNode;
  /** Flips border + title to err palette for destructive sections. */
  danger?: boolean;
  className?: string;
  children: React.ReactNode;
}

export default function OzSettingsCard({
  title,
  sub,
  right,
  danger,
  className,
  children,
}: Readonly<Props>) {
  return (
    <section
      className={classNames(
        "rounded-oz2-card border bg-oz2-surface px-5 py-[18px] shadow-oz2-sm",
        danger ? "border-oz2-err/40" : "border-oz2-border",
        className,
      )}
    >
      <header className="mb-[14px] flex items-baseline justify-between gap-3.5">
        <div className="min-w-0">
          <h3
            className={classNames(
              "text-[14px] font-semibold",
              danger ? "text-oz2-err" : "text-oz2-text",
            )}
          >
            {title}
          </h3>
          {sub && (
            <p className="mt-[3px] text-[12.5px] leading-[1.5] text-oz2-text-muted">
              {sub}
            </p>
          )}
        </div>
        {right && <div className="shrink-0">{right}</div>}
      </header>
      <div className="flex flex-col gap-[14px]">{children}</div>
    </section>
  );
}
