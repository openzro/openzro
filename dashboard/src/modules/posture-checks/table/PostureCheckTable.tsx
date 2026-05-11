"use client";

import InlineLink from "@components/InlineLink";
import { TooltipProvider } from "@components/Tooltip";
import { useLocalStorage } from "@hooks/useLocalStorage";
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
import useFetchApi from "@utils/api";
import { ExternalLinkIcon, PlusCircle, ShieldCheck } from "lucide-react";
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
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Policy } from "@/interfaces/Policy";
import { PostureCheck } from "@/interfaces/PostureCheck";
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import PostureCheckModal from "@/modules/posture-checks/modal/PostureCheckModal";
import { PostureCheckActionCellV2 } from "@/modules/posture-checks/table/cells/v2/PostureCheckActionCellV2";
import { PostureCheckChecksCellV2 } from "@/modules/posture-checks/table/cells/v2/PostureCheckChecksCellV2";
import { PostureCheckNameCellV2 } from "@/modules/posture-checks/table/cells/v2/PostureCheckNameCellV2";
import { PostureCheckPolicyUsageCellV2 } from "@/modules/posture-checks/table/cells/v2/PostureCheckPolicyUsageCellV2";

type Props = {
  isLoading: boolean;
  postureChecks: PostureCheck[] | undefined;
  headingTarget?: HTMLHeadingElement | null;
};

// PostureCheckTable — v2 paint. Drop-in replacement for the legacy
// DataTable. Each row carries computed `active`/`policies` fields
// derived from /policies, exactly like the legacy did. Cells reuse
// the existing per-row modules.
//
// Surface mirrors UsersTableV2: toolbar with search, Active / All
// segmented filter, page-size combobox, count chip, and a primary
// Add CTA. Cold-start uses OzEmptyState with helper cards.

type ActiveFilter = "all" | "active";

const noopFilter: FilterFn<unknown> = () => true;
const noopSort: SortingFn<unknown> = () => 0;
const NOOP_FILTER_FNS = {
  fuzzy: noopFilter,
  dateRange: noopFilter,
  exactMatch: noopFilter,
  arrIncludesSomeExact: noopFilter,
};
const NOOP_SORTING_FNS = { checkbox: noopSort };

type RowData = PostureCheck & { active?: boolean; policies?: Policy[] };

export default function PostureCheckTable({
  postureChecks,
  isLoading,
  headingTarget: _headingTarget,
}: Props) {
  const { permission } = usePermissions();
  const { data: policies } = useFetchApi<Policy[]>("/policies");
  const { mutate } = useSWRConfig();
  const path = usePathname();

  const data = useMemo<RowData[]>(() => {
    if (!postureChecks) return [];
    return postureChecks.map((check) => {
      if (!policies) return check;
      const usage = policies.filter((policy) => {
        if (!policy.source_posture_checks) return false;
        const checks = policy.source_posture_checks as string[];
        return checks.includes(check.id);
      });
      return {
        ...check,
        policies: usage,
        active: usage.some((p) => p.enabled),
      };
    });
  }, [postureChecks, policies]);

  const [sorting, setSorting] = useLocalStorage<SortingState>(
    "openzro-table-sort" + path,
    [{ id: "active", desc: true }],
  );
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  });

  const [search, setSearch] = useState("");
  const [activeFilter, setActiveFilter] = useState<ActiveFilter>("all");
  const [refreshing, setRefreshing] = useState(false);

  const [postureCheckModal, setPostureCheckModal] = useState(false);
  const [currentRow, setCurrentRow] = useState<RowData | undefined>();

  const openCreate = () => {
    setCurrentRow(undefined);
    setPostureCheckModal(true);
  };

  // Mount Add Posture Check trigger into the V2 topbar's right slot,
  // matching the placement other v2 list screens use. The button
  // reuses the table's existing modal state via openCreate so the
  // create flow stays consolidated.
  useV2TopbarRight(
    <AddPostureCheckButtonV2
      canCreate={permission.policies.create && permission.policies.update}
      onCreate={openCreate}
    />,
  );

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return data.filter((row) => {
      if (activeFilter === "active" && !row.active) return false;
      if (!q) return true;
      const haystack = [row.name, row.description]
        .filter(Boolean)
        .join(" ")
        .toLowerCase();
      return haystack.includes(q);
    });
  }, [data, search, activeFilter]);

  useEffect(() => {
    setPagination((prev) => ({ ...prev, pageIndex: 0 }));
  }, [search, activeFilter]);

  const counts = useMemo(() => {
    let active = 0;
    for (const row of data) if (row.active) active += 1;
    return { total: data.length, active };
  }, [data]);

  const columns = useMemo<ColumnDef<RowData>[]>(
    () => [
      {
        id: "name",
        accessorFn: (row) => row.name ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Name" />,
        cell: ({ row }) => <PostureCheckNameCellV2 check={row.original} />,
      },
      {
        id: "checks",
        accessorFn: (row) => Object.keys(row.checks ?? {}).length,
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Checks" />,
        cell: ({ row }) => <PostureCheckChecksCellV2 check={row.original} />,
      },
      {
        id: "access_control_usage",
        enableSorting: false,
        header: () => (
          <span className="font-mono text-[11.5px] font-semibold uppercase tracking-widest text-oz2-text-muted">
            Used by
          </span>
        ),
        cell: ({ row }) => (
          <PostureCheckPolicyUsageCellV2 check={row.original} />
        ),
      },
      {
        id: "active",
        accessorFn: (row) => (row.active ? 1 : 0),
        sortingFn: "basic",
        enableHiding: true,
      },
      {
        id: "actions",
        size: 60,
        enableSorting: false,
        header: () => null,
        cell: ({ row }) => (
          <PostureCheckActionCellV2
            check={row.original}
            onEdit={() => {
              setCurrentRow(row.original);
              setPostureCheckModal(true);
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
    state: {
      sorting,
      pagination,
      columnVisibility: { active: false },
    },
    onSortingChange: setSorting,
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getRowId: (r) => r.id,
    filterFns: NOOP_FILTER_FNS,
    sortingFns: NOOP_SORTING_FNS,
  });

  const refreshClick = () => {
    setRefreshing(true);
    Promise.all([mutate("/posture-checks"), mutate("/policies")]).finally(() =>
      setRefreshing(false),
    );
  };

  const handleRowClick = (row: RowData) => {
    setCurrentRow(row);
    setPostureCheckModal(true);
  };

  const pageInfo = table.getState().pagination;
  const total = filtered.length;
  const pageStart = total === 0 ? 0 : pageInfo.pageIndex * pageInfo.pageSize + 1;
  const pageEnd = Math.min(total, (pageInfo.pageIndex + 1) * pageInfo.pageSize);

  const isColdStart = !isLoading && data.length === 0;

  return (
    <TooltipProvider delayDuration={250} skipDelayDuration={100}>
      {postureCheckModal && (
        <PostureCheckModal
          open={postureCheckModal}
          key={currentRow ? 1 : 0}
          onOpenChange={setPostureCheckModal}
          onSuccess={() => setPostureCheckModal(false)}
          postureCheck={currentRow}
        />
      )}

      {isColdStart ? (
        <PostureChecksEmptyState
          canCreate={
            permission.policies.create && permission.policies.update
          }
          onCreate={openCreate}
        />
      ) : (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2.5">
            <div className="inline-flex h-8 w-[280px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-2.5">
              <span className="text-oz2-text-faint">{ICONS.search}</span>
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search by name and description…"
                className="h-full flex-1 border-0 bg-transparent text-[12.5px] outline-none placeholder:text-oz2-text-faint"
              />
            </div>

            <SegmentedTabs
              value={activeFilter}
              onChange={setActiveFilter}
              options={[
                { id: "all", label: "All", count: counts.total },
                { id: "active", label: "Active", count: counts.active },
              ]}
            />

            <PageSizeCombobox
              value={pageInfo.pageSize}
              onChange={(n) => table.setPageSize(n)}
            />

            <button
              type="button"
              onClick={refreshClick}
              aria-label="Refresh posture checks"
              className="grid h-8 w-8 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 hover:border-oz2-border-strong hover:bg-oz2-hover"
            >
              <span className={refreshing ? "animate-spin text-oz2-acc" : ""}>
                {ICONS.refresh}
              </span>
            </button>

            <span className="ml-auto font-mono text-[11px] uppercase tracking-[0.04em] text-oz2-text-faint">
              {counts.total} check{counts.total === 1 ? "" : "s"}
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
                    onClick={(e) => {
                      const target = e.target as HTMLElement;
                      if (
                        target.closest(
                          "button, a, [role='switch'], input, select, [data-stop-row-click]",
                        )
                      ) {
                        return;
                      }
                      handleRowClick(row.original);
                    }}
                    className="cursor-pointer"
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
                ))}
                {table.getRowModel().rows.length === 0 && (
                  <OzTableRow className="hover:bg-transparent">
                    <OzTableCell
                      colSpan={columns.length}
                      className="px-[18px] py-12 text-center text-oz2-text-muted"
                    >
                      {isLoading
                        ? "Loading posture checks…"
                        : "No posture checks match your filter."}
                    </OzTableCell>
                  </OzTableRow>
                )}
              </OzTableBody>
            </OzTable>

            <div className="flex flex-wrap items-center justify-between gap-3 border-t border-oz2-border-soft bg-oz2-bg-sunken px-[18px] py-3 text-[13.5px]">
              <span className="text-oz2-text-muted">
                {total === 0
                  ? "0 checks"
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
      )}
    </TooltipProvider>
  );
}

function AddPostureCheckButtonV2({
  canCreate,
  onCreate,
}: {
  canCreate: boolean;
  onCreate: () => void;
}) {
  return (
    <OzButton
      variant="primary"
      type="button"
      onClick={onCreate}
      disabled={!canCreate}
    >
      <PlusCircle size={14} />
      Add Posture Check
    </OzButton>
  );
}

function PostureChecksEmptyState({
  canCreate,
  onCreate,
}: {
  canCreate: boolean;
  onCreate: () => void;
}) {
  return (
    <OzEmptyState
      title="Create Posture Check"
      description="Posture checks layer device-state requirements on top of access policies — block non-compliant peers (OS version, geo, MDM/EDR posture, etc.) before they reach the data plane."
      primaryAction={
        <OzButton
          variant="primary"
          type="button"
          onClick={onCreate}
          disabled={!canCreate}
        >
          <PlusCircle size={14} />
          Create Posture Check
        </OzButton>
      }
      learnMore={
        <>
          Learn more about{" "}
          <a
            href="https://docs.openzro.io/how-to/manage-posture-checks"
            target="_blank"
            rel="noopener noreferrer"
            className="font-medium text-oz2-acc-text underline-offset-2 hover:underline"
          >
            Posture Checks
          </a>
          .
        </>
      }
    />
  );
}

// ─── helpers ──────────────────────────────────────────────────────────

function SortHeader({
  column,
  label,
}: {
  column: Column<RowData, unknown>;
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
