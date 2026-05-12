"use client";

import FullTooltip from "@components/FullTooltip";
import { ArrowUpDown, InfoIcon } from "lucide-react";
import * as React from "react";

// V2 paint of RouteMetricCell — swaps the legacy nb-gray palette for
// oz2 tokens. Tooltip behavior preserved (lower metrics = higher
// priority hint).

type Props = {
  metric?: number;
  useHoverStyle?: boolean;
};

export default function RouteMetricCellV2({
  metric,
  useHoverStyle = true,
}: Readonly<Props>) {
  return (
    <FullTooltip
      hoverButton={useHoverStyle}
      isAction={true}
      content={
        <div className="flex max-w-xs items-center gap-2 text-xs">
          <div>Lower metrics have higher priority.</div>
        </div>
      }
    >
      <div className="flex items-center gap-2 text-oz2-text-2">
        <ArrowUpDown size={14} />
        <span className="font-mono text-[12.5px]">{metric}</span>
        <InfoIcon size={14} className="text-oz2-text-faint" />
      </div>
    </FullTooltip>
  );
}
