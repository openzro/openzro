"use client";

import classNames from "classnames";
import React, { forwardRef } from "react";

// v2 card primitive — surface + border + 14px radius + shadow-sm.
// Padding is left to the consumer (handoff suggests 18-20px common).
// Token spec: design_handoff_openzro_dashboard/design/tokens.css `.oz-card`.

export interface OzCardProps extends React.HTMLAttributes<HTMLDivElement> {
  /**
   * `flush` drops the default padding so consumers can place a header
   * row + a divider + a content area inside without fighting the
   * outer padding. Default `false` keeps the comfortable 18px inset.
   */
  flush?: boolean;
}

const OzCard = forwardRef<HTMLDivElement, OzCardProps>(
  ({ className, flush, ...props }, ref) => {
    return (
      <div
        ref={ref}
        className={classNames(
          "rounded-oz2-card border border-oz2-border bg-oz2-surface shadow-oz2-sm",
          // `flush` consumers stack edge-painted children (table headers,
          // bulk-action bands, footers) that bleed past rounded corners
          // unless we clip. Padded variants don't have this problem
          // because the inset keeps content off the corners.
          flush ? "overflow-hidden" : "p-[18px]",
          className,
        )}
        {...props}
      />
    );
  },
);

OzCard.displayName = "OzCard";

export default OzCard;
