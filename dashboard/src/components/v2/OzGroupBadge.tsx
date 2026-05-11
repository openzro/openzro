"use client";

import { GroupBadgeIcon } from "@components/ui/GroupBadgeIcon";
import TruncatedText from "@components/ui/TruncatedText";
import classNames from "classnames";
import { XIcon } from "lucide-react";
import * as React from "react";
import { Group } from "@/interfaces/Group";

// v2 paint of GroupBadge — uses oz2 tokens for surface/border/text
// instead of the legacy `Badge variant="gray-ghost"`. The
// GroupBadgeIcon (folder / IdP glyph) is reused as-is (already
// neutral). Optional `showX` retains the legacy chip-remove
// affordance.

type Props = {
  group: Group;
  onClick?: (e: React.MouseEvent<HTMLDivElement>) => void;
  showX?: boolean;
  children?: React.ReactNode;
  className?: string;
  maxChars?: number;
  maxWidth?: string;
  hideTooltip?: boolean;
};

export default function OzGroupBadge({
  onClick,
  group,
  showX = false,
  children,
  className,
  maxChars = 20,
  maxWidth,
  hideTooltip = false,
}: Readonly<Props>) {
  return (
    <div
      key={group.id ?? group.name}
      data-cy="group-badge"
      onClick={(e) => {
        e.preventDefault();
        onClick?.(e);
      }}
      className={classNames(
        "group inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border px-2.5 py-[3px]",
        "border-oz2-border bg-oz2-surface text-oz2-text-2 text-[12px] font-medium",
        "transition-colors",
        onClick ? "cursor-pointer hover:bg-oz2-hover hover:border-oz2-border-strong" : "",
        className,
      )}
    >
      <GroupBadgeIcon id={group?.id} issued={group?.issued} />
      <TruncatedText
        text={group?.name || ""}
        maxChars={maxChars}
        maxWidth={maxWidth}
        hideTooltip={hideTooltip}
      />
      {children}
      {showX && (
        <XIcon
          size={12}
          className="shrink-0 cursor-pointer text-oz2-text-faint transition-colors group-hover:text-oz2-text"
        />
      )}
    </div>
  );
}
