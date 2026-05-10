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
import { Layers3Icon, PlusCircle } from "lucide-react";
import { useSearchParams } from "next/navigation";
import * as React from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";
import {
  OzTable,
  OzTableBody,
  OzTableCell,
  OzTableHead,
  OzTableHeader,
  OzTableRow,
} from "@/components/v2/OzTable";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Group } from "@/interfaces/Group";
import { NetworkResource } from "@/interfaces/Network";
import { useNetworksContext } from "@/modules/networks/NetworkProvider";
import { ResourceActionCell } from "@/modules/networks/resources/ResourceActionCell";
import ResourceAddressCell from "@/modules/networks/resources/ResourceAddressCell";
import { ResourceEnabledCell } from "@/modules/networks/resources/ResourceEnabledCell";
import { ResourceGroupCell } from "@/modules/networks/resources/ResourceGroupCell";
import ResourceNameCell from "@/modules/networks/resources/ResourceNameCell";
import { ResourcePolicyCell } from "@/modules/networks/resources/ResourcePolicyCell";

type Props = {
  resources?: NetworkResource[];
  isLoading: boolean;
  headingTarget?: HTMLHeadingElement | null;
};

// ResourcesTable — v2 paint. Drop-in replacement for the legacy
// DataTable + Card wrapper; same data shape (NetworkResource[]) and
// reuses every per-row cell module so cell-level functionality is
// preserved verbatim. Add Resource CTA sits in the table's own
// toolbar (right side, next to the count chip) so the toolbar reads
// as a self-contained action band — mirrors UsersTableV2.

const noopFilter: FilterFn<unknown> = () => true;
const noopSort: SortingFn<unknown> = () => 0;
const NOOP_FILTER_FNS = {
  fuzzy: noopFilter,
  dateRange: noopFilter,
  exactMatch: noopFilter,
  arrIncludesSomeExact: noopFilter,
};
const NOOP_SORTING_FNS = { checkbox: noopSort };

export default function ResourcesTable({
  resources,
  isLoading,
  headingTarget: _headingTarget,
}: Readonly<Props>) {
  const { permission } = usePermissions();
  const params = useSearchParams();
  const focusedResourceId = params.get("resource") ?? undefined;
  const { openResourceModal, network } = useNetworksContext();

  // Default search is the deep-link resource id (so navigations like
  // /network?id=…&resource=… land on a pre-filtered table).
  const [search, setSearch] = useState(focusedResourceId ?? "");
  const [sorting, setSorting] = useState<SortingState>([]);
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  });

  const all = useMemo(() => resources ?? [], [resources]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return all;
    return all.filter((r) => {
      if (r.id && r.id.toLowerCase() === q) return true;
      const haystack: string[] = [];
      if (r.name) haystack.push(r.name);
      if (r.address) haystack.push(r.address);
      if (r.description) haystack.push(r.description);
      const groups = (r.groups as Group[] | undefined) ?? [];
      for (const g of groups) {
        if (g.name) haystack.push(g.name);
      }
      return haystack.join(" ").toLowerCase().includes(q);
    });
  }, [all, search]);

  useEffect(() => {
    setPagination((prev) => ({ ...prev, pageIndex: 0 }));
  }, [search]);

  const columns = useMemo<ColumnDef<NetworkResource>[]>(
    () => [
      {
        id: "name",
        accessorFn: (r) => r.name ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Resource" />,
        cell: ({ row }) => <ResourceNameCell resource={row.original} />,
      },
      {
        id: "address",
        accessorFn: (r) => r.address ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Address" />,
        cell: ({ row }) => <ResourceAddressCell resource={row.original} />,
      },
      {
        id: "enabled",
        accessorFn: (r) => (r.enabled ? 1 : 0),
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Active" />,
        cell: ({ row }) => <ResourceEnabledCell resource={row.original} />,
      },
      {
        id: "groups",
        accessorFn: (r) =>
          ((r.groups as Group[] | undefined) ?? [])
            .map((g) => g.name)
            .join(", "),
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Groups" />,
        cell: ({ row }) => <ResourceGroupCell resource={row.original} />,
      },
      {
        id: "policies",
        enableSorting: false,
        header: () => <span>Policies</span>,
        cell: ({ row }) => <ResourcePolicyCell resource={row.original} />,
      },
      {
        id: "actions",
        size: 60,
        enableSorting: false,
        header: () => null,
        cell: ({ row }) => <ResourceActionCell resource={row.original} />,
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
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getRowId: (r) => r.id ?? "",
    filterFns: NOOP_FILTER_FNS,
    sortingFns: NOOP_SORTING_FNS,
  });

  const pageInfo = table.getState().pagination;
  const total = filtered.length;
  const pageStart = total === 0 ? 0 : pageInfo.pageIndex * pageInfo.pageSize + 1;
  const pageEnd = Math.min(total, (pageInfo.pageIndex + 1) * pageInfo.pageSize);

  const isColdStart = !isLoading && all.length === 0;

  // Cold-start: no resources on this network. Renders an
  // OzCard-on-dashed hero instead of the table chrome.
  if (isColdStart) {
    return (
      <OzCard className="border-dashed">
        <div className="flex flex-col items-center gap-3 px-6 py-10 text-center">
          <div
            aria-hidden
            className="grid h-11 w-11 place-items-center rounded-full border border-oz2-border-soft bg-oz2-bg-sunken text-oz2-text-2"
          >
            <Layers3Icon size={20} />
          </div>
          <div>
            <p className="text-[14px] font-medium text-oz2-text">
              This network has no resources
            </p>
            <p className="mx-auto mt-1 max-w-md text-[12.5px] text-oz2-text-muted">
              Resources can be a single IP, a subnet, or a domain. Add one to
              control what peers in this network can reach.
            </p>
          </div>
          <OzButton
            variant="primary"
            type="button"
            onClick={() => network && openResourceModal(network)}
            disabled={!permission.networks.update}
          >
            <PlusCircle size={14} />
            Add Resource
          </OzButton>
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
              placeholder="Search by name, address or group…"
              className="h-full flex-1 border-0 bg-transparent text-[12.5px] outline-none placeholder:text-oz2-text-faint"
            />
          </div>

          <PageSizeCombobox
            value={pageInfo.pageSize}
            onChange={(n) => table.setPageSize(n)}
          />

          <span className="ml-auto font-mono text-[11px] uppercase tracking-[0.04em] text-oz2-text-faint">
            {total} resource{total === 1 ? "" : "s"}
          </span>

          <OzButton
            variant="primary"
            type="button"
            onClick={() => network && openResourceModal(network)}
            disabled={!permission.networks.update}
          >
            <PlusCircle size={14} />
            Add Resource
          </OzButton>
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
                      ? "Loading resources…"
                      : "No resources match your filter."}
                  </OzTableCell>
                </OzTableRow>
              )}
            </OzTableBody>
          </OzTable>

          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-oz2-border-soft bg-oz2-bg-sunken px-[18px] py-3 text-[13.5px]">
            <span className="text-oz2-text-muted">
              {total === 0
                ? "0 resources"
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

// ─── Sortable header ──────────────────────────────────────────────────

function SortHeader({
  column,
  label,
}: {
  column: Column<NetworkResource, unknown>;
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

// ─── PageSizeCombobox + Pager (same shape as other v2 tables) ─────────

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
        className="inline-flex h-8 items-center gap-1.5 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 text-[13px] font-medium text-oz2-text-2 hover:border-oz2-border-strong hover:bg-oz2-hover"
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
};
