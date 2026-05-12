"use client";

import { TooltipProvider } from "@components/Tooltip";
import {
  Column,
  ColumnDef,
  FilterFn,
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  PaginationState,
  SortingFn,
  SortingState,
  useReactTable,
} from "@tanstack/react-table";
import * as React from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useSWRConfig } from "swr";
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
import { Peer } from "@/interfaces/Peer";
import PeerAddressCell from "@/modules/peers/PeerAddressCell";
import PeerLastSeenCell from "@/modules/peers/PeerLastSeenCell";
import PeerNameCell from "@/modules/peers/PeerNameCell";
import { PeerOSCell } from "@/modules/peers/PeerOSCell";

type Props = {
  peers?: Peer[];
  peerID: string;
  isLoading: boolean;
  headingTarget?: HTMLHeadingElement | null;
};

// AccessiblePeersTable — v2 paint. Drop-in replacement for the legacy
// DataTable + Card + ButtonGroup chrome. Cells reuse existing
// per-row modules (PeerNameCell, PeerAddressCell, PeerLastSeenCell,
// PeerOSCell). Same data path and refresh keys; the legacy
// ButtonGroup All/Online/Offline filter is reshaped as a segmented
// pill above the table.

type ConnFilter = "all" | "online" | "offline";

const noopFilter: FilterFn<unknown> = () => true;
const noopSort: SortingFn<unknown> = () => 0;
const NOOP_FILTER_FNS = {
  fuzzy: noopFilter,
  dateRange: noopFilter,
  exactMatch: noopFilter,
  arrIncludesSomeExact: noopFilter,
};
const NOOP_SORTING_FNS = { checkbox: noopSort };

export default function AccessiblePeersTable({
  peers,
  isLoading,
  headingTarget: _headingTarget,
  peerID,
}: Props) {
  const { mutate } = useSWRConfig();

  const [search, setSearch] = useState("");
  const [connFilter, setConnFilter] = useState<ConnFilter>("all");
  const [refreshing, setRefreshing] = useState(false);
  const [sorting, setSorting] = useState<SortingState>([
    { id: "connected", desc: true },
    { id: "last_seen", desc: true },
    { id: "name", desc: false },
  ]);
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  });

  const all = useMemo(() => peers ?? [], [peers]);

  const counts = useMemo(() => {
    let online = 0;
    for (const p of all) if (p.connected) online += 1;
    return { total: all.length, online, offline: all.length - online };
  }, [all]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return all.filter((p) => {
      if (connFilter === "online" && !p.connected) return false;
      if (connFilter === "offline" && p.connected) return false;
      if (!q) return true;
      const haystack = [
        p.name,
        p.ip,
        p.dns_label,
        p.user?.name,
        p.user?.email,
        p.hostname,
      ]
        .filter(Boolean)
        .join(" ")
        .toLowerCase();
      return haystack.includes(q);
    });
  }, [all, search, connFilter]);

  useEffect(() => {
    setPagination((prev) => ({ ...prev, pageIndex: 0 }));
  }, [search, connFilter]);

  const columns = useMemo<ColumnDef<Peer>[]>(
    () => [
      {
        id: "name",
        accessorFn: (p) => p.name ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Name" />,
        cell: ({ row }) => <PeerNameCell peer={row.original} />,
      },
      {
        id: "dns_label",
        accessorFn: (p) => p.dns_label ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Address" />,
        cell: ({ row }) => <PeerAddressCell peer={row.original} />,
      },
      {
        id: "last_seen",
        accessorFn: (p) => p.last_seen ?? "",
        sortingFn: "datetime",
        header: ({ column }) => (
          <SortHeader column={column} label="Last seen" />
        ),
        cell: ({ row }) => <PeerLastSeenCell peer={row.original} />,
      },
      {
        id: "os",
        accessorFn: (p) => p.os ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="OS" />,
        cell: ({ row }) => <PeerOSCell os={row.original.os} />,
      },
      {
        id: "connected",
        accessorFn: (p) => (p.connected ? 1 : 0),
        sortingFn: "basic",
        enableHiding: true,
      },
    ],
    [],
  );

  const table = useReactTable({
    data: filtered,
    columns,
    state: {
      sorting,
      pagination,
      columnVisibility: { connected: false },
    },
    onSortingChange: setSorting,
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getRowId: (p) => p.id ?? "",
    filterFns: NOOP_FILTER_FNS,
    sortingFns: NOOP_SORTING_FNS,
  });

  const refreshClick = () => {
    setRefreshing(true);
    Promise.all([
      mutate("/users"),
      mutate(`/peers/${peerID}/accessible-peers`),
    ]).finally(() => setRefreshing(false));
  };

  const pageInfo = table.getState().pagination;
  const total = filtered.length;
  const pageStart = total === 0 ? 0 : pageInfo.pageIndex * pageInfo.pageSize + 1;
  const pageEnd = Math.min(total, (pageInfo.pageIndex + 1) * pageInfo.pageSize);

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
              This peer has no accessible peers
            </p>
            <p className="mx-auto mt-1 max-w-md text-[12.5px] text-oz2-text-muted">
              Add more peers to your network or check your access control
              policies — at least one policy must permit traffic from this
              peer to another for it to show up here.
            </p>
          </div>
        </div>
      </OzCard>
    );
  }

  return (
    <TooltipProvider delayDuration={250} skipDelayDuration={100}>
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2.5">
          <div className="inline-flex h-8 w-[280px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-2.5">
            <span className="text-oz2-text-faint">{ICONS.search}</span>
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search by name, IP, owner…"
              className="h-full flex-1 border-0 bg-transparent text-[12.5px] outline-none placeholder:text-oz2-text-faint"
            />
          </div>

          <SegmentedTabs
            value={connFilter}
            onChange={setConnFilter}
            options={[
              { id: "all", label: "All", count: counts.total },
              { id: "online", label: "Online", count: counts.online },
              { id: "offline", label: "Offline", count: counts.offline },
            ]}
          />

          <PageSizeCombobox
            value={pageInfo.pageSize}
            onChange={(n) => table.setPageSize(n)}
          />

          <button
            type="button"
            onClick={refreshClick}
            aria-label="Refresh accessible peers"
            className="grid h-8 w-8 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 hover:border-oz2-border-strong hover:bg-oz2-hover"
          >
            <span className={refreshing ? "animate-spin text-oz2-acc" : ""}>
              {ICONS.refresh}
            </span>
          </button>

          <span className="ml-auto font-mono text-[11px] uppercase tracking-[0.04em] text-oz2-text-faint">
            {total} peer{total === 1 ? "" : "s"}
          </span>
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
                      ? "Loading peers…"
                      : "No peers match your filter."}
                  </OzTableCell>
                </OzTableRow>
              )}
            </OzTableBody>
          </OzTable>

          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-oz2-border-soft bg-oz2-bg-sunken px-[18px] py-3 text-[13.5px]">
            <span className="text-oz2-text-muted">
              {total === 0
                ? "0 peers"
                : `Showing ${pageStart}–${pageEnd} of ${total}`}
            </span>
            <Pager
              page={pageInfo.pageIndex + 1}
              totalPages={Math.max(1, table.getPageCount())}
              onChange={(p) => table.setPageIndex(p - 1)}
            />
          </div>
        </OzCard>
      </div>
    </TooltipProvider>
  );
}

// ─── helpers ──────────────────────────────────────────────────────────

function SortHeader({
  column,
  label,
}: {
  column: Column<Peer, unknown>;
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
        className="inline-flex h-[34px] items-center gap-1.5 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 text-[14px] font-medium text-oz2-text-2 hover:border-oz2-border-strong hover:bg-oz2-hover"
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
};
