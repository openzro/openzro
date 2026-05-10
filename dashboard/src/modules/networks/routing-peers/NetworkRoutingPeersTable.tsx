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
import * as React from "react";
import { useMemo, useState } from "react";
import PeerIcon from "@/assets/icons/PeerIcon";
import OzCard from "@/components/v2/OzCard";
import {
  OzTable,
  OzTableBody,
  OzTableCell,
  OzTableHead,
  OzTableHeader,
  OzTableRow,
} from "@/components/v2/OzTable";
import { NetworkRouter } from "@/interfaces/Network";
import { NetworkRoutingPeerName } from "@/modules/networks/routing-peers/NetworkRoutingPeerName";
import { RoutingPeersActionCell } from "@/modules/networks/routing-peers/RoutingPeersActionCell";
import { RoutingPeersEnabledCell } from "@/modules/networks/routing-peers/RoutingPeersEnabledCell";
import { RoutingPeersMasqueradeCell } from "@/modules/networks/routing-peers/RoutingPeersMasqueradeCell";
import RouteMetricCell from "@/modules/routes/RouteMetricCell";

type Props = {
  routers?: NetworkRouter[];
  isLoading: boolean;
  headingTarget?: HTMLHeadingElement | null;
};

// NetworkRoutingPeersTable — v2 paint. Drop-in replacement for the
// legacy DataTable + Card. Add CTA stays in the parent
// NetworkRoutingPeersSection header (the routing-peer list is small
// — typically 1-3 entries per network — so the table doesn't need
// its own toolbar). No search either; sort defaults to metric ASC
// so the lowest-metric (preferred) peer leads.

const noopFilter: FilterFn<unknown> = () => true;
const noopSort: SortingFn<unknown> = () => 0;
const NOOP_FILTER_FNS = {
  fuzzy: noopFilter,
  dateRange: noopFilter,
  exactMatch: noopFilter,
  arrIncludesSomeExact: noopFilter,
};
const NOOP_SORTING_FNS = { checkbox: noopSort };

export default function NetworkRoutingPeersTable({
  routers,
  isLoading,
  headingTarget: _headingTarget,
}: Readonly<Props>) {
  const [sorting, setSorting] = useState<SortingState>([
    { id: "metric", desc: false },
  ]);

  const all = useMemo(() => routers ?? [], [routers]);

  const columns = useMemo<ColumnDef<NetworkRouter>[]>(
    () => [
      {
        id: "name",
        accessorFn: (r) => r.id ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Peer" />,
        cell: ({ row }) => <NetworkRoutingPeerName router={row.original} />,
      },
      {
        id: "enabled",
        accessorFn: (r) => (r.enabled ? 1 : 0),
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Active" />,
        cell: ({ row }) => <RoutingPeersEnabledCell router={row.original} />,
      },
      {
        id: "metric",
        accessorFn: (r) => r.metric ?? 0,
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Metric" />,
        cell: ({ row }) => (
          <RouteMetricCell metric={row.original.metric} useHoverStyle={false} />
        ),
      },
      {
        id: "masquerade",
        accessorFn: (r) => (r.masquerade ? 1 : 0),
        sortingFn: "basic",
        header: ({ column }) => (
          <SortHeader column={column} label="Masquerade" />
        ),
        cell: ({ row }) => <RoutingPeersMasqueradeCell router={row.original} />,
      },
      {
        id: "actions",
        size: 60,
        enableSorting: false,
        header: () => null,
        cell: ({ row }) => <RoutingPeersActionCell router={row.original} />,
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

  // Cold-start: no routing peers on this network. Renders an
  // OzCard-on-dashed hero. Add CTA lives in the section header.
  if (!isLoading && all.length === 0) {
    return (
      <OzCard className="border-dashed">
        <div className="flex flex-col items-center gap-3 px-6 py-10 text-center">
          <div
            aria-hidden
            className="grid h-11 w-11 place-items-center rounded-full border border-oz2-border-soft bg-oz2-bg-sunken text-oz2-text-2"
          >
            <PeerIcon size={20} />
          </div>
          <div>
            <p className="text-[14px] font-medium text-oz2-text">
              This network has no routing peers
            </p>
            <p className="mx-auto mt-1 max-w-md text-[12.5px] text-oz2-text-muted">
              Add a routing peer to bridge mesh traffic into this network. Run
              2+ in different fault domains to enable high availability.
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
                  {isLoading ? "Loading routing peers…" : "—"}
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
  column: Column<NetworkRouter, unknown>;
  label: string;
}) {
  if (!column.getCanSort()) {
    return <span>{label}</span>;
  }
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
