"use client";

import classNames from "classnames";
import React from "react";

// v2 theme toggle — slide pill, 52×26 track with a sliding white
// thumb. Both sun + moon icons render on top of the thumb so the
// active glyph (the one the thumb sits under) reads as "dark text on
// white", and the inactive glyph reads faded against the colored
// track.
//
// The earlier round of this component had the icons rendered BEFORE
// the thumb in the DOM, so the thumb (later element, higher in
// stacking order) covered the active icon and the toggle looked
// blank. The fix is just DOM order — render thumb first, icons
// after, both sit on top.
//
// API: `theme` is the current resolved theme; `onToggle` flips it.

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
        // Track palette comes from v2 tokens so it sits naturally in
        // both surfaces:
        //   light → bg-oz2-bg-soft (#f3f1ec, warm beige tan from the
        //           sidebar palette) on top of warm-paper topbar
        //   dark  → bg-oz2-surface-2 (#1a1538, violet-lifted) against
        //           the very dark #0d091a topbar bg.
        // Border-strong gives the edge enough definition in both
        // modes (warmer beige in light, soft violet glow in dark).
        // The previous zinc-200/zinc-700 was high-contrast but cool
        // gray, which clashed with the warm + violet v2 palettes.
        "relative inline-flex h-[26px] w-[52px] items-center rounded-full border bg-oz2-bg-soft transition-colors dark:bg-oz2-surface-2",
        "border-oz2-border-strong hover:border-oz2-border-strong",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-oz2-acc focus-visible:ring-offset-2 focus-visible:ring-offset-oz2-bg",
        className,
      )}
    >
      {/* Sliding thumb FIRST in DOM so the icons below sit on top
          and the active glyph shows on the white thumb surface. */}
      <span
        aria-hidden="true"
        className={classNames(
          "absolute top-[2px] h-[20px] w-[22px] rounded-full bg-white shadow-md ring-1 ring-black/5 transition-[left] duration-200 ease-out",
          isDark ? "left-[26px]" : "left-[2px]",
        )}
      />

      {/* Sun — pinned to the LIGHT thumb's footprint (left-[2px]
          top-[2px] w-[22px] h-[20px]) so the glyph sits exactly on
          the thumb when light mode is active and exactly mirrored
          on the track when dark mode is active. Identical x/y to the
          thumb means no centering drift. Active glyph uses a fixed
          zinc-700 (oz2-text inverts across themes and would go
          near-white on a white thumb in dark mode); inactive uses
          oz2-text-faint which is theme-adaptive. */}
      <span
        aria-hidden="true"
        className={classNames(
          "pointer-events-none absolute left-[2px] top-[2px] grid h-[20px] w-[22px] place-items-center transition-colors",
          isDark ? "text-oz2-text-faint" : "text-zinc-700",
        )}
      >
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
      </span>

      {/* Moon — pinned to the DARK thumb's footprint (left-[26px]
          top-[2px] w-[22px] h-[20px]). Same alignment story as sun. */}
      <span
        aria-hidden="true"
        className={classNames(
          "pointer-events-none absolute left-[26px] top-[2px] grid h-[20px] w-[22px] place-items-center transition-colors",
          isDark ? "text-zinc-700" : "text-oz2-text-faint",
        )}
      >
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
      </span>
    </button>
  );
};

export default OzThemeToggle;
