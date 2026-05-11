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
  SortingFn,
  SortingState,
  useReactTable,
} from "@tanstack/react-table";
import dayjs from "dayjs";
import { BookOpen, KeyRound, PlusCircle, ShieldCheck } from "lucide-react";
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
import { useGroups } from "@/contexts/GroupsProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Group } from "@/interfaces/Group";
import { SetupKey } from "@/interfaces/SetupKey";
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import EmptyRow from "@/modules/common-table-rows/EmptyRow";
import ExpirationDateRow from "@/modules/common-table-rows/ExpirationDateRow";
import LastTimeRow from "@/modules/common-table-rows/LastTimeRow";
import SetupKeyActionCellV2 from "@/modules/setup-keys/v2/cells/SetupKeyActionCellV2";
import SetupKeyGroupsCell from "@/modules/setup-keys/SetupKeyGroupsCell";
import SetupKeyModal from "@/modules/setup-keys/SetupKeyModal";
import SetupKeyNameCell from "@/modules/setup-keys/SetupKeyNameCell";
import SetupKeyStatusCell from "@/modules/setup-keys/SetupKeyStatusCell";
import SetupKeyUsageCell from "@/modules/setup-keys/SetupKeyUsageCell";

// SetupKeysTableV2 — phase-5.4 v2 paint over real /api/setup-keys data.
// Mirrors PeersTableV2/NetworksTableV2 chrome (header + stat badges +
// toolbar + TanStack table + cold-start hero) with SetupKey columns.
// Cell renderers are reused from the legacy module unchanged so the
// only delta is paint, not behavior.

interface Props {
  setupKeys: SetupKey[] | undefined;
  isLoading: boolean;
}

type ValidFilter = "all" | "valid" | "expired";

const noopFilter: FilterFn<unknown> = () => true;
const noopSort: SortingFn<unknown> = () => 0;
const NOOP_FILTER_FNS = {
  fuzzy: noopFilter,
  dateRange: noopFilter,
  exactMatch: noopFilter,
  arrIncludesSomeExact: noopFilter,
};
const NOOP_SORTING_FNS = { checkbox: noopSort };

export default function SetupKeysTableV2({ setupKeys, isLoading }: Props) {
  const { mutate } = useSWRConfig();
  const { groups } = useGroups();
  const { permission } = usePermissions();

  // Modal state lives at the view level so the topbar slot button and
  // the cold-start hero CTA both open the same SetupKeyModal instance.
  const [modalOpen, setModalOpen] = useState(false);

  useV2TopbarRight(
    <OzButton
      variant="primary"
      type="button"
      onClick={() => setModalOpen(true)}
      disabled={!permission.setup_keys.create}
    >
      <PlusCircle size={14} />
      Create Setup Key
    </OzButton>,
  );

  const [search, setSearch] = useState("");
  const [validFilter, setValidFilter] = useState<ValidFilter>("all");
  const [refreshing, setRefreshing] = useState(false);
  const [sorting, setSorting] = useState<SortingState>([
    { id: "valid", desc: true },
    { id: "last_used", desc: true },
    { id: "name", desc: true },
  ]);
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  });

  // Hydrate auto_groups (string[] of group IDs) into Group objects so
  // SetupKeyGroupsCell can render names. Mirrors the legacy page-level
  // mapping so the cell stays unchanged.
  const enriched = useMemo<SetupKey[]>(() => {
    const list = setupKeys ?? [];
    if (!groups) return list;
    return list.map((sk) => {
      if (!sk.auto_groups) return sk;
      const hydrated = sk.auto_groups
        .map((id) => groups.find((g) => g.id === id))
        .filter((g): g is Group => Boolean(g));
      return { ...sk, groups: hydrated };
    });
  }, [setupKeys, groups]);

  const counts = useMemo(() => {
    let valid = 0;
    let expired = 0;
    for (const sk of enriched) {
      if (sk.valid) valid += 1;
      else expired += 1;
    }
    return { total: enriched.length, valid, expired };
  }, [enriched]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return enriched.filter((sk) => {
      if (validFilter === "valid" && !sk.valid) return false;
      if (validFilter === "expired" && sk.valid) return false;
      if (!q) return true;
      const haystack = [
        sk.name,
        sk.type,
        sk.groups?.map((g) => g?.name ?? "").join(" "),
      ]
        .filter(Boolean)
        .join(" ")
        .toLowerCase();
      return haystack.includes(q);
    });
  }, [enriched, search, validFilter]);

  useEffect(() => {
    setPagination((prev) => ({ ...prev, pageIndex: 0 }));
  }, [search, validFilter]);

  const columns = useMemo<ColumnDef<SetupKey>[]>(
    () => [
      {
        id: "name",
        accessorFn: (sk) => sk.name ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Name & Key" />,
        cell: ({ row }) => (
          <SetupKeyNameCell
            name={row.original.name}
            valid={row.original.valid}
            secret={row.original.key}
          />
        ),
      },
      {
        id: "usage",
        accessorFn: (sk) => sk.used_times ?? 0,
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Usage" />,
        cell: ({ row }) => (
          <SetupKeyUsageCell
            current={row.original.used_times}
            limit={row.original.usage_limit || 0}
            reusable={row.original.type === "reusable"}
          />
        ),
      },
      {
        id: "last_used",
        accessorFn: (sk) => sk.last_used ?? "",
        sortingFn: "datetime",
        header: ({ column }) => <SortHeader column={column} label="Last used" />,
        cell: ({ row }) => (
          <LastTimeRow date={row.original.last_used} text="Last used on" />
        ),
      },
      {
        id: "groups",
        accessorFn: (sk) => sk.auto_groups?.length ?? 0,
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Groups" />,
        cell: ({ row }) => <SetupKeyGroupsCell setupKey={row.original} />,
      },
      {
        id: "expires",
        accessorFn: (sk) => sk.expires ?? "",
        sortingFn: "datetime",
        header: ({ column }) => <SortHeader column={column} label="Expires" />,
        cell: ({ row }) => {
          // Backend uses year-1 as a sentinel for "never expires" — keep
          // the legacy detection so the column stays empty for those keys
          // instead of showing a bogus "1/1/0001" date.
          const expires = dayjs(row.original.expires);
          const isNeverExpiring = expires?.year() === 1;
          return isNeverExpiring ? (
            <EmptyRow className="px-3" />
          ) : (
            <ExpirationDateRow date={row.original.expires} />
          );
        },
      },
      {
        id: "status",
        size: 1,
        enableSorting: false,
        header: () => null,
        cell: ({ row }) => <SetupKeyStatusCell setupKey={row.original} />,
      },
      {
        id: "actions",
        size: 40,
        enableSorting: false,
        header: () => null,
        cell: ({ row }) => <SetupKeyActionCellV2 setupKey={row.original} />,
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
    getRowId: (sk) => sk.id,
    filterFns: NOOP_FILTER_FNS,
    sortingFns: NOOP_SORTING_FNS,
  });

  const refreshClick = () => {
    setRefreshing(true);
    Promise.all([mutate("/setup-keys"), mutate("/groups")]).finally(() =>
      setRefreshing(false),
    );
  };

  const pageInfo = table.getState().pagination;
  const total = filtered.length;
  const pageStart = total === 0 ? 0 : pageInfo.pageIndex * pageInfo.pageSize + 1;
  const pageEnd = Math.min(total, (pageInfo.pageIndex + 1) * pageInfo.pageSize);

  // Cold-start: no setup keys exist yet. Page header + description stay
  // visible (operator still gets the orientation copy and the topbar
  // CTA), and the stats row + filter toolbar + table block are replaced
  // by the OzEmptyState hero. Mirrors PeersTableV2 / NetworksTableV2.
  const isColdStart = !isLoading && enriched.length === 0;

  return (
    <TooltipProvider delayDuration={250} skipDelayDuration={100}>
      <div className="space-y-6 p-8">
        <header>
          <h1 className="text-[24px] font-semibold tracking-tight">
            Setup Keys
          </h1>
          <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
            Setup keys are pre-authentication keys that allow to register new
            machines in your network. Learn more about{" "}
            <a
              href="https://docs.openzro.io/how-to/register-machines-using-setup-keys"
              target="_blank"
              rel="noopener noreferrer"
              className="text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Setup Keys
            </a>
            .
          </p>
        </header>

        {modalOpen && (
          <SetupKeyModal open={modalOpen} setOpen={setModalOpen} />
        )}

        {isColdStart ? (
          <SetupKeysEmptyState
            canCreate={permission.setup_keys.create}
            onCreate={() => setModalOpen(true)}
          />
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-[13.5px] text-oz2-text-muted">
              <span className="inline-flex items-center gap-2">
                <span className="font-medium text-oz2-text">{counts.total}</span>
                Total
              </span>
              <span className="inline-flex items-center gap-2 border-l border-oz2-border-soft pl-5">
                <span className="font-medium text-oz2-text">{counts.valid}</span>
                Valid
              </span>
              <span className="inline-flex items-center gap-2 border-l border-oz2-border-soft pl-5">
                <span className="font-medium text-oz2-text">
                  {counts.expired}
                </span>
                Expired
              </span>
            </div>

            <div className="flex flex-wrap items-center gap-3">
              <div className="inline-flex h-[34px] w-[280px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3">
                <span className="text-oz2-text-faint">{ICONS.search}</span>
                <input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Search by name, type or group…"
                  className="h-full flex-1 border-0 bg-transparent text-[14px] outline-none placeholder:text-oz2-text-faint"
                />
              </div>

              <SegmentedTabs
                value={validFilter}
                onChange={setValidFilter}
                options={[
                  { id: "all", label: "All", count: counts.total },
                  { id: "valid", label: "Valid", count: counts.valid },
                  { id: "expired", label: "Expired", count: counts.expired },
                ]}
              />

              <PageSizeCombobox
                value={pageInfo.pageSize}
                onChange={(n) => table.setPageSize(n)}
              />

              <button
                type="button"
                onClick={refreshClick}
                aria-label="Refresh setup keys"
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
                          ? "Loading setup keys…"
                          : "No setup keys match your filter."}
                      </OzTableCell>
                    </OzTableRow>
                  )}
                </OzTableBody>
              </OzTable>

              <div className="flex flex-wrap items-center justify-between gap-3 border-t border-oz2-border-soft bg-oz2-bg-sunken px-[18px] py-3 text-[13.5px]">
                <span className="text-oz2-text-muted">
                  {total === 0
                    ? "0 setup keys"
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

// SetupKeysEmptyState — cold-start hero. Delegates the visual to the
// shared OzEmptyState; copy is preserved verbatim from the legacy
// GetStartedTest at /setup-keys.
function SetupKeysEmptyState({
  canCreate,
  onCreate,
}: {
  canCreate: boolean;
  onCreate: () => void;
}) {
  return (
    <OzEmptyState
      title="Create Setup Key"
      description="Add a setup key to register new machines in your network. The key links machines to your account during initial setup."
      primaryAction={
        <OzButton
          variant="primary"
          type="button"
          onClick={onCreate}
          disabled={!canCreate}
        >
          <PlusCircle size={14} />
          Create Setup Key
        </OzButton>
      }
      learnMore={
        <>
          Learn more about{" "}
          <a
            href="https://docs.openzro.io/how-to/register-machines-using-setup-keys"
            target="_blank"
            rel="noopener noreferrer"
            className="font-medium text-oz2-acc-text underline-offset-2 hover:underline"
          >
            Setup Keys
          </a>
          .
        </>
      }
      helperCards={[
        {
          icon: <BookOpen size={16} />,
          title: "Register machines",
          description:
            "Walk through enrolling Linux, macOS, Windows or Docker hosts using a setup key.",
          href: "https://docs.openzro.io/how-to/register-machines-using-setup-keys",
        },
        {
          icon: <KeyRound size={16} />,
          title: "Reusable vs one-off",
          description:
            "Reusable keys cover bulk enrollment and CI; one-off keys are single-use and safer.",
          href: "https://docs.openzro.io/how-to/register-machines-using-setup-keys",
        },
        {
          icon: <ShieldCheck size={16} />,
          title: "Auto-assigned groups",
          description:
            "Pre-attach groups so machines registered with this key inherit the right policies.",
          href: "https://docs.openzro.io/how-to/manage-network-access",
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
  column: Column<SetupKey, unknown>;
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

// ─── Segmented tabs (mirrors PeersTableV2) ───────────────────────────────

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

// ─── PageSizeCombobox + Pager (mirrors NetworksTableV2) ─────────────────

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
