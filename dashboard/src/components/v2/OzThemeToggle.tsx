"use client";

import classNames from "classnames";
import React from "react";

// v2 theme toggle — 52×26 outer track, 22×20 thumb that slides
// left↔right. Sun + moon icons absolutely-positioned at fixed
// offsets so they stay vertically centered with the thumb.
//
// Reference: design_handoff_openzro_dashboard/design/shell.jsx.
//
// Wires to whatever theme state the consumer owns. Common pattern:
// next-themes' useTheme + setTheme. Pass `theme` as the current
// resolved value and `onToggle` as the flip callback.

export interface OzThemeToggleProps {
  theme: "light" | "dark";
  onToggle: () => void;
  className?: string;
}

const OzThemeToggle = ({ theme, onToggle, className }: OzThemeToggleProps) => {
  const isDark = theme === "dark";
  return (
    <button
      type="button"
      role="switch"
      aria-checked={isDark}
      aria-label={`Switch to ${isDark ? "light" : "dark"} theme`}
      onClick={onToggle}
      className={classNames(
        // Track: zinc utilities give clear contrast against both
        // warm-paper light (#fbfaf7) and dark-violet (#0d091a) topbar
        // bg. The v2 surface tokens (bg-soft / bg-sunken / surface)
        // are intentionally close in hue for the calm-palette feel,
        // which left the toggle visually mushy in both themes. zinc
        // breaks out of that for this small but high-frequency
        // affordance only.
        "relative inline-flex h-[26px] w-[52px] items-center rounded-full border border-zinc-300 bg-zinc-200 transition-colors",
        "dark:border-zinc-600 dark:bg-zinc-700",
        "hover:border-zinc-400 dark:hover:border-zinc-500",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-oz2-acc focus-visible:ring-offset-2 focus-visible:ring-offset-oz2-bg",
        className,
      )}
    >
      {/* Sliding thumb with the active icon embedded inside — sun in
          light mode, moon in dark mode. The thumb covers the track
          area where a fixed icon would sit, so the previous design
          (separate spans at left/right edges) hid the active icon
          under the thumb. Putting the icon inside the thumb keeps
          it visible on the white surface and slides together. */}
      <span
        aria-hidden="true"
        className={classNames(
          "absolute top-[2px] grid h-[20px] w-[22px] place-items-center rounded-full bg-white shadow-md ring-1 ring-black/5 transition-[left] duration-200 ease-out",
          isDark ? "left-[28px] text-zinc-700" : "left-[2px] text-amber-600",
        )}
      >
        {isDark ? (
          <svg
            width={11}
            height={11}
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth={2}
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
          </svg>
        ) : (
          <svg
            width={11}
            height={11}
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth={2}
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <circle cx={12} cy={12} r={4} />
            <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41" />
          </svg>
        )}
      </span>
    </button>
  );
};

export default OzThemeToggle;
