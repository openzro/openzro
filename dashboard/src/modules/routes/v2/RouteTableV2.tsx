"use client";

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
import {
  OzTable,
  OzTableBody,
  OzTableCell,
  OzTableHead,
  OzTableHeader,
  OzTableRow,
} from "@/components/v2/OzTable";
import { useGroups } from "@/contexts/GroupsProvider";
import { GroupedRoute, Route } from "@/interfaces/Route";
import RouteAccessControlGroupsV2 from "@/modules/routes/v2/RouteAccessControlGroupsV2";
import RouteActionCellV2 from "@/modules/routes/v2/RouteActionCellV2";
import RouteActiveCellV2 from "@/modules/routes/v2/RouteActiveCellV2";
import RouteDistributionGroupsCellV2 from "@/modules/routes/v2/RouteDistributionGroupsCellV2";
import RouteMetricCellV2 from "@/modules/routes/v2/RouteMetricCellV2";
import RoutePeerCellV2 from "@/modules/routes/v2/RoutePeerCellV2";

// V2 paint of RouteTable — replaces the legacy DataTable shell with
// OzTable. Reuses the existing data cells (RoutePeerCell,
// RouteMetricCell, RouteActiveCell, distribution/ACL group cells)
// and the v2-painted RouteActionCellV2. Same TanStack sort behavior,
// no pagination / toolbar (matches the legacy `minimal` + manualPagination
// behavior that the parent accordion expects).

type Props = {
  row: GroupedRoute;
};

const noopFilter: FilterFn<unknown> = () => true;
const noopSort: SortingFn<unknown> = () => 0;
const NOOP_FILTER_FNS = {
  fuzzy: noopFilter,
  dateRange: noopFilter,
  exactMatch: noopFilter,
  arrIncludesSomeExact: noopFilter,
};
const NOOP_SORTING_FNS = { checkbox: noopSort };

const columns: ColumnDef<Route>[] = [
  {
    id: "network_id",
    accessorKey: "network_id",
    sortingFn: "text",
    header: ({ column }) => <SortHeader column={column} label="Name" />,
    cell: ({ row }) => <RoutePeerCellV2 route={row.original} />,
  },
  {
    id: "metric",
    accessorKey: "metric",
    sortingFn: "alphanumeric",
    header: ({ column }) => <SortHeader column={column} label="Metric" />,
    cell: ({ row }) => <RouteMetricCellV2 metric={row.original.metric} />,
  },
  {
    id: "enabled",
    accessorKey: "enabled",
    sortingFn: "basic",
    header: ({ column }) => <SortHeader column={column} label="Active" />,
    cell: ({ row }) => <RouteActiveCellV2 route={row.original} />,
  },
  {
    id: "groups",
    accessorFn: (r) => r.groups?.length ?? 0,
    sortingFn: "basic",
    header: ({ column }) => (
      <SortHeader column={column} label="Distribution Groups" />
    ),
    cell: ({ row }) => <RouteDistributionGroupsCellV2 route={row.original} />,
  },
  {
    id: "access_control_groups",
    accessorFn: (r) => r?.access_control_groups?.length ?? 0,
    sortingFn: "basic",
    header: ({ column }) => (
      <SortHeader column={column} label="Access Control Groups" />
    ),
    cell: ({ row }) => <RouteAccessControlGroupsV2 route={row.original} />,
  },
  {
    id: "actions",
    size: 56,
    enableSorting: false,
    header: () => null,
    cell: ({ row }) => <RouteActionCellV2 route={row.original} />,
  },
];

export default function RouteTableV2({ row }: Props) {
  const { groups } = useGroups();
  const [sorting, setSorting] = useState<SortingState>([
    { id: "network_id", desc: true },
    { id: "metric", desc: true },
  ]);

  const data = useMemo(() => {
    if (!row.routes) return [];
    return row.routes.map((route) => {
      const distributionGroupNames =
        route.groups?.map((id) => {
          return groups?.find((g) => g.id === id)?.name || "";
        }) || [];
      const peerGroupNames =
        route.peer_groups?.map((id) => {
          return groups?.find((g) => g.id === id)?.name || "";
        }) || [];
      const allGroupNames = [...distributionGroupNames, ...peerGroupNames];
      const domainString = route?.domains?.join(", ") || "";
      return {
        ...route,
        group_names: allGroupNames,
        domain_search: domainString,
      } as Route;
    });
  }, [row.routes, groups]);

  const table = useReactTable({
    data,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getRowId: (r) => r.id ?? r.network_id,
    filterFns: NOOP_FILTER_FNS,
    sortingFns: NOOP_SORTING_FNS,
  });

  return (
    <div>
      <OzTable className="text-[13px]">
        <OzTableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <OzTableRow key={headerGroup.id} className="hover:bg-transparent">
              {headerGroup.headers.map((header) => (
                <OzTableHead
                  key={header.id}
                  style={
                    header.column.columnDef.size
                      ? { width: header.column.columnDef.size }
                      : undefined
                  }
                  className="bg-oz2-bg-sunken"
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
                  {flexRender(cell.column.columnDef.cell, cell.getContext())}
                </OzTableCell>
              ))}
            </OzTableRow>
          ))}
          {table.getRowModel().rows.length === 0 && (
            <OzTableRow className="hover:bg-transparent">
              <OzTableCell
                colSpan={columns.length}
                className="px-[18px] py-6 text-center text-oz2-text-muted"
              >
                No routes in this network.
              </OzTableCell>
            </OzTableRow>
          )}
        </OzTableBody>
      </OzTable>
    </div>
  );
}

// ─── Sortable header (clone of NetworkRoutesTableV2/NetworksTableV2) ──

function SortHeader({
  column,
  label,
}: {
  column: Column<Route, unknown>;
  label: string;
}) {
  if (!column.getCanSort()) {
    return <span>{label}</span>;
  }
  const sorted = column.getIsSorted();
  return (
    <button
      type="button"
      onClick={() => column.toggleSorting()}
      className="-mx-1 inline-flex h-5 items-center gap-1.5 rounded px-1 text-left font-mono text-[11px] font-semibold uppercase tracking-widest text-oz2-text-muted transition-colors hover:text-oz2-text"
    >
      {label}
      <span
        className={
          "text-oz2-text-faint transition-opacity " +
          (sorted ? "text-oz2-text opacity-100" : "opacity-50")
        }
      >
        {sorted === "asc" ? SORT_ASC : sorted === "desc" ? SORT_DESC : SORT_IDLE}
      </span>
    </button>
  );
}

const SORT_ASC = (
  <svg
    viewBox="0 0 24 24"
    width={10}
    height={10}
    fill="none"
    stroke="currentColor"
    strokeWidth={2}
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <path d="m6 15 6-6 6 6" />
  </svg>
);
const SORT_DESC = (
  <svg
    viewBox="0 0 24 24"
    width={10}
    height={10}
    fill="none"
    stroke="currentColor"
    strokeWidth={2}
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <path d="m6 9 6 6 6-6" />
  </svg>
);
const SORT_IDLE = (
  <svg
    viewBox="0 0 24 24"
    width={10}
    height={10}
    fill="none"
    stroke="currentColor"
    strokeWidth={2}
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <path d="m6 9 6-6 6 6" />
    <path d="m6 15 6 6 6-6" />
  </svg>
);
