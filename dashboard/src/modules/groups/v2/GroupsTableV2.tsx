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
import {
  BookOpen,
  Layers3Icon,
  PlusCircle,
  ShieldCheck,
  Users as UsersIcon,
} from "lucide-react";
import React, { useEffect, useMemo, useRef, useState } from "react";
import { useSWRConfig } from "swr";
import AccessControlIcon from "@/assets/icons/AccessControlIcon";
import DNSIcon from "@/assets/icons/DNSIcon";
import NetworkRoutesIcon from "@/assets/icons/NetworkRoutesIcon";
import PeerIcon from "@/assets/icons/PeerIcon";
import SetupKeysIcon from "@/assets/icons/SetupKeysIcon";
import TeamIcon from "@/assets/icons/TeamIcon";
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
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import CreateGroupModal from "@/modules/groups/CreateGroupModal";
import GroupsCountCell from "@/modules/groups/GroupsCountCell";
import GroupsNameCell from "@/modules/groups/GroupsNameCell";
import useGroupsUsage, { GroupUsage } from "@/modules/groups/useGroupsUsage";
import GroupsActionCellV2 from "@/modules/groups/v2/GroupsActionCellV2";
import TeamTabs from "@/modules/team/v2/TeamTabs";

// GroupsTableV2 — phase-5.9 v2 paint over /api/groups (with usage
// aggregation via useGroupsUsage). Mirrors the standard scaffold
// (header + stats + toolbar + TanStack table + cold-start hero) used
// by the other migrated screens. Cell renderers reused unchanged from
// the legacy module — the visual identity of the count cells survives.

interface Props {
  isLoading?: boolean;
}

type UseFilter = "all" | "used" | "unused";

const noopFilter: FilterFn<unknown> = () => true;
const noopSort: SortingFn<unknown> = () => 0;
const NOOP_FILTER_FNS = {
  fuzzy: noopFilter,
  dateRange: noopFilter,
  exactMatch: noopFilter,
  arrIncludesSomeExact: noopFilter,
};
const NOOP_SORTING_FNS = { checkbox: noopSort };

function isInUse(g: GroupUsage): boolean {
  return (
    (g.peers_count ?? 0) > 0 ||
    (g.nameservers_count ?? 0) > 0 ||
    (g.policies_count ?? 0) > 0 ||
    (g.routes_count ?? 0) > 0 ||
    (g.setup_keys_count ?? 0) > 0 ||
    (g.users_count ?? 0) > 0 ||
    (g.resources_count ?? 0) > 0
  );
}

export default function GroupsTableV2({ isLoading }: Props) {
  const groups = useGroupsUsage();
  const { mutate } = useSWRConfig();
  const { permission } = usePermissions();

  const [createOpen, setCreateOpen] = useState(false);

  useV2TopbarRight(
    <OzButton
      variant="primary"
      type="button"
      onClick={() => setCreateOpen(true)}
      disabled={!permission.groups.create}
    >
      <PlusCircle size={14} />
      New Group
    </OzButton>,
  );

  const [search, setSearch] = useState("");
  const [useFilter, setUseFilter] = useState<UseFilter>("all");
  const [refreshing, setRefreshing] = useState(false);
  const [sorting, setSorting] = useState<SortingState>([
    { id: "name", desc: true },
  ]);
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  });

  const all = useMemo<GroupUsage[]>(() => groups ?? [], [groups]);

  const counts = useMemo(() => {
    let used = 0;
    let unused = 0;
    for (const g of all) {
      if (isInUse(g)) used += 1;
      else unused += 1;
    }
    return { total: all.length, used, unused };
  }, [all]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return all.filter((g) => {
      if (useFilter === "used" && !isInUse(g)) return false;
      if (useFilter === "unused" && isInUse(g)) return false;
      if (!q) return true;
      return (g.name ?? "").toLowerCase().includes(q);
    });
  }, [all, search, useFilter]);

  useEffect(() => {
    setPagination((prev) => ({ ...prev, pageIndex: 0 }));
  }, [search, useFilter]);

  const columns = useMemo<ColumnDef<GroupUsage>[]>(
    () => [
      {
        id: "name",
        accessorFn: (g) => g.name ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Name" />,
        cell: ({ row }) => (
          <GroupsNameCell
            active={isInUse(row.original)}
            group={{
              id: row.original?.id,
              issued: row.original?.issued,
              name: row.original?.name,
            }}
          />
        ),
      },
      countColumn({
        id: "setup_keys_count",
        label: "Setup Keys",
        headerIcon: <SetupKeysIcon size={12} />,
        cellIcon: <SetupKeysIcon size={10} />,
        cellText: "Setup Key(s)",
      }),
      countColumn({
        id: "peers_count",
        label: "Peers",
        headerIcon: <PeerIcon size={12} />,
        cellIcon: <PeerIcon size={10} />,
        cellText: "Peer(s)",
      }),
      countColumn({
        id: "nameservers_count",
        label: "DNS",
        headerIcon: <DNSIcon size={12} />,
        cellIcon: <DNSIcon size={10} />,
        cellText: "DNS",
      }),
      countColumn({
        id: "policies_count",
        label: "Access Controls",
        headerIcon: <AccessControlIcon size={12} />,
        cellIcon: <AccessControlIcon size={10} />,
        cellText: "Access Control(s)",
      }),
      countColumn({
        id: "routes_count",
        label: "Network Routes",
        headerIcon: <NetworkRoutesIcon size={12} />,
        cellIcon: <NetworkRoutesIcon size={10} />,
        cellText: "Network Route(s)",
      }),
      countColumn({
        id: "resources_count",
        label: "Network Resources",
        headerIcon: <Layers3Icon size={12} />,
        cellIcon: <Layers3Icon size={10} />,
        cellText: "Network Resource(s)",
      }),
      countColumn({
        id: "users_count",
        label: "Users",
        headerIcon: <TeamIcon size={12} />,
        cellIcon: <TeamIcon size={10} />,
        cellText: "User(s)",
      }),
      {
        id: "actions",
        // Wider than other v2 tables' action columns because two row
        // buttons sit side-by-side (Edit + Delete). Same sizing as
        // /team/users for visual rhythm across the Identity tabs.
        size: 200,
        enableSorting: false,
        header: () => null,
        cell: ({ row }) => (
          <GroupsActionCellV2
            group={row.original}
            in_use={isInUse(row.original)}
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
    mutate("/groups").finally(() => setRefreshing(false));
  };

  const pageInfo = table.getState().pagination;
  const total = filtered.length;
  const pageStart = total === 0 ? 0 : pageInfo.pageIndex * pageInfo.pageSize + 1;
  const pageEnd = Math.min(total, (pageInfo.pageIndex + 1) * pageInfo.pageSize);

  // Cold-start: no groups created yet. Page header + description stay
  // visible; stats + toolbar + table block are swapped for an
  // OzEmptyState hero. The legacy GroupsTable used a NoResults block
  // (no GetStartedTest), so the v2 hero introduces a small set of
  // helper-card pointers without rewriting the legacy strings.
  const isColdStart = !isLoading && all.length === 0;

  return (
    <TooltipProvider delayDuration={250} skipDelayDuration={100}>
      <div className="space-y-6 p-8">
        <header>
          <h1 className="text-[24px] font-semibold tracking-tight">
            Users &amp; Groups
          </h1>
          <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
            Identity drives policy. Groups are how peers and users get scoped.
            Learn more about{" "}
            <a
              href="https://docs.openzro.io/how-to/manage-network-access"
              target="_blank"
              rel="noopener noreferrer"
              className="text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Groups
            </a>
            .
          </p>
        </header>

        <TeamTabs />

        {/* Create modal — controlled, opened from the topbar CTA or
            the cold-start hero CTA. */}
        <CreateGroupModal open={createOpen} onOpenChange={setCreateOpen} />

        {isColdStart ? (
          <GroupsEmptyState
            canCreate={permission.groups.create}
            onCreate={() => setCreateOpen(true)}
          />
        ) : (
          <OzCard flush>
            {/* Internal card header — handoff (screens-2.jsx, TeamScreen):
                title + count on the left, compact search + segmented
                Used/Unused filter + refresh icon on the right. The
                external stats row is dropped since the count rides
                inline with the title and the segmented filter exposes
                the used/unused split. */}
            <div className="flex flex-wrap items-center gap-3 border-b border-oz2-border-soft px-[18px] py-3.5">
              <div className="mr-auto inline-flex items-baseline gap-2">
                <span className="text-[14px] font-semibold text-oz2-text">
                  Groups
                </span>
                <span className="font-mono text-[12px] font-medium text-oz2-text-faint">
                  {counts.total}
                </span>
              </div>

              <div className="inline-flex h-[28px] w-[200px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-2.5">
                <span className="text-oz2-text-faint">{ICONS.search}</span>
                <input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Search groups…"
                  className="h-full flex-1 border-0 bg-transparent text-[12.5px] outline-none placeholder:text-oz2-text-faint"
                />
              </div>

              <SegmentedTabs
                value={useFilter}
                onChange={setUseFilter}
                options={[
                  { id: "all", label: "All", count: counts.total },
                  { id: "used", label: "Used", count: counts.used },
                  { id: "unused", label: "Unused", count: counts.unused },
                ]}
              />

              <button
                type="button"
                onClick={refreshClick}
                aria-label="Refresh groups"
                className="grid h-[28px] w-[28px] place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 hover:border-oz2-border-strong hover:bg-oz2-hover"
              >
                <span className={refreshing ? "animate-spin text-oz2-acc" : ""}>
                  {ICONS.refresh}
                </span>
              </button>
            </div>

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
                          ? "Loading groups…"
                          : "No groups match your filter."}
                      </OzTableCell>
                    </OzTableRow>
                  )}
                </OzTableBody>
              </OzTable>

            <div className="flex flex-wrap items-center justify-between gap-3 border-t border-oz2-border-soft bg-oz2-bg-sunken px-[18px] py-3 text-[13.5px]">
              <span className="text-oz2-text-muted">
                {total === 0
                  ? "0 groups"
                  : `Showing ${pageStart}–${pageEnd} of ${total}`}
              </span>
              <div className="flex items-center gap-3">
                <PageSizeCombobox
                  value={pageInfo.pageSize}
                  onChange={(n) => table.setPageSize(n)}
                />
                <Pager
                  page={pageInfo.pageIndex + 1}
                  totalPages={Math.max(1, table.getPageCount())}
                  onChange={(p) => table.setPageIndex(p - 1)}
                />
              </div>
            </div>
          </OzCard>
        )}
      </div>
    </TooltipProvider>
  );
}

// Tooltip-headered count column factory. Each usage column shares the
// same shape — icon-only header with a plain-text tooltip, and the
// cell delegates to the legacy GroupsCountCell for the badge paint.
function countColumn({
  id,
  label,
  headerIcon,
  cellIcon,
  cellText,
}: {
  id: keyof GroupUsage & string;
  label: string;
  headerIcon: React.ReactNode;
  cellIcon: React.ReactNode;
  cellText: string;
}): ColumnDef<GroupUsage> {
  return {
    id,
    accessorFn: (g) => (g[id] as number | undefined) ?? 0,
    sortingFn: "basic",
    size: 80,
    header: ({ column }) => (
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              column.toggleSorting();
            }}
            aria-label={`Sort by ${label}`}
            className="grid h-5 w-5 place-items-center rounded text-oz2-text-muted transition-colors hover:text-oz2-text"
          >
            {headerIcon}
          </button>
        </TooltipTrigger>
        <TooltipContent>{label}</TooltipContent>
      </Tooltip>
    ),
    cell: ({ row }) => (
      <GroupsCountCell
        icon={cellIcon}
        groupName={row.original.name}
        text={cellText}
        count={(row.original[id] as number | undefined) ?? 0}
      />
    ),
  };
}

function GroupsEmptyState({
  canCreate,
  onCreate,
}: {
  canCreate: boolean;
  onCreate: () => void;
}) {
  return (
    <OzEmptyState
      title="No groups"
      description="You don't have any groups created yet. Groups bundle peers, resources and users so policies, routes and setup keys can target them by name."
      primaryAction={
        <OzButton
          variant="primary"
          type="button"
          onClick={onCreate}
          disabled={!canCreate}
        >
          <PlusCircle size={14} />
          New Group
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
            Groups
          </a>
          .
        </>
      }
      helperCards={[
        {
          icon: <BookOpen size={16} />,
          title: "How groups work",
          description:
            "Tag peers, users and resources so policies and routes target them by name.",
          href: "https://docs.openzro.io/how-to/manage-network-access",
        },
        {
          icon: <UsersIcon size={16} />,
          title: "Sync from your IdP",
          description:
            "JWT and SCIM-synced groups stay in lockstep with your identity provider.",
          href: "https://docs.openzro.io/how-to/manage-users",
        },
        {
          icon: <ShieldCheck size={16} />,
          title: "Wire to a policy",
          description:
            "Drop a group into Sources or Destinations of an Access Control policy.",
          href: "https://docs.openzro.io/how-to/manage-network-access",
        },
      ]}
    />
  );
}

// ─── Sortable header (text) ─────────────────────────────────────────────

function SortHeader({
  column,
  label,
}: {
  column: Column<GroupUsage, unknown>;
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
