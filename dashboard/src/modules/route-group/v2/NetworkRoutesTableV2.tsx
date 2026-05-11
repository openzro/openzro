"use client";

import {
  Column,
  ColumnDef,
  ExpandedState,
  FilterFn,
  flexRender,
  getCoreRowModel,
  getExpandedRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  PaginationState,
  SortingFn,
  SortingState,
  useReactTable,
} from "@tanstack/react-table";
import { cloneDeep } from "lodash";
import { BookOpen, Network, PlusCircle, Route as RouteIcon, ShieldCheck } from "lucide-react";
import { usePathname } from "next/navigation";
import React, { useEffect, useMemo, useRef, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";
import OzEmptyState from "@/components/v2/OzEmptyState";
import {
  OzTable,
  OzTableBody,
  OzTableCell,
  OzTableHead,
  OzTableHeader,
  OzTableRow,
} from "@/components/v2/OzTable";
import GroupRouteProvider from "@/contexts/GroupRouteProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { useLocalStorage } from "@/hooks/useLocalStorage";
import { GroupedRoute, Route } from "@/interfaces/Route";
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import { AddExitNodeButton } from "@/modules/exit-node/AddExitNodeButton";
import GroupedRouteNameCell from "@/modules/route-group/GroupedRouteNameCell";
import GroupedRouteNetworkRangeCell from "@/modules/route-group/GroupedRouteNetworkRangeCell";
import GroupedRouteActionCellV2 from "@/modules/route-group/v2/GroupedRouteActionCellV2";
import GroupedRouteHighAvailabilityCellV2 from "@/modules/route-group/v2/GroupedRouteHighAvailabilityCellV2";
import GroupedRouteTypeCellV2 from "@/modules/route-group/v2/GroupedRouteTypeCellV2";
import { RouteAddRoutingPeerProvider } from "@/modules/routes/RouteAddRoutingPeerProvider";
import RouteModal from "@/modules/routes/RouteModal";
import RouteTableV2 from "@/modules/routes/v2/RouteTableV2";

// NetworkRoutesTableV2 — v2 paint over /api/routes data. Mirrors
// NetworksTableV2's chrome (header + stat row + toolbar + TanStack
// table + footer pager) but with grouped-route columns and an
// expand-row that renders the legacy nested RouteTable inside a
// GroupRouteProvider, matching the legacy NetworkRoutesTable
// behavior.

interface Props {
  isLoading: boolean;
  groupedRoutes?: GroupedRoute[];
  routes?: Route[];
}

type EnabledFilter = "all" | "enabled";

const noopFilter: FilterFn<unknown> = () => true;
const noopSort: SortingFn<unknown> = () => 0;
const NOOP_FILTER_FNS = {
  fuzzy: noopFilter,
  dateRange: noopFilter,
  exactMatch: noopFilter,
  arrIncludesSomeExact: noopFilter,
};
const NOOP_SORTING_FNS = { checkbox: noopSort };

export default function NetworkRoutesTableV2(props: Props) {
  return (
    <RouteAddRoutingPeerProvider>
      <NetworkRoutesView {...props} />
    </RouteAddRoutingPeerProvider>
  );
}

function NetworkRoutesView({ isLoading, groupedRoutes, routes }: Props) {
  const { mutate } = useSWRConfig();
  const path = usePathname();

  const [routeModal, setRouteModal] = useState(false);
  const openCreateRouteModal = () => setRouteModal(true);

  // Mount Add Exit Node + Add Route triggers into the V2 topbar's
  // right slot so the actions live next to the theme toggle. Matches
  // the placement other v2 list screens use.
  useV2TopbarRight(
    <>
      <AddExitNodeButton />
      <AddRouteButtonV2 onClick={openCreateRouteModal} />
    </>,
  );

  const [search, setSearch] = useState("");
  const [enabledFilter, setEnabledFilter] = useState<EnabledFilter>("all");
  const [refreshing, setRefreshing] = useState(false);

  const [sorting, setSorting] = useLocalStorage<SortingState>(
    "openzro-table-sort-v2" + path,
    [{ id: "network_id", desc: false }],
  );
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  });
  const [expanded, setExpanded] = useState<ExpandedState>({});

  const all = useMemo(() => groupedRoutes ?? [], [groupedRoutes]);

  const counts = useMemo(() => {
    let enabled = 0;
    for (const r of all) if (r.enabled) enabled += 1;
    return {
      total: all.length,
      enabled,
      disabled: all.length - enabled,
    };
  }, [all]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return all.filter((r) => {
      const enabledOk = enabledFilter === "all" || !!r.enabled;
      if (!enabledOk) return false;
      if (!q) return true;
      const haystack = [
        r.network_id,
        r.description,
        r.description_search,
        r.network,
        r.routes_search,
        r.domain_search,
        r.domains?.join(" "),
        r.group_names?.join(" "),
      ]
        .filter(Boolean)
        .join(" ")
        .toLowerCase();
      return haystack.includes(q);
    });
  }, [all, search, enabledFilter]);

  useEffect(() => {
    setPagination((prev) => ({ ...prev, pageIndex: 0 }));
  }, [search, enabledFilter]);

  const columns = useMemo<ColumnDef<GroupedRoute>[]>(
    () => [
      {
        id: "network_id",
        accessorFn: (r) => r.network_id ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Name" />,
        cell: ({ row }) => (
          <GroupedRouteNameCell groupedRoute={row.original} />
        ),
      },
      {
        id: "network",
        accessorFn: (r) => r.network ?? r.domains?.join(", ") ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Network" />,
        cell: ({ row }) => (
          <GroupedRouteNetworkRangeCell
            network={row.original.network}
            domains={row.original.domains}
          />
        ),
      },
      {
        id: "type",
        accessorFn: (r) => (r.is_using_route_groups ? 1 : 0),
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Type" />,
        cell: ({ row }) => <GroupedRouteTypeCellV2 groupedRoute={row.original} />,
      },
      {
        id: "high_availability",
        accessorFn: (r) => r.high_availability_count ?? 0,
        sortingFn: "basic",
        header: ({ column }) => (
          <SortHeader column={column} label="High Availability" />
        ),
        cell: ({ row }) => (
          <GroupedRouteHighAvailabilityCellV2 groupedRoute={row.original} />
        ),
      },
      {
        id: "actions",
        size: 40,
        enableSorting: false,
        header: () => null,
        cell: ({ row }) => (
          <GroupedRouteActionCellV2 groupedRoute={row.original} />
        ),
      },
    ],
    [],
  );

  const table = useReactTable({
    data: filtered,
    columns,
    state: { sorting, pagination, expanded },
    onSortingChange: setSorting,
    onPaginationChange: setPagination,
    onExpandedChange: setExpanded,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getExpandedRowModel: getExpandedRowModel(),
    getRowId: (r) => r.id,
    filterFns: NOOP_FILTER_FNS,
    sortingFns: NOOP_SORTING_FNS,
  });

  const refreshClick = () => {
    setRefreshing(true);
    mutate("/routes").finally(() => setRefreshing(false));
  };

  const pageInfo = table.getState().pagination;
  const total = filtered.length;
  const pageStart =
    total === 0 ? 0 : pageInfo.pageIndex * pageInfo.pageSize + 1;
  const pageEnd = Math.min(total, (pageInfo.pageIndex + 1) * pageInfo.pageSize);

  // Cold-start: no routes exist yet. Mirrors NetworksTableV2's
  // isColdStart branch — replaces stats + toolbar + table with the
  // OzEmptyState hero. The toolbar topbar Add buttons stay mounted.
  const isColdStart = !isLoading && (routes?.length ?? 0) === 0;

  return (
    <>
      <RouteModal open={routeModal} setOpen={setRouteModal} />

      {isColdStart ? (
        <NetworkRoutesEmptyState onAddRoute={openCreateRouteModal} />
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-[13.5px] text-oz2-text-muted">
            <span className="inline-flex items-center gap-2">
              <span className="font-medium text-oz2-text">{counts.total}</span>
              Routes
            </span>
            <span className="inline-flex items-center gap-2 border-l border-oz2-border-soft pl-5">
              <span className="font-medium text-oz2-text">{counts.enabled}</span>
              Enabled
            </span>
            <span className="inline-flex items-center gap-2 border-l border-oz2-border-soft pl-5">
              <span className="font-medium text-oz2-text">
                {counts.disabled}
              </span>
              Disabled
            </span>
          </div>

          <div className="flex flex-wrap items-center gap-3">
            <div className="inline-flex h-[34px] w-[280px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3">
              <span className="text-oz2-text-faint">{ICONS.search}</span>
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search by network, range, name or groups…"
                className="h-full flex-1 border-0 bg-transparent text-[14px] outline-none placeholder:text-oz2-text-faint"
              />
            </div>

            <SegmentedTabs
              value={enabledFilter}
              onChange={setEnabledFilter}
              options={[
                { id: "all", label: "All", count: counts.total },
                { id: "enabled", label: "Enabled", count: counts.enabled },
              ]}
            />

            <PageSizeCombobox
              value={pageInfo.pageSize}
              onChange={(n) => table.setPageSize(n)}
            />

            <button
              type="button"
              onClick={refreshClick}
              aria-label="Refresh routes"
              className="grid h-[34px] w-[34px] place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 hover:border-oz2-border-strong hover:bg-oz2-hover"
            >
              <span className={refreshing ? "animate-spin text-oz2-acc" : ""}>
                {ICONS.refresh}
              </span>
            </button>
          </div>

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
                {table.getRowModel().rows.map((row) => {
                  const isOpen = row.getIsExpanded();
                  return (
                    <React.Fragment key={row.id}>
                      <OzTableRow
                        className="group/accordion cursor-pointer"
                        data-accordion={isOpen ? "opened" : "closed"}
                        onClick={(e) => {
                          // Don't toggle when clicks come from the
                          // action cell's buttons; the cell uses
                          // stopPropagation but defensive guard here
                          // keeps the row inert for any nested form.
                          const target = e.target as HTMLElement;
                          if (target.closest("button, a, input, [role=dialog]"))
                            return;
                          row.toggleExpanded();
                        }}
                      >
                        {row.getVisibleCells().map((cell) => (
                          <OzTableCell key={cell.id}>
                            {flexRender(
                              cell.column.columnDef.cell,
                              cell.getContext(),
                            )}
                          </OzTableCell>
                        ))}
                      </OzTableRow>
                      {isOpen && (
                        <OzTableRow className="hover:bg-transparent">
                          <OzTableCell
                            colSpan={columns.length}
                            className="bg-oz2-bg-sunken p-0"
                          >
                            <GroupRouteProvider
                              groupedRoute={cloneDeep(row.original)}
                            >
                              <RouteTableV2 row={cloneDeep(row.original)} />
                            </GroupRouteProvider>
                          </OzTableCell>
                        </OzTableRow>
                      )}
                    </React.Fragment>
                  );
                })}
                {table.getRowModel().rows.length === 0 && (
                  <OzTableRow className="hover:bg-transparent">
                    <OzTableCell
                      colSpan={columns.length}
                      className="px-[18px] py-12 text-center text-oz2-text-muted"
                    >
                      {isLoading
                        ? "Loading network routes…"
                        : search || enabledFilter !== "all"
                          ? "No routes match your filters."
                          : "No network routes yet."}
                    </OzTableCell>
                  </OzTableRow>
                )}
              </OzTableBody>
            </OzTable>

            <div className="flex flex-wrap items-center justify-between gap-3 border-t border-oz2-border-soft bg-oz2-bg-sunken px-[18px] py-3 text-[13.5px]">
              <span className="text-oz2-text-muted">
                {total === 0
                  ? "0 routes"
                  : `Showing ${pageStart}–${pageEnd} of ${total}`}
              </span>
              <Pager
                page={pageInfo.pageIndex + 1}
                totalPages={Math.max(1, table.getPageCount())}
                onChange={(p) => table.setPageIndex(p - 1)}
              />
            </div>
          </OzCard>
        </>
      )}
    </>
  );
}

// ─── Empty state ───────────────────────────────────────────────────────────

function NetworkRoutesEmptyState({ onAddRoute }: { onAddRoute: () => void }) {
  return (
    <OzEmptyState
      title="Create New Route"
      description="It looks like you don't have any routes. Access LANs and VPC by adding a network route."
      primaryAction={<AddRouteButtonV2 onClick={onAddRoute} />}
      secondaryAction={<AddExitNodeButton firstTime />}
      learnMore={
        <>
          Learn more about{" "}
          <a
            href="https://docs.openzro.io/how-to/routing-traffic-to-private-networks"
            target="_blank"
            rel="noopener noreferrer"
            className="font-medium text-oz2-acc-text underline-offset-2 hover:underline"
          >
            Network Routes
          </a>
          .
        </>
      }
      helperCards={[
        {
          icon: <Network size={16} />,
          title: "What are routes?",
          description:
            "Reach LANs and VPCs through a routing peer — no openZro client on every resource.",
          href: "https://docs.openzro.io/how-to/routing-traffic-to-private-networks",
        },
        {
          icon: <RouteIcon size={16} />,
          title: "High availability",
          description:
            "Designate multiple peers per network so traffic survives a single peer going down.",
          href: "https://docs.openzro.io/how-to/routing-traffic-to-private-networks#high-availability",
        },
        {
          icon: <ShieldCheck size={16} />,
          title: "Exit nodes",
          description:
            "Route a peer's traffic through another peer to reach the public internet.",
          href: "https://docs.openzro.io/how-to/using-an-exit-node",
        },
      ]}
    />
  );
}

// ─── Add Route button ──────────────────────────────────────────────────────

function AddRouteButtonV2({ onClick }: { onClick: () => void }) {
  const { permission } = usePermissions();
  return (
    <OzButton
      variant="primary"
      onClick={onClick}
      disabled={!permission.routes.create}
      type="button"
    >
      <PlusCircle size={14} />
      Add Route
    </OzButton>
  );
}

// ─── Sortable header (clone of NetworksTableV2 SortHeader) ────────────────

function SortHeader({
  column,
  label,
}: {
  column: Column<GroupedRoute, unknown>;
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
      className="-mx-1 inline-flex h-5 items-center gap-1.5 rounded px-1 text-left font-mono text-[11.5px] font-semibold uppercase tracking-widest text-oz2-text-muted transition-colors hover:text-oz2-text"
    >
      {label}
      <span
        className={
          "text-oz2-text-faint transition-opacity " +
          (sorted ? "text-oz2-text opacity-100" : "opacity-50")
        }
      >
        {sorted === "asc"
          ? ICONS.sortAsc
          : sorted === "desc"
            ? ICONS.sortDesc
            : ICONS.sortIdle}
      </span>
    </button>
  );
}

// ─── Segmented filter (clone of PeersTableV2) ─────────────────────────────

function SegmentedTabs<T extends string>({
  value,
  onChange,
  options,
}: {
  value: T;
  onChange: (next: T) => void;
  options: { id: T; label: string; count?: number }[];
}) {
  return (
    <div
      role="tablist"
      className="inline-flex h-[34px] items-center rounded-oz2-input bg-oz2-bg-sunken p-1 text-oz2-text-muted"
    >
      {options.map((opt) => {
        const active = opt.id === value;
        return (
          <button
            key={opt.id}
            type="button"
            role="tab"
            aria-selected={active}
            onClick={() => onChange(opt.id)}
            className={
              "inline-flex h-full items-center gap-1.5 whitespace-nowrap rounded-[6px] px-3 text-[13.5px] font-medium transition-colors " +
              (active
                ? "bg-oz2-surface text-oz2-text shadow-oz2-sm"
                : "hover:text-oz2-text")
            }
          >
            {opt.label}
            {typeof opt.count === "number" && (
              <span className="font-mono text-[11.5px] text-oz2-text-faint">
                {opt.count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}

// ─── Page size + pager (clone of NetworksTableV2) ─────────────────────────

function PageSizeCombobox({
  value,
  onChange,
}: {
  value: number;
  onChange: (next: number) => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const choices = [10, 25, 50, 100, 1000];

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="inline-flex h-[34px] items-center gap-1.5 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 text-[14px] font-medium text-oz2-text-2 hover:bg-oz2-hover hover:border-oz2-border-strong"
      >
        <span className="text-oz2-text-faint">Rows:</span>
        <span className="font-mono">{value}</span>
        <span className="text-oz2-text-faint">{ICONS.chevDown}</span>
      </button>
      {open && (
        <div className="absolute right-0 top-full z-30 mt-1 min-w-[110px] overflow-hidden rounded-oz2-input border border-oz2-border bg-oz2-bg-elev shadow-oz2-md">
          <ul className="py-1">
            {choices.map((c) => (
              <li key={c}>
                <button
                  type="button"
                  onClick={() => {
                    onChange(c);
                    setOpen(false);
                  }}
                  className={
                    "flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left text-[13.5px] hover:bg-oz2-hover " +
                    (c === value ? "text-oz2-acc-text" : "text-oz2-text")
                  }
                >
                  <span className="font-mono">{c}</span>
                  <span className="text-oz2-text-faint">rows</span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function Pager({
  page,
  totalPages,
  onChange,
}: {
  page: number;
  totalPages: number;
  onChange: (next: number) => void;
}) {
  const canPrev = page > 1;
  const canNext = page < totalPages;
  return (
    <div className="flex items-center gap-1">
      <PagerBtn
        disabled={!canPrev}
        onClick={() => onChange(page - 1)}
        aria-label="Previous page"
      >
        <span className="rotate-90">{ICONS.chevDown}</span>
      </PagerBtn>
      <span className="px-2 font-mono text-[13px] tabular-nums text-oz2-text-muted">
        {page} / {totalPages}
      </span>
      <PagerBtn
        disabled={!canNext}
        onClick={() => onChange(page + 1)}
        aria-label="Next page"
      >
        <span className="-rotate-90">{ICONS.chevDown}</span>
      </PagerBtn>
    </div>
  );
}

function PagerBtn({
  disabled,
  onClick,
  children,
  ...props
}: {
  disabled?: boolean;
  onClick: () => void;
  children: React.ReactNode;
} & Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, "onClick">) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={
        "grid h-7 w-7 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 transition-colors " +
        (disabled
          ? "opacity-40"
          : "hover:border-oz2-border-strong hover:bg-oz2-hover")
      }
      {...props}
    >
      {children}
    </button>
  );
}

// ─── Icons ─────────────────────────────────────────────────────────────────

const baseIcon = (path: React.ReactNode) => (
  <svg
    viewBox="0 0 24 24"
    width={16}
    height={16}
    fill="none"
    stroke="currentColor"
    strokeWidth={1.7}
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    {path}
  </svg>
);

const ICONS = {
  search: baseIcon(
    <>
      <circle cx={11} cy={11} r={7} />
      <path d="m20 20-3.5-3.5" />
    </>,
  ),
  chevDown: baseIcon(<path d="m6 9 6 6 6-6" />),
  refresh: baseIcon(
    <>
      <path d="M21 12a9 9 0 1 1-3.5-7.1" />
      <path d="M21 4v5h-5" />
    </>,
  ),
  sortAsc: (
    <svg
      viewBox="0 0 24 24"
      width={11}
      height={11}
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="m6 15 6-6 6 6" />
    </svg>
  ),
  sortDesc: (
    <svg
      viewBox="0 0 24 24"
      width={11}
      height={11}
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="m6 9 6 6 6-6" />
    </svg>
  ),
  sortIdle: (
    <svg
      viewBox="0 0 24 24"
      width={11}
      height={11}
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="m6 9 6-6 6 6" />
      <path d="m6 15 6 6 6-6" />
    </svg>
  ),
};
