"use client";

import classNames from "classnames";
import React from "react";

// v2 theme toggle — paired circular icon buttons (sun + moon) sitting
// side-by-side on the topbar. Active mode is the icon rendered in
// `text-oz2-text` weight; the inactive sibling fades to text-faint.
// Click the inactive one to flip the theme; clicking the active one
// is a no-op.
//
// Why not a slide-pill: the previous design used a 52×26 track with
// a sliding white thumb, but the thumb's footprint covered the active
// icon's position on the track, leaving the affordance visually
// blank in both themes. Two separate circular buttons stay legible
// and read as discrete affordances.

export interface OzThemeToggleProps {
  theme: "light" | "dark";
  onToggle: () => void;
  className?: string;
}

const OzThemeToggle = ({ theme, onToggle, className }: OzThemeToggleProps) => {
  const isDark = theme === "dark";

  return (
    <div
      role="radiogroup"
      aria-label="Theme"
      className={classNames("inline-flex items-center gap-1", className)}
    >
      <ThemeIconButton
        active={!isDark}
        label="Switch to light theme"
        onClick={isDark ? onToggle : undefined}
      >
        {/* sun */}
        <svg
          viewBox="0 0 24 24"
          width={13}
          height={13}
          fill="none"
          stroke="currentColor"
          strokeWidth={1.7}
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <circle cx={12} cy={12} r={4} />
          <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41" />
        </svg>
      </ThemeIconButton>
      <ThemeIconButton
        active={isDark}
        label="Switch to dark theme"
        onClick={isDark ? undefined : onToggle}
      >
        {/* moon */}
        <svg
          viewBox="0 0 24 24"
          width={13}
          height={13}
          fill="none"
          stroke="currentColor"
          strokeWidth={1.7}
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
        </svg>
      </ThemeIconButton>
    </div>
  );
};

function ThemeIconButton({
  active,
  label,
  onClick,
  children,
}: {
  active: boolean;
  label: string;
  onClick: (() => void) | undefined;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={active}
      aria-label={label}
      onClick={onClick}
      tabIndex={active ? 0 : -1}
      className={classNames(
        "grid h-7 w-7 shrink-0 place-items-center rounded-full border transition-colors",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-oz2-acc focus-visible:ring-offset-2 focus-visible:ring-offset-oz2-bg",
        active
          ? // Active: subtle filled chip in text-color so the icon
            // reads "this is the current mode".
            "border-oz2-border bg-oz2-bg-sunken text-oz2-text"
          : // Inactive: transparent + faded glyph + hover lift to invite
            // clicking. Cursor only when actually clickable.
            "cursor-pointer border-oz2-border-soft bg-transparent text-oz2-text-faint hover:border-oz2-border hover:bg-oz2-hover hover:text-oz2-text-2",
      )}
      style={!onClick ? { cursor: "default" } : undefined}
    >
      {children}
    </button>
  );
}

export default OzThemeToggle;
