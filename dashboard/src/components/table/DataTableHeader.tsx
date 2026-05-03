"use client";

import FullTooltip from "@components/FullTooltip";
import { IconSortAscending, IconSortDescending } from "@tabler/icons-react";
import type { Column } from "@tanstack/table-core";
import { cn } from "@utils/helpers";
import React from "react";

type Props = {
  column: Column<any>;
  children: React.ReactNode;
  tooltip?: string | React.ReactNode;
  center?: boolean;
  className?: string;
};
export default function DataTableHeader({
  children,
  column,
  tooltip,
  center,
  className,
}: Props) {
  return (
    <FullTooltip content={tooltip} disabled={!tooltip}>
      <div
        onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
        className={cn(
          "flex items-center whitespace-nowrap cursor-pointer gap-2 transition-all select-none text-xs tracking-wide",
          // Light: neutral chip text, deeper on hover. Dark: keep
          // the original gray-400 / gray-300 pair. The previous
          // `hover:text-nb-gray` resolved to the light-mode page bg
          // token (near-white) on white surfaces — hovering made
          // the header label vanish.
          "text-neutral-500 hover:text-neutral-900",
          "dark:text-gray-400 dark:hover:text-gray-300",
          center && "justify-center w-full",
          className,
        )}
      >
        {children}
        {column.getIsSorted() === "desc" ? (
          <IconSortAscending size={16} />
        ) : (
          <IconSortDescending size={16} />
        )}
      </div>
    </FullTooltip>
  );
}
