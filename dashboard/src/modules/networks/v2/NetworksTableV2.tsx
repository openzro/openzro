"use client";

import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@components/Tooltip";
import {
  Column,
  ColumnDef,
  FilterFn,
  flexRender,
  getCoreRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  PaginationState,
  SortingFn,
  SortingState,
  useReactTable,
} from "@tanstack/react-table";
import { BookOpen, PlusCircle, Route, ShieldCheck } from "lucide-react";
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
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Network } from "@/interfaces/Network";
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import {
  NetworkProvider,
  useNetworksContext,
} from "@/modules/networks/NetworkProvider";
import NetworkActionCell from "@/modules/networks/table/NetworkActionCell";
import NetworkNameCell from "@/modules/networks/table/NetworkNameCell";
import { NetworkPolicyCell } from "@/modules/networks/table/NetworkPolicyCell";
import { NetworkResourceCell } from "@/modules/networks/table/NetworkResourceCell";
import NetworkRoutingPeerCell from "@/modules/networks/table/NetworkRoutingPeerCell";

// NetworksTableV2 — phase-5.2 v2 paint over /api/networks data.
// Mirrors PeersTableV2's chrome (header + stat badges + toolbar +
// TanStack table) but with Network's columns and the legacy
// NetworkProvider wrapping (which exposes openCreateNetworkModal /
// openEditNetworkModal / deleteNetwork to the cells).

interface Props {
  data: Network[] | undefined;
  isLoading: boolean;
}

const noopFilter: FilterFn<unknown> = () => true;
const noopSort: SortingFn<unknown> = () => 0;
const NOOP_FILTER_FNS = {
  fuzzy: noopFilter,
  dateRange: noopFilter,
  exactMatch: noopFilter,
  arrIncludesSomeExact: noopFilter,
};
const NOOP_SORTING_FNS = { checkbox: noopSort };

export default function NetworksTableV2({ data, isLoading }: Props) {
  return (
    <NetworkProvider>
      <NetworksView data={data} isLoading={isLoading} />
    </NetworkProvider>
  );
}

function NetworksView({ data, isLoading }: Props) {
  const { mutate } = useSWRConfig();

  // Mount the Add Network trigger into the V2 topbar's right slot so
  // the action lives next to the theme toggle (matches PeersTableV2).
  // AddNetworkButtonV2 reads NetworksContext for openCreateNetworkModal,
  // so it has to be rendered from inside NetworkProvider — which is
  // exactly where this view is mounted.
  useV2TopbarRight(<AddNetworkButtonV2 />);

  const [search, setSearch] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const [sorting, setSorting] = useState<SortingState>([
    { id: "name", desc: false },
  ]);
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  });

  const all = useMemo(() => data ?? [], [data]);

  const stats = useMemo(() => {
    let resources = 0;
    let routers = 0;
    for (const n of all) {
      resources += n.resources?.length ?? 0;
      routers += n.routers?.length ?? 0;
    }
    return { total: all.length, resources, routers };
  }, [all]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return all;
    return all.filter(
      (n) =>
        n.name?.toLowerCase().includes(q) ||
        n.description?.toLowerCase().includes(q),
    );
  }, [all, search]);

  useEffect(() => {
    setPagination((prev) => ({ ...prev, pageIndex: 0 }));
  }, [search]);

  const columns = useMemo<ColumnDef<Network>[]>(
    () => [
      {
        id: "name",
        accessorFn: (n) => n.name ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Network" />,
        cell: ({ row }) => <NetworkNameCell network={row.original} />,
      },
      {
        id: "resources",
        accessorFn: (n) => n.resources?.length ?? 0,
        sortingFn: "basic",
        header: ({ column }) => (
          <SortHeader column={column} label="Resources" />
        ),
        cell: ({ row }) => <NetworkResourceCell network={row.original} />,
      },
      {
        id: "policies",
        accessorFn: (n) => n.policies?.length ?? 0,
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Policies" />,
        cell: ({ row }) => <NetworkPolicyCell network={row.original} />,
      },
      {
        id: "routers",
        accessorFn: (n) => n.routers?.length ?? 0,
        sortingFn: "basic",
        header: ({ column }) => (
          <SortHeader column={column} label="Routing peers" />
        ),
        cell: ({ row }) => <NetworkRoutingPeerCell network={row.original} />,
      },
      {
        id: "actions",
        size: 40,
        enableSorting: false,
        header: () => null,
        cell: ({ row }) => <NetworkActionCell network={row.original} />,
      },
    ],
    [],
  );

  const table = useReactTable({
    data: filtered,
    columns,
    state: { sorting, pagination },
    onSortingChange: setSorting,
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getRowId: (n) => n.id,
    filterFns: NOOP_FILTER_FNS,
    sortingFns: NOOP_SORTING_FNS,
  });

  const refreshClick = () => {
    setRefreshing(true);
    mutate("/networks").finally(() => setRefreshing(false));
  };

  const pageInfo = table.getState().pagination;
  const total = filtered.length;
  const pageStart = total === 0 ? 0 : pageInfo.pageIndex * pageInfo.pageSize + 1;
  const pageEnd = Math.min(total, (pageInfo.pageIndex + 1) * pageInfo.pageSize);

  // Cold-start: no networks created yet. Keep the page header + Add
  // Network CTA visible (so the operator still has the orientation
  // copy and an action affordance), and replace the stats row +
  // search/page toolbar + table with a centered "get started" hero.
  // Mirrors PeersTableV2 cold-start; copy comes from the legacy
  // GetStartedTest at /networks.
  const isColdStart = !isLoading && all.length === 0;

  return (
    <TooltipProvider delayDuration={250} skipDelayDuration={100}>
      <div className="space-y-6 p-8">
        <header>
          <h1 className="text-[24px] font-semibold tracking-tight">Networks</h1>
          <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
            Networks allow you to access internal resources in LANs and VPCs
            without installing Openzro on every machine. Learn more about{" "}
            <a
              href="https://docs.openzro.io/how-to/networks"
              target="_blank"
              rel="noopener noreferrer"
              className="text-oz2-acc-text underline-offset-2 hover:underline"
            >
              networks
            </a>
            .
          </p>
        </header>

        {isColdStart ? (
          <NetworksEmptyState />
        ) : (
        <>
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-[13.5px] text-oz2-text-muted">
          <span className="inline-flex items-center gap-2">
            <span className="font-medium text-oz2-text">{stats.total}</span>
            Networks
          </span>
          <span className="inline-flex items-center gap-2 border-l border-oz2-border-soft pl-5">
            <span className="font-medium text-oz2-text">{stats.resources}</span>
            Resources
          </span>
          <span className="inline-flex items-center gap-2 border-l border-oz2-border-soft pl-5">
            <span className="font-medium text-oz2-text">{stats.routers}</span>
            Routing peers
          </span>
        </div>

        <div className="flex flex-wrap items-center gap-3">
          <div className="inline-flex h-[34px] flex-1 min-w-[220px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3">
            <span className="text-oz2-text-faint">{ICONS.search}</span>
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search by network name or description…"
              className="h-full flex-1 border-0 bg-transparent text-[14px] outline-none placeholder:text-oz2-text-faint"
            />
          </div>

          <PageSizeCombobox
            value={pageInfo.pageSize}
            onChange={(n) => table.setPageSize(n)}
          />

          <button
            type="button"
            onClick={refreshClick}
            aria-label="Refresh networks"
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
                    {isLoading
                      ? "Loading networks…"
                      : search
                        ? "No networks match your search."
                        : "No networks yet — click Add Network to create one."}
                  </OzTableCell>
                </OzTableRow>
              )}
            </OzTableBody>
          </OzTable>

          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-oz2-border-soft bg-oz2-bg-sunken px-[18px] py-3 text-[13.5px]">
            <span className="text-oz2-text-muted">
              {total === 0
                ? "0 networks"
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
      </div>
    </TooltipProvider>
  );
}

// NetworksEmptyState — cold-start hero shown when no networks exist.
// Delegates the visual to OzEmptyState (mesh emblem + helper-card row)
// so /peers and /networks share the same paint. Copy is preserved
// from the legacy NetworksTable GetStartedTest. The Add Network CTA
// is the same one the page-header renders, since AddNetworkButtonV2
// already lives next to NetworksContext via NetworkProvider.
function NetworksEmptyState() {
  return (
    <OzEmptyState
      title="Create New Network"
      description="It looks like you don't have any networks. Access internal resources in your LANs and VPC by adding a network."
      primaryAction={<AddNetworkButtonV2 />}
      learnMore={
        <>
          Learn more about{" "}
          <a
            href="https://docs.openzro.io/how-to/networks"
            target="_blank"
            rel="noopener noreferrer"
            className="font-medium text-oz2-acc-text underline-offset-2 hover:underline"
          >
            Networks
          </a>
          .
        </>
      }
      helperCards={[
        {
          icon: <BookOpen size={16} />,
          title: "What are networks?",
          description:
            "Reach internal LANs and VPCs without installing Openzro on every machine.",
          href: "https://docs.openzro.io/how-to/networks",
        },
        {
          icon: <Route size={16} />,
          title: "Routing peers",
          description: "Designate peers that route traffic to internal resources.",
          href: "https://docs.openzro.io/how-to/networks#routing-peers",
        },
        {
          icon: <ShieldCheck size={16} />,
          title: "Resources & policies",
          description:
            "Define what's reachable inside the network and who can access it.",
          href: "https://docs.openzro.io/how-to/networks#resources",
        },
      ]}
    />
  );
}

// ─── Add Network button (in-page, since it needs NetworksContext) ────────

function AddNetworkButtonV2() {
  const { openCreateNetworkModal } = useNetworksContext();
  const { permission } = usePermissions();
  return (
    <OzButton
      variant="primary"
      onClick={openCreateNetworkModal}
      disabled={!permission.networks.create}
      type="button"
    >
      <PlusCircle size={14} />
      Add Network
    </OzButton>
  );
}

// ─── Sortable header ────────────────────────────────────────────────────

function SortHeader({
  column,
  label,
}: {
  column: Column<Network, unknown>;
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

// ─── PageSizeCombobox + Pager (mirrors PeersTableV2) ─────────────────────

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
      <path d="m18 15-6-6-6 6" />
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
      <path d="m6 9 6-6 6 6M6 15l6 6 6-6" />
    </svg>
  ),
};
