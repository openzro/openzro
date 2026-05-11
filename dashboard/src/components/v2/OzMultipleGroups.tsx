"use client";

import { ScrollArea } from "@components/ScrollArea";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@components/Tooltip";
import classNames from "classnames";
import { ArrowRightIcon } from "lucide-react";
import * as React from "react";
import OzGroupBadge from "@/components/v2/OzGroupBadge";
import OzPeerBadge from "@/components/v2/OzPeerBadge";
import { Group } from "@/interfaces/Group";
import EmptyRow from "@/modules/common-table-rows/EmptyRow";

// v2 paint of MultipleGroups — renders the first group as an
// OzGroupBadge plus a "+N" overflow pill, with a hover tooltip
// listing every group → peer count. Mirrors the legacy ordering
// rules (All last, then by peer count desc, then alpha).

type Props = {
  groups: Group[];
  label?: string;
  onClick?: () => void;
  className?: string;
};

export default function OzMultipleGroups({
  groups,
  label = "Assigned Groups",
  onClick,
  className,
}: Readonly<Props>) {
  if (!groups || groups.length === 0) return <EmptyRow />;

  const orderedGroups = [...groups].sort((a, b) => {
    if (a.name === "All") return 1;
    if (b.name === "All") return -1;
    const aPeerCount = a.peers_count ?? 0;
    const bPeerCount = b.peers_count ?? 0;
    if (aPeerCount !== bPeerCount) return bPeerCount - aPeerCount;
    return a.name.localeCompare(b.name);
  });

  const firstGroup = orderedGroups[0];
  const otherGroups = orderedGroups.slice(1);

  return (
    <TooltipProvider
      disableHoverableContent={false}
      delayDuration={200}
      skipDelayDuration={200}
    >
      <Tooltip>
        <TooltipTrigger asChild>
          <div
            data-cy="multiple-groups"
            onClick={onClick}
            className={classNames("inline-flex items-center gap-2", className)}
          >
            {firstGroup && <OzGroupBadge group={firstGroup} />}
            {otherGroups.length > 0 && (
              <span className="inline-flex items-center whitespace-nowrap rounded-full border border-oz2-border bg-oz2-surface px-2.5 py-[3px] text-[12px] font-medium text-oz2-text-2">
                + {otherGroups.length}
              </span>
            )}
          </div>
        </TooltipTrigger>
        <TooltipContent
          className="p-0"
          onClick={(e) => e.stopPropagation()}
        >
          <div className="px-5 pt-3 text-left text-sm font-medium">{label}</div>
          <ScrollArea className="flex max-h-[285px] flex-col overflow-y-auto px-5 pt-3">
            <div className="mb-2 flex flex-col items-start gap-2 last:pb-2">
              {orderedGroups.map(
                (group) =>
                  group && (
                    <div
                      key={group.id}
                      className="flex w-full items-center justify-between gap-2"
                    >
                      <OzGroupBadge group={group} />
                      <ArrowRightIcon size={14} />
                      <OzPeerBadge>{group.peers_count} Peer(s)</OzPeerBadge>
                    </div>
                  ),
              )}
            </div>
          </ScrollArea>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
