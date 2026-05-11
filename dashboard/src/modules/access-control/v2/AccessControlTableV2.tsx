"use client";

import { Modal } from "@components/modal/Modal";
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
import { BookOpen, Network, PlusCircle, ShieldCheck } from "lucide-react";
import { useSearchParams } from "next/navigation";
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
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import {
  AccessControlModalContent,
  AccessControlUpdateModal,
} from "@/modules/access-control/AccessControlModal";
import AccessControlActionCellV2 from "@/modules/access-control/v2/cells/AccessControlActionCellV2";
import AccessControlActiveCell from "@/modules/access-control/table/AccessControlActiveCell";
import AccessControlDestinationsCell from "@/modules/access-control/table/AccessControlDestinationsCell";
import AccessControlDirectionCell from "@/modules/access-control/table/AccessControlDirectionCell";
import AccessControlNameCell from "@/modules/access-control/table/AccessControlNameCell";
import AccessControlPortsCell from "@/modules/access-control/table/AccessControlPortsCell";
import AccessControlPostureCheckCell from "@/modules/access-control/table/AccessControlPostureCheckCell";
import AccessControlProtocolCell from "@/modules/access-control/table/AccessControlProtocolCell";
import AccessControlSourcesCell from "@/modules/access-control/table/AccessControlSourcesCell";

// AccessControlTableV2 — phase-5.6 v2 paint over real /api/policies data.
// Mirrors SetupKeysTableV2 chrome (header + stat badges + toolbar +
// TanStack table + cold-start hero) with Policy columns. Cell renderers
// are reused from the legacy table unchanged so the only delta is paint,
// not behavior. Page-level wrapping (GroupsProvider, PoliciesProvider)
// stays in the page entry point so the cells' usePolicies() / useGroups()
// hooks resolve correctly.

interface Props {
  policies: Policy[] | undefined;
  isLoading: boolean;
}

type EnabledFilter = "all" | "active" | "inactive";

const noopFilter: FilterFn<unknown> = () => true;
const noopSort: SortingFn<unknown> = () => 0;
const NOOP_FILTER_FNS = {
  fuzzy: noopFilter,
  dateRange: noopFilter,
  exactMatch: noopFilter,
  arrIncludesSomeExact: noopFilter,
};
const NOOP_SORTING_FNS = { checkbox: noopSort };

export default function AccessControlTableV2({ policies, isLoading }: Props) {
  const { mutate } = useSWRConfig();
  const { permission } = usePermissions();
  const params = useSearchParams();
  const idParam = params.get("id") ?? undefined;

  // Create-modal state lives at the view level so the topbar slot CTA
  // and the cold-start hero CTA both open the same modal instance.
  // AccessControlModalContent is rendered inline inside the page so it
  // resolves usePolicies() / useGroups() from the providers wrapping
  // this component (the page wraps both).
  const [createOpen, setCreateOpen] = useState(false);

  // Edit modal — opened by a row click. Mirrors the legacy
  // AccessControlTable behavior, including the cell-name passthrough
  // so the form scrolls to the clicked column on mount.
  const [editPolicy, setEditPolicy] = useState<Policy | null>(null);
  const [editCell, setEditCell] = useState<string>("");

  useV2TopbarRight(
    <OzButton
      variant="primary"
      type="button"
      onClick={() => setCreateOpen(true)}
      disabled={!permission.policies.create}
    >
      <PlusCircle size={14} />
      Add Policy
    </OzButton>,
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

  const all = useMemo(() => policies ?? [], [policies]);

  const counts = useMemo(() => {
    let active = 0;
    let inactive = 0;
    for (const p of all) {
      if (p.enabled) active += 1;
      else inactive += 1;
    }
    return { total: all.length, active, inactive };
  }, [all]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return all.filter((p) => {
      if (idParam && p.id !== idParam) return false;
      if (enabledFilter === "active" && !p.enabled) return false;
      if (enabledFilter === "inactive" && p.enabled) return false;
      if (!q) return true;
      const haystack = [p.name, p.description].filter(Boolean).join(" ").toLowerCase();
      return haystack.includes(q);
    });
  }, [all, search, enabledFilter, idParam]);

  useEffect(() => {
    setPagination((prev) => ({ ...prev, pageIndex: 0 }));
  }, [search, enabledFilter]);

  const columns = useMemo<ColumnDef<Policy>[]>(
    () => [
      {
        id: "name",
        accessorFn: (p) => p.name ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Name" />,
        cell: ({ row }) => <AccessControlNameCell policy={row.original} />,
      },
      {
        id: "enabled",
        accessorFn: (p) => p.enabled,
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Active" />,
        cell: ({ row }) => <AccessControlActiveCell policy={row.original} />,
      },
      {
        id: "sources",
        accessorFn: (p) => firstRule(p)?.sources?.length ?? 0,
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Sources" />,
        cell: ({ row }) => <AccessControlSourcesCell policy={row.original} />,
      },
      {
        id: "direction",
        accessorFn: (p) => firstRule(p)?.bidirectional ?? true,
        sortingFn: "basic",
        header: ({ column }) => (
          <SortHeader column={column} label="Direction" />
        ),
        cell: ({ row }) => <AccessControlDirectionCell policy={row.original} />,
      },
      {
        id: "destinations",
        accessorFn: (p) => firstRule(p)?.destinations?.length ?? 0,
        sortingFn: "basic",
        header: ({ column }) => (
          <SortHeader column={column} label="Destinations" />
        ),
        cell: ({ row }) => (
          <AccessControlDestinationsCell policy={row.original} />
        ),
      },
      {
        id: "protocol",
        accessorFn: (p) => firstRule(p)?.protocol ?? "",
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Protocol" />,
        cell: ({ row }) => <AccessControlProtocolCell policy={row.original} />,
      },
      {
        id: "ports",
        accessorFn: (p) => firstRule(p)?.ports?.length ?? 0,
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Ports" />,
        cell: ({ row }) => <AccessControlPortsCell policy={row.original} />,
      },
      {
        id: "posture_checks",
        accessorFn: (p) => p.source_posture_checks?.length ?? 0,
        sortingFn: "basic",
        header: ({ column }) => (
          <SortHeader column={column} label="Posture Checks" />
        ),
        cell: ({ row }) => (
          <AccessControlPostureCheckCell policy={row.original} />
        ),
      },
      {
        id: "actions",
        size: 40,
        enableSorting: false,
        header: () => null,
        cell: ({ row }) => (
          <AccessControlActionCellV2
            policy={row.original}
            onEdit={() => {
              setEditPolicy(row.original);
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
    getRowId: (p) => p.id ?? "",
    filterFns: NOOP_FILTER_FNS,
    sortingFns: NOOP_SORTING_FNS,
  });

  const refreshClick = () => {
    setRefreshing(true);
    Promise.all([mutate("/policies"), mutate("/groups")]).finally(() =>
      setRefreshing(false),
    );
  };

  // Row click → open AccessControlUpdateModal with the cell name so
  // the form scrolls to that field (mirrors legacy onRowClick wiring).
  // Skip the click when the user actually clicked an interactive child
  // (button, toggle, link) so we don't double-fire — those targets
  // already handle their own action.
  const handleRowClick = (row: Row<Policy>, event: React.MouseEvent) => {
    const target = event.target as HTMLElement;
    if (target.closest("button, a, [role='switch'], input, [data-stop-row-click]")) {
      return;
    }
    const cellEl = target.closest("td");
    const cellId = cellEl?.getAttribute("data-cell-id") ?? "";
    setEditPolicy(row.original);
    setEditCell(cellId);
  };

  const pageInfo = table.getState().pagination;
  const total = filtered.length;
  const pageStart = total === 0 ? 0 : pageInfo.pageIndex * pageInfo.pageSize + 1;
  const pageEnd = Math.min(total, (pageInfo.pageIndex + 1) * pageInfo.pageSize);

  // Cold-start: no policies created yet. Page header + description
  // stay visible; the stats row + filter toolbar + table block are
  // swapped for an OzEmptyState hero. Mirrors PeersTableV2 /
  // NetworksTableV2 / SetupKeysTableV2.
  const isColdStart = !isLoading && all.length === 0;

  return (
    <TooltipProvider delayDuration={250} skipDelayDuration={100}>
      <div className="space-y-6 p-8">
        <header>
          <h1 className="text-[24px] font-semibold tracking-tight">
            Access Control Policies
          </h1>
          <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
            Create rules to manage access in your network and define what peers
            can connect. Learn more about{" "}
            <a
              href="https://docs.openzro.io/how-to/manage-network-access"
              target="_blank"
              rel="noopener noreferrer"
              className="text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Access Controls
            </a>
            .
          </p>
        </header>

        {/* Create modal — controlled, opened from the topbar CTA or the
            cold-start hero CTA. Rendered inline so usePolicies() etc.
            resolve from the page-level providers. */}
        <Modal
          open={createOpen}
          onOpenChange={setCreateOpen}
          key={createOpen ? "create-open" : "create-closed"}
        >
          {createOpen && (
            <AccessControlModalContent
              onSuccess={() => setCreateOpen(false)}
            />
          )}
        </Modal>

        {/* Edit modal — opened by row click. AccessControlUpdateModal
            already manages its own Modal wrapper. */}
        {editPolicy && (
          <AccessControlUpdateModal
            policy={editPolicy}
            open={Boolean(editPolicy)}
            onOpenChange={(next) => {
              if (!next) {
                setEditPolicy(null);
                setEditCell("");
              }
            }}
            cell={editCell}
          />
        )}

        {isColdStart ? (
          <AccessControlEmptyState
            canCreate={permission.policies.create}
            onCreate={() => setCreateOpen(true)}
          />
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-[13.5px] text-oz2-text-muted">
              <span className="inline-flex items-center gap-2">
                <span className="font-medium text-oz2-text">{counts.total}</span>
                Policies
              </span>
              <span className="inline-flex items-center gap-2 border-l border-oz2-border-soft pl-5">
                <span className="font-medium text-oz2-text">{counts.active}</span>
                Active
              </span>
              <span className="inline-flex items-center gap-2 border-l border-oz2-border-soft pl-5">
                <span className="font-medium text-oz2-text">
                  {counts.inactive}
                </span>
                Inactive
              </span>
            </div>

            <div className="flex flex-wrap items-center gap-3">
              <div className="inline-flex h-[34px] w-[280px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3">
                <span className="text-oz2-text-faint">{ICONS.search}</span>
                <input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Search by name and description…"
                  className="h-full flex-1 border-0 bg-transparent text-[14px] outline-none placeholder:text-oz2-text-faint"
                />
              </div>

              <SegmentedTabs
                value={enabledFilter}
                onChange={setEnabledFilter}
                options={[
                  { id: "all", label: "All", count: counts.total },
                  { id: "active", label: "Active", count: counts.active },
                  {
                    id: "inactive",
                    label: "Inactive",
                    count: counts.inactive,
                  },
                ]}
              />

              <PageSizeCombobox
                value={pageInfo.pageSize}
                onChange={(n) => table.setPageSize(n)}
              />

              <button
                type="button"
                onClick={refreshClick}
                aria-label="Refresh policies"
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
                          ? "Loading policies…"
                          : "No policies match your filter."}
                      </OzTableCell>
                    </OzTableRow>
                  )}
                </OzTableBody>
              </OzTable>

              <div className="flex flex-wrap items-center justify-between gap-3 border-t border-oz2-border-soft bg-oz2-bg-sunken px-[18px] py-3 text-[13.5px]">
                <span className="text-oz2-text-muted">
                  {total === 0
                    ? "0 policies"
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

// Some Policy fixtures arrive with rules undefined or empty before
// hydration; the legacy table swallowed the resulting throw with a
// try/catch. Centralizing the guard keeps the column accessors
// readable without scattered try/catch blocks.
function firstRule(p: Policy) {
  return p?.rules?.[0];
}

function AccessControlEmptyState({
  canCreate,
  onCreate,
}: {
  canCreate: boolean;
  onCreate: () => void;
}) {
  return (
    <OzEmptyState
      title="Create New Policy"
      description="It looks like you don't have any policies yet. Policies can allow connections by specific protocol and ports."
      primaryAction={
        <OzButton
          variant="primary"
          type="button"
          onClick={onCreate}
          disabled={!canCreate}
        >
          <PlusCircle size={14} />
          Add Policy
        </OzButton>
      }
      learnMore={
        <>
          Learn more about{" "}
          <a
            href="https://docs.openzro.io/how-to/manage-network-access"
            target="_blank"
            rel="noopener noreferrer"
            className="font-medium text-oz2-acc-text underline-offset-2 hover:underline"
          >
            Access Controls
          </a>
          .
        </>
      }
      helperCards={[
        {
          icon: <BookOpen size={16} />,
          title: "How policies work",
          description:
            "Sources, destinations and direction define which peers can talk to each other.",
          href: "https://docs.openzro.io/how-to/manage-network-access",
        },
        {
          icon: <Network size={16} />,
          title: "Group your peers",
          description:
            "Tag machines with groups so a single policy reaches every device in a role.",
          href: "https://docs.openzro.io/how-to/manage-groups",
        },
        {
          icon: <ShieldCheck size={16} />,
          title: "Layer posture checks",
          description:
            "Require OS version, geolocation or process checks before a peer can connect.",
          href: "https://docs.openzro.io/how-to/manage-posture-checks",
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
  column: Column<Policy, unknown>;
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
