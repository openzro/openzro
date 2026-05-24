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
  Row,
  SortingFn,
  SortingState,
  useReactTable,
} from "@tanstack/react-table";
import { BookOpen, Globe, PlusCircle, ShieldCheck } from "lucide-react";
import { useRouter } from "next/navigation";
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
import { DNSZone } from "@/interfaces/DNSZone";
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import DnsTabs from "@/modules/dns/v2/DnsTabs";
import DNSZoneActionCell from "@/modules/dns-zones/cells/DNSZoneActionCell";
import DNSZoneActiveCell from "@/modules/dns-zones/cells/DNSZoneActiveCell";
import DNSZoneDistributionCell from "@/modules/dns-zones/cells/DNSZoneDistributionCell";
import DNSZoneNameCell from "@/modules/dns-zones/cells/DNSZoneNameCell";
import DNSZoneRecordsCell from "@/modules/dns-zones/cells/DNSZoneRecordsCell";
import DNSZoneModal from "@/modules/dns-zones/DNSZoneModal";

interface Props {
  zones: DNSZone[] | undefined;
  isLoading: boolean;
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

export default function DNSZonesTable({ zones, isLoading }: Props) {
  const router = useRouter();
  const { mutate } = useSWRConfig();
  const { permission } = usePermissions();

  // Track the row being edited by id (kebab → Edit opens the modal).
  // Row click goes to the detail page instead; that's where records
  // live now.
  const [editZoneId, setEditZoneId] = useState<string | null>(null);
  const [openCreate, setOpenCreate] = useState(false);

  useV2TopbarRight(
    <OzButton
      variant="primary"
      type="button"
      disabled={!permission.dns_zones.create}
      onClick={() => setOpenCreate(true)}
    >
      <PlusCircle size={14} />
      Add Zone
    </OzButton>,
  );

  const [search, setSearch] = useState("");
  const [enabledFilter, setEnabledFilter] = useState<EnabledFilter>("all");
  const [refreshing, setRefreshing] = useState(false);
  const [sorting, setSorting] = useState<SortingState>([
    { id: "name", desc: false },
  ]);
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  });

  const all = useMemo(() => zones ?? [], [zones]);

  const counts = useMemo(() => {
    let enabled = 0;
    let disabled = 0;
    for (const z of all) {
      // Treat undefined as enabled (server default true). Phase 1
      // backend never returns it unset, but a defensive default keeps
      // legacy responses sane.
      if (z.enabled ?? true) enabled += 1;
      else disabled += 1;
    }
    return { total: all.length, enabled, disabled };
  }, [all]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return all.filter((z) => {
      if (enabledFilter === "enabled" && !(z.enabled ?? true)) return false;
      if (!q) return true;
      const haystack = [
        z.name,
        z.domain,
        z.records?.map((r) => `${r.name} ${r.content}`).join(" "),
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

  const columns = useMemo<ColumnDef<DNSZone>[]>(
    () => [
      {
        id: "name",
        accessorFn: (z) => z.name ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Name" />,
        cell: ({ row }) => <DNSZoneNameCell zone={row.original} />,
      },
      {
        id: "enabled",
        accessorFn: (z) => z.enabled ?? true,
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Active" />,
        cell: ({ row }) => <DNSZoneActiveCell zone={row.original} />,
      },
      {
        id: "records",
        accessorFn: (z) => z.records?.length ?? 0,
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Records" />,
        cell: ({ row }) => <DNSZoneRecordsCell zone={row.original} />,
      },
      {
        id: "groups",
        accessorFn: (z) => z.distribution_groups?.length ?? 0,
        sortingFn: "basic",
        header: ({ column }) => (
          <SortHeader column={column} label="Distribution Groups" />
        ),
        cell: ({ row }) => <DNSZoneDistributionCell zone={row.original} />,
      },
      {
        id: "actions",
        size: 40,
        enableSorting: false,
        header: () => null,
        cell: ({ row }) => (
          <DNSZoneActionCell
            zone={row.original}
            onEdit={() => setEditZoneId(row.original.id)}
          />
        ),
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
    getRowId: (z) => z.id ?? "",
    filterFns: NOOP_FILTER_FNS,
    sortingFns: NOOP_SORTING_FNS,
  });

  const refreshClick = () => {
    setRefreshing(true);
    mutate("/dns/zones").finally(() => setRefreshing(false));
  };

  const handleRowClick = (row: Row<DNSZone>, event: React.MouseEvent) => {
    const target = event.target as HTMLElement;
    if (
      target.closest(
        "button, a, [role='switch'], input, select, [data-stop-row-click]",
      )
    ) {
      return;
    }
    // Row click navigates to the zone detail page (records live
    // there, Cloudflare-style). Zone metadata editing is reachable
    // from the kebab menu or the detail-page "Edit zone" CTA.
    router.push(`/dns/zone?id=${row.original.id}`);
  };

  const pageInfo = table.getState().pagination;
  const total = filtered.length;
  const pageStart =
    total === 0 ? 0 : pageInfo.pageIndex * pageInfo.pageSize + 1;
  const pageEnd = Math.min(total, (pageInfo.pageIndex + 1) * pageInfo.pageSize);

  const isColdStart = !isLoading && all.length === 0;

  // Resolve the editing zone from the live `zones` array each render —
  // when /dns/zones is revalidated after a record CRUD inside the
  // modal, this picks up the fresh records slice without forcing the
  // modal to close. If the row was deleted from under us, the lookup
  // returns undefined and the modal closes naturally.
  const editZone = useMemo(
    () => (editZoneId ? all.find((z) => z.id === editZoneId) ?? null : null),
    [editZoneId, all],
  );

  return (
    <TooltipProvider delayDuration={250} skipDelayDuration={100}>
      <div className="space-y-6 p-8">
        <header>
          <h1 className="text-[24px] font-semibold tracking-tight">DNS</h1>
          <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
            Operator-managed authoritative DNS zones distributed to peers in
            selected groups. Learn more about{" "}
            <a
              href="https://docs.openzro.io/how-to/manage-dns-in-your-network"
              target="_blank"
              rel="noopener noreferrer"
              className="text-oz2-acc-text underline-offset-2 hover:underline"
            >
              DNS zones
            </a>
            .
          </p>
        </header>

        <DnsTabs />

        {openCreate && (
          <DNSZoneModal
            open={openCreate}
            onOpenChange={(next) => setOpenCreate(next)}
          />
        )}

        {editZone && (
          <DNSZoneModal
            preset={editZone}
            open={Boolean(editZone)}
            onOpenChange={(next) => {
              if (!next) setEditZoneId(null);
            }}
          />
        )}

        {isColdStart ? (
          <DNSZonesEmptyState
            canCreate={permission.dns_zones.create}
            onCreate={() => setOpenCreate(true)}
          />
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2.5">
              <div className="inline-flex h-8 w-[280px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-2.5">
                <span className="text-oz2-text-faint">{ICONS.search}</span>
                <input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Search by name, domain or record…"
                  className="h-full flex-1 border-0 bg-transparent text-[12.5px] outline-none placeholder:text-oz2-text-faint"
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
                aria-label="Refresh zones"
                className="grid h-8 w-8 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 hover:border-oz2-border-strong hover:bg-oz2-hover"
              >
                <span className={refreshing ? "animate-spin text-oz2-acc" : ""}>
                  {ICONS.refresh}
                </span>
              </button>

              <span className="ml-auto font-mono text-[11px] uppercase tracking-[0.04em] text-oz2-text-faint">
                {counts.total} zone
                {counts.total === 1 ? "" : "s"}
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
                    <OzTableRow
                      key={row.id}
                      onClick={(e) => handleRowClick(row, e)}
                      className="cursor-pointer"
                    >
                      {row.getVisibleCells().map((cell) => (
                        <OzTableCell
                          key={cell.id}
                          data-cell-id={cell.column.id}
                        >
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
                          ? "Loading zones…"
                          : "No zones match your filter."}
                      </OzTableCell>
                    </OzTableRow>
                  )}
                </OzTableBody>
              </OzTable>

              <div className="flex flex-wrap items-center justify-between gap-3 border-t border-oz2-border-soft bg-oz2-bg-sunken px-[18px] py-3 text-[13.5px]">
                <span className="text-oz2-text-muted">
                  {total === 0
                    ? "0 zones"
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

function DNSZonesEmptyState({
  canCreate,
  onCreate,
}: {
  canCreate: boolean;
  onCreate: () => void;
}) {
  return (
    <OzEmptyState
      title="Create DNS Zone"
      description="Authoritative DNS zones let you publish internal hostnames to peers in selected groups — no external nameservers needed."
      primaryAction={
        <OzButton
          variant="primary"
          type="button"
          disabled={!canCreate}
          onClick={onCreate}
        >
          <PlusCircle size={14} />
          Add Zone
        </OzButton>
      }
      learnMore={
        <>
          Learn more about{" "}
          <a
            href="https://docs.openzro.io/how-to/manage-dns-in-your-network"
            target="_blank"
            rel="noopener noreferrer"
            className="font-medium text-oz2-acc-text underline-offset-2 hover:underline"
          >
            DNS zones
          </a>
          .
        </>
      }
      helperCards={[
        {
          icon: <BookOpen size={16} />,
          title: "Authoritative resolution",
          description:
            "Peers answer queries inside the zone locally with NXDOMAIN on miss — no upstream fall-through.",
          href: "https://docs.openzro.io/how-to/manage-dns-in-your-network",
        },
        {
          icon: <Globe size={16} />,
          title: "A / AAAA / CNAME",
          description:
            "Publish IPv4, IPv6 and alias records under a custom domain. Per-record TTL configurable.",
          href: "https://docs.openzro.io/how-to/manage-dns-in-your-network",
        },
        {
          icon: <ShieldCheck size={16} />,
          title: "Scope by group",
          description:
            "Distribute each zone only to the peer groups that need it. Toggle off to pause without deleting.",
          href: "https://docs.openzro.io/how-to/manage-dns-in-your-network",
        },
      ]}
    />
  );
}

function SortHeader({
  column,
  label,
}: {
  column: Column<DNSZone, unknown>;
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
        {sorted === "asc"
          ? ICONS.sortAsc
          : sorted === "desc"
            ? ICONS.sortDesc
            : ICONS.sortIdle}
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
      className="inline-flex h-8 items-center rounded-oz2-input bg-oz2-bg-sunken p-1 text-oz2-text-muted"
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
              "inline-flex h-full items-center gap-1.5 whitespace-nowrap rounded-[6px] px-3 text-[13px] font-medium transition-colors " +
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
        className="inline-flex h-8 items-center gap-1.5 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 text-[13px] font-medium text-oz2-text-2 hover:bg-oz2-hover hover:border-oz2-border-strong"
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
