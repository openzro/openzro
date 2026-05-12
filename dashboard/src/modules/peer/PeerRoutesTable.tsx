"use client";

import { TooltipProvider } from "@components/Tooltip";
import {
  Column,
  ColumnDef,
  FilterFn,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  SortingFn,
  SortingState,
  useReactTable,
} from "@tanstack/react-table";
import React, { useMemo, useState } from "react";
import NetworkRoutesIcon from "@/assets/icons/NetworkRoutesIcon";
import OzCard from "@/components/v2/OzCard";
import {
  OzTable,
  OzTableBody,
  OzTableCell,
  OzTableHead,
  OzTableHeader,
  OzTableRow,
} from "@/components/v2/OzTable";
import { Peer } from "@/interfaces/Peer";
import { Route } from "@/interfaces/Route";
import PeerRouteActionCell from "@/modules/peer/PeerRouteActionCell";
import PeerRouteActiveCell from "@/modules/peer/PeerRouteActiveCell";
import PeerRouteNameCell from "@/modules/peer/PeerRouteNameCell";
import GroupedRouteNetworkRangeCell from "@/modules/route-group/GroupedRouteNetworkRangeCell";
import RouteDistributionGroupsCell from "@/modules/routes/RouteDistributionGroupsCell";

type Props = {
  peerRoutes?: Route[];
  isLoading: boolean;
  headingTarget?: HTMLHeadingElement | null;
  peer: Peer;
};

// PeerRoutesTable — v2 paint. Drop-in replacement for the legacy
// DataTable + Card chrome. Cells reuse the existing per-row modules
// (PeerRouteNameCell, GroupedRouteNetworkRangeCell, RouteDistributionGroupsCell,
// PeerRouteActiveCell, PeerRouteActionCell) so cell-level behavior
// is verbatim. No search/pager — the per-peer route list is small
// enough that a toolbar would just add noise.

const noopFilter: FilterFn<unknown> = () => true;
const noopSort: SortingFn<unknown> = () => 0;
const NOOP_FILTER_FNS = {
  fuzzy: noopFilter,
  dateRange: noopFilter,
  exactMatch: noopFilter,
  arrIncludesSomeExact: noopFilter,
};
const NOOP_SORTING_FNS = { checkbox: noopSort };

export default function PeerRoutesTable({
  peerRoutes,
  isLoading,
  headingTarget: _headingTarget,
}: Props) {
  const [sorting, setSorting] = useState<SortingState>([
    { id: "network_id", desc: true },
  ]);

  const all = useMemo(() => peerRoutes ?? [], [peerRoutes]);

  const columns = useMemo<ColumnDef<Route>[]>(
    () => [
      {
        id: "network_id",
        accessorFn: (r) => r.network_id ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Name" />,
        cell: ({ row }) => <PeerRouteNameCell route={row.original} />,
      },
      {
        id: "network",
        accessorFn: (r) => r.network ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Network" />,
        cell: ({ row }) => (
          <GroupedRouteNetworkRangeCell
            domains={row.original?.domains}
            network={row.original?.network}
          />
        ),
      },
      {
        id: "groups",
        accessorFn: (r) => r.groups?.length ?? 0,
        sortingFn: "basic",
        header: ({ column }) => (
          <SortHeader column={column} label="Distribution Groups" />
        ),
        cell: ({ row }) => <RouteDistributionGroupsCell route={row.original} />,
      },
      {
        id: "enabled",
        accessorFn: (r) => (r.enabled ? 1 : 0),
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Active" />,
        cell: ({ row }) => <PeerRouteActiveCell route={row.original} />,
      },
      {
        id: "actions",
        size: 60,
        enableSorting: false,
        header: () => null,
        cell: ({ row }) => <PeerRouteActionCell route={row.original} />,
      },
    ],
    [],
  );

  const table = useReactTable({
    data: all,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getRowId: (r) => r.id ?? "",
    filterFns: NOOP_FILTER_FNS,
    sortingFns: NOOP_SORTING_FNS,
  });

  if (!isLoading && all.length === 0) {
    return (
      <OzCard className="border-dashed">
        <div className="flex flex-col items-center gap-3 px-6 py-10 text-center">
          <div
            aria-hidden
            className="grid h-11 w-11 place-items-center rounded-full border border-oz2-border-soft bg-oz2-bg-sunken text-oz2-text-2"
          >
            <NetworkRoutesIcon size={20} />
          </div>
          <div>
            <p className="text-[14px] font-medium text-oz2-text">
              This peer has no network routes
            </p>
            <p className="mx-auto mt-1 max-w-md text-[12.5px] text-oz2-text-muted">
              Assign this peer to an existing route or create a new network
              route from the buttons above to expose LANs / VPCs through it.
            </p>
          </div>
        </div>
      </OzCard>
    );
  }

  return (
    <TooltipProvider delayDuration={250} skipDelayDuration={100}>
      <OzCard flush>
        <OzTable>
          <OzTableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <OzTableRow
                key={headerGroup.id}
                className="hover:bg-transparent"
              >
                {headerGroup.headers.map((header) => (
                  <OzTableHead
                    key={header.id}
                    style={
                      header.column.columnDef.size
                        ? { width: header.column.columnDef.size }
                        : undefined
                    }
                  >
                    {header.isPlaceholder
                      ? null
                      : flexRender(
                          header.column.columnDef.header,
                          header.getContext(),
                        )}
                  </OzTableHead>
                ))}
              </OzTableRow>
            ))}
          </OzTableHeader>
          <OzTableBody>
            {table.getRowModel().rows.map((row) => (
              <OzTableRow key={row.id}>
                {row.getVisibleCells().map((cell) => (
                  <OzTableCell key={cell.id}>
                    {flexRender(
                      cell.column.columnDef.cell,
                      cell.getContext(),
                    )}
                  </OzTableCell>
                ))}
              </OzTableRow>
            ))}
            {table.getRowModel().rows.length === 0 && (
              <OzTableRow className="hover:bg-transparent">
                <OzTableCell
                  colSpan={columns.length}
                  className="px-[18px] py-12 text-center text-oz2-text-muted"
                >
                  {isLoading ? "Loading network routes…" : "—"}
                </OzTableCell>
              </OzTableRow>
            )}
          </OzTableBody>
        </OzTable>
      </OzCard>
    </TooltipProvider>
  );
}

function SortHeader({
  column,
  label,
}: {
  column: Column<Route, unknown>;
  label: string;
}) {
  if (!column.getCanSort()) return <span>{label}</span>;
  const sorted = column.getIsSorted();
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation();
        column.toggleSorting();
      }}
      className="-mx-1 inline-flex h-5 items-center gap-1.5 rounded px-1 text-left font-mono text-[11.5px] font-semibold uppercase tracking-widest text-oz2-text-muted transition-colors hover:text-oz2-text"
    >
      {label}
      <span
        className={
          "text-oz2-text-faint transition-opacity " +
          (sorted ? "text-oz2-text opacity-100" : "opacity-50")
        }
      >
        {sorted === "asc" ? "↑" : sorted === "desc" ? "↓" : "↕"}
      </span>
    </button>
  );
}
