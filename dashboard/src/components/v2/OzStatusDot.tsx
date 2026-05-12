"use client";

import classNames from "classnames";
import React from "react";

// v2 status dot — 8×8 circle. `on` and `warn` get a 3px halo via
// box-shadow + color-mix (matches handoff `.oz-dot.on/warn`).
// Token spec: design_handoff_openzro_dashboard/design/tokens.css.

export interface OzStatusDotProps extends React.HTMLAttributes<HTMLSpanElement> {
  status?: "on" | "off" | "warn";
}

const OzStatusDot = ({
  status = "off",
  className,
  style,
  ...props
}: OzStatusDotProps) => {
  // Inline styles for the colored bg + halo because color-mix() at
  // an arbitrary fraction (22%) of a CSS variable doesn't lend
  // itself to a clean Tailwind utility. The base shape (size, shape,
  // inline-block) stays in Tailwind.
  const colorVar =
    status === "on" ? "var(--ozv2-dot-on)" :
    status === "warn" ? "var(--ozv2-dot-warn)" :
    "var(--ozv2-dot-off)";

  const haloShadow =
    status === "on" || status === "warn"
      ? `0 0 0 3px color-mix(in oklab, ${colorVar} 22%, transparent)`
      : undefined;

  return (
    <span
      className={classNames("inline-block h-2 w-2 rounded-full", className)}
      style={{
        backgroundColor: colorVar,
        boxShadow: haloShadow,
        ...style,
      }}
      {...props}
    />
  );
};

export default OzStatusDot;
