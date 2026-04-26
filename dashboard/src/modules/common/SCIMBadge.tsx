"use client";

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@components/Tooltip";
import { CableIcon } from "lucide-react";
import React from "react";

type Props = {
  /** Optional override of the tooltip — context-specific copy. */
  description?: string;
  className?: string;
};

// SCIMBadge marks a row (User or Group) as provisioned by an external
// IdP via SCIM. The hover text warns operators that local edits will
// be overwritten on the next sync — that's the IdP-as-source-of-truth
// contract, intentional, but easy to forget.
export default function SCIMBadge({
  description = "Provisioned by an external IdP via SCIM. Manual edits will be overwritten on the next sync.",
  className = "",
}: Readonly<Props>) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          className={
            "inline-flex items-center gap-1 rounded bg-violet-900/30 border border-violet-700 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-violet-200 " +
            className
          }
        >
          <CableIcon size={10} />
          SCIM
        </span>
      </TooltipTrigger>
      <TooltipContent>
        <span className="text-xs">{description}</span>
      </TooltipContent>
    </Tooltip>
  );
}
