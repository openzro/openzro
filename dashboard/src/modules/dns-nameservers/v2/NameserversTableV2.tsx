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
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  PaginationState,
  Row,
  SortingFn,
  SortingState,
  useReactTable,
} from "@tanstack/react-table";
import { BookOpen, Globe, PlusCircle, Server, ShieldCheck } from "lucide-react";
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
import { NameserverGroup } from "@/interfaces/Nameserver";
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import DnsTabs from "@/modules/dns/v2/DnsTabs";
import NameserverModal from "@/modules/dns-nameservers/NameserverModal";
import NameserverTemplateModal from "@/modules/dns-nameservers/NameserverTemplateModal";
import NameserverActionCellV2 from "@/modules/dns-nameservers/v2/cells/NameserverActionCellV2";
import NameserverActiveCell from "@/modules/dns-nameservers/table/NameserverActiveCell";
import NameserverDistributionGroupsCell from "@/modules/dns-nameservers/table/NameserverDistributionGroupsCell";
import NameserverMatchDomainsCell from "@/modules/dns-nameservers/table/NameserverMatchDomainsCell";
import NameserverNameCell from "@/modules/dns-nameservers/table/NameserverNameCell";
import NameserverNameserversCell from "@/modules/dns-nameservers/table/NameserverNameserversCell";

// NameserversTableV2 — phase-5.14 v2 paint over /api/dns/nameservers.
// Mirrors the SetupKeysTableV2 / AccessControlTableV2 chrome (header
// + DnsTabs sub-nav + toolbar + TanStack table + cold-start hero)
// with the NameserverGroup columns. Cell renderers reused unchanged
// from the legacy module.
//
// Behavior preserved verbatim from NameserverGroupTable: same SWR
// endpoint, same edit-modal flow (clicking a row opens the form on
// the matching tab via the cell-name passthrough), same enabled/all
// filter, same RestrictedAccess + permission gates.

interface Props {
  nameserverGroups: NameserverGroup[] | undefined;
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

export default function NameserversTableV2({
  nameserverGroups,
  isLoading,
}: Props) {
  const { mutate } = useSWRConfig();
  const { permission } = usePermissions();

  // Edit modal state owned at the view level — opened by row click,
  // optionally pre-selecting the cell-derived tab inside the form.
  const [editGroup, setEditGroup] = useState<NameserverGroup | null>(null);
  const [editCell, setEditCell] = useState<string>("");

  useV2TopbarRight(
    <NameserverTemplateModal>
      <OzButton
        variant="primary"
        type="button"
        disabled={!permission.nameservers.create}
      >
        <PlusCircle size={14} />
        Add Nameserver
      </OzButton>
    </NameserverTemplateModal>,
  );

  const [search, setSearch] = useState("");
  const [enabledFilter, setEnabledFilter] = useState<EnabledFilter>("all");
  const [refreshing, setRefreshing] = useState(false);
  const [sorting, setSorting] = useState<SortingState>([
    { id: "name", desc: true },
  ]);
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  });

  const all = useMemo(() => nameserverGroups ?? [], [nameserverGroups]);

  const counts = useMemo(() => {
    let enabled = 0;
    let disabled = 0;
    for (const g of all) {
      if (g.enabled) enabled += 1;
      else disabled += 1;
    }
    return { total: all.length, enabled, disabled };
  }, [all]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return all.filter((g) => {
      if (enabledFilter === "enabled" && !g.enabled) return false;
      if (!q) return true;
      const haystack = [
        g.name,
        g.description,
        g.domains?.join(" "),
        g.nameservers?.map((n) => n.ip).join(" "),
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

  const columns = useMemo<ColumnDef<NameserverGroup>[]>(
    () => [
      {
        id: "name",
        accessorFn: (g) => g.name ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Name" />,
        cell: ({ row }) => <NameserverNameCell ns={row.original} />,
      },
      {
        id: "enabled",
        accessorFn: (g) => g.enabled,
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Active" />,
        cell: ({ row }) => <NameserverActiveCell ns={row.original} />,
      },
      {
        id: "domains",
        accessorFn: (g) => g.domains?.length ?? 0,
        sortingFn: "basic",
        header: ({ column }) => (
          <SortHeader column={column} label="Match Domains" />
        ),
        cell: ({ row }) => <NameserverMatchDomainsCell ns={row.original} />,
      },
      {
        id: "nameservers",
        accessorFn: (g) => g.nameservers?.length ?? 0,
        sortingFn: "basic",
        header: ({ column }) => (
          <SortHeader column={column} label="Nameservers" />
        ),
        cell: ({ row }) => <NameserverNameserversCell ns={row.original} />,
      },
      {
        id: "groups",
        accessorFn: (g) => g.groups?.length ?? 0,
        sortingFn: "basic",
        header: ({ column }) => (
          <SortHeader column={column} label="Distribution Groups" />
        ),
        cell: ({ row }) => (
          <NameserverDistributionGroupsCell ns={row.original} />
        ),
      },
      {
        id: "actions",
        size: 40,
        enableSorting: false,
        header: () => null,
        cell: ({ row }) => (
          <NameserverActionCellV2
            ns={row.original}
            onEdit={() => {
              setEditGroup(row.original);
              setEditCell("");
            }}
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
    getRowId: (g) => g.id ?? "",
    filterFns: NOOP_FILTER_FNS,
    sortingFns: NOOP_SORTING_FNS,
  });

  const refreshClick = () => {
    setRefreshing(true);
    mutate("/dns/nameservers").finally(() => setRefreshing(false));
  };

  // Row click → open edit modal with the clicked cell so the form
  // can scroll to / focus that tab. Skip when the click landed on
  // an interactive child so toggles / kebabs keep their own
  // behavior.
  const handleRowClick = (
    row: Row<NameserverGroup>,
    event: React.MouseEvent,
  ) => {
    const target = event.target as HTMLElement;
    if (
      target.closest(
        "button, a, [role='switch'], input, select, [data-stop-row-click]",
      )
    ) {
      return;
    }
    const cellEl = target.closest("td");
    const cellId = cellEl?.getAttribute("data-cell-id") ?? "";
    setEditGroup(row.original);
    setEditCell(cellId);
  };

  const pageInfo = table.getState().pagination;
  const total = filtered.length;
  const pageStart = total === 0 ? 0 : pageInfo.pageIndex * pageInfo.pageSize + 1;
  const pageEnd = Math.min(total, (pageInfo.pageIndex + 1) * pageInfo.pageSize);

  const isColdStart = !isLoading && all.length === 0;

  return (
    <TooltipProvider delayDuration={250} skipDelayDuration={100}>
      <div className="space-y-6 p-8">
        <header>
          <h1 className="text-[24px] font-semibold tracking-tight">DNS</h1>
          <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
            Add nameservers for domain name resolution in your Openzro
            network. Learn more about{" "}
            <a
              href="https://docs.openzro.io/how-to/manage-dns-in-your-network"
              target="_blank"
              rel="noopener noreferrer"
              className="text-oz2-acc-text underline-offset-2 hover:underline"
            >
              DNS
            </a>
            .
          </p>
        </header>

        <DnsTabs />

        {/* Edit modal — controlled, opened by row click. The legacy
            NameserverModal accepts open/onOpenChange + cell prop for
            tab-targeting inside the form. */}
        {editGroup && (
          <NameserverModal
            preset={editGroup}
            open={Boolean(editGroup)}
            onOpenChange={(next) => {
              if (!next) {
                setEditGroup(null);
                setEditCell("");
              }
            }}
            cell={editCell}
          />
        )}

        {isColdStart ? (
          <NameserversEmptyState canCreate={permission.nameservers.create} />
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2.5">
              <div className="inline-flex h-8 w-[280px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-2.5">
                <span className="text-oz2-text-faint">{ICONS.search}</span>
                <input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Search by name, domains or nameservers…"
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
                aria-label="Refresh nameservers"
                className="grid h-8 w-8 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 hover:border-oz2-border-strong hover:bg-oz2-hover"
              >
                <span className={refreshing ? "animate-spin text-oz2-acc" : ""}>
                  {ICONS.refresh}
                </span>
              </button>

              <span className="ml-auto font-mono text-[11px] uppercase tracking-[0.04em] text-oz2-text-faint">
                {counts.total} nameserver
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
                          ? "Loading nameservers…"
                          : "No nameservers match your filter."}
                      </OzTableCell>
                    </OzTableRow>
                  )}
                </OzTableBody>
              </OzTable>

              <div className="flex flex-wrap items-center justify-between gap-3 border-t border-oz2-border-soft bg-oz2-bg-sunken px-[18px] py-3 text-[13.5px]">
                <span className="text-oz2-text-muted">
                  {total === 0
                    ? "0 nameservers"
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

function NameserversEmptyState({ canCreate }: { canCreate: boolean }) {
  return (
    <OzEmptyState
      title="Create Nameserver"
      description="It looks like you don't have any nameservers. Get started by adding one to your network. Select a predefined or add your custom nameservers."
      primaryAction={
        <NameserverTemplateModal>
          <OzButton variant="primary" type="button" disabled={!canCreate}>
            <PlusCircle size={14} />
            Add Nameserver
          </OzButton>
        </NameserverTemplateModal>
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
            DNS
          </a>
          .
        </>
      }
      helperCards={[
        {
          icon: <BookOpen size={16} />,
          title: "How DNS works",
          description:
            "Walk through how Openzro intercepts DNS queries on each peer and forwards to your nameservers.",
          href: "https://docs.openzro.io/how-to/manage-dns-in-your-network",
        },
        {
          icon: <Server size={16} />,
          title: "Predefined templates",
          description:
            "Pick Google, Cloudflare, DNS0.EU or Quad9 in one click — TLS-encrypted resolvers ready to go.",
          href: "https://docs.openzro.io/how-to/manage-dns-in-your-network",
        },
        {
          icon: <ShieldCheck size={16} />,
          title: "Distribute by group",
          description:
            "Scope a nameserver group to specific peer groups — only members of those groups resolve through it.",
          href: "https://docs.openzro.io/how-to/manage-dns-in-your-network",
        },
      ]}
    />
  );
}

// ─── Sortable header ────────────────────────────────────────────────────

function SortHeader({
  column,
  label,
}: {
  column: Column<NameserverGroup, unknown>;
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

// ─── Segmented tabs ────────────────────────────────────────────────────

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

// ─── PageSizeCombobox + Pager ──────────────────────────────────────────

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
