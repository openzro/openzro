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
  RowSelectionState,
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
import { usePermissions } from "@/contexts/PermissionsProvider";
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import { GroupIssued } from "@/interfaces/Group";
import CreateGroupModal from "@/modules/groups/CreateGroupModal";
import GroupsNameCell from "@/modules/groups/GroupsNameCell";
import useGroupsUsage, { GroupUsage } from "@/modules/groups/useGroupsUsage";
import GroupsActionCellV2 from "@/modules/groups/v2/GroupsActionCellV2";
import GroupsBulkActionsV2 from "@/modules/groups/v2/GroupsBulkActionsV2";
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

// Origin filter values. The 6 concrete cases match
// useGroupIdentification's logic (granular Okta/Google/Azure inferred
// from the row's id), plus a generic "integration" bucket for any
// IdP-issued group that doesn't carry one of those substrings.
type Origin = "api" | "jwt" | "okta" | "google" | "azure" | "integration";
type OriginFilter = "all" | Origin;

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

// getGroupOrigin derives a granular origin label for the filter. The
// backend's `issued` field only distinguishes api / jwt / integration;
// per-provider granularity (okta / google / azure) is reverse-engineered
// from substrings in the row id — same heuristic useGroupIdentification
// uses to gate Edit/Delete. Anything labelled `integration` but not
// matching one of those substrings falls into the generic "integration"
// bucket.
function getGroupOrigin(g: GroupUsage): Origin {
  if (g.issued === GroupIssued.JWT) return "jwt";
  if (g.issued === GroupIssued.INTEGRATION) {
    const id = g.id ?? "";
    if (id.includes("okta")) return "okta";
    if (id.includes("google")) return "google";
    if (id.includes("azure")) return "azure";
    return "integration";
  }
  return "api";
}

// isDeletable mirrors GroupsActionCellV2's gate. Used to decide which
// rows the bulk checkbox can select — the "All" group, IdP-managed
// groups, and groups currently in use are not selectable so the bulk
// confirm never offers to delete something the backend would refuse.
function isDeletable(g: GroupUsage): boolean {
  if (g.name === "All") return false;
  if (isInUse(g)) return false;
  const origin = getGroupOrigin(g);
  return origin === "api";
}

// Display labels for origin values. Keep these in sync with the badge
// renderer (GroupBadgeIcon) so the filter dropdown reads naturally.
const ORIGIN_LABEL: Record<Origin, string> = {
  api: "Created via API",
  jwt: "JWT",
  okta: "Okta",
  google: "Google Workspace",
  azure: "Microsoft Entra",
  integration: "Integration (other)",
};

// Toolbar Sort-by combobox values. Two alphabetical orders + a single
// "most used" aggregate, in keeping with the 95% UX assumption: the
// operator either knows the group name (alphabetical) or is hunting
// for what's most referenced (most-used). Per-dimension sorts (peers,
// policies, etc.) were dropped along with the per-dimension columns.
type SortBy = "name-asc" | "name-desc" | "most-used";

const SORT_BY_LABEL: Record<SortBy, string> = {
  "name-asc": "Name (A → Z)",
  "name-desc": "Name (Z → A)",
  "most-used": "Most referenced",
};

// usageTotal aggregates every per-type count into a single ranking
// signal — used as the sort key for the "Most referenced" mode and as
// the data signal for the pill stack.
function usageTotal(g: GroupUsage): number {
  return (
    (g.peers_count ?? 0) +
    (g.policies_count ?? 0) +
    (g.routes_count ?? 0) +
    (g.setup_keys_count ?? 0) +
    (g.nameservers_count ?? 0) +
    (g.resources_count ?? 0) +
    (g.users_count ?? 0)
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
  const [originFilter, setOriginFilter] = useState<OriginFilter>("all");
  const [sortBy, setSortBy] = useState<SortBy>("name-asc");
  const [refreshing, setRefreshing] = useState(false);
  const [sorting, setSorting] = useState<SortingState>([]);
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  });
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({});

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
    const matches = all.filter((g) => {
      if (useFilter === "used" && !isInUse(g)) return false;
      if (useFilter === "unused" && isInUse(g)) return false;
      if (originFilter !== "all" && getGroupOrigin(g) !== originFilter) {
        return false;
      }
      if (!q) return true;
      return (g.name ?? "").toLowerCase().includes(q);
    });
    // Pre-sort here instead of through TanStack so the Sort-by
    // combobox can drive both alphabetical and usage-volume sorts
    // through a single state path. Stable secondary key on name keeps
    // groups with identical usage counts in a predictable order.
    return matches.slice().sort((a, b) => {
      const nameA = (a.name ?? "").toLowerCase();
      const nameB = (b.name ?? "").toLowerCase();
      if (sortBy === "name-asc") return nameA.localeCompare(nameB);
      if (sortBy === "name-desc") return nameB.localeCompare(nameA);
      // most-used: total references desc, tie-break by name asc
      const totalA = usageTotal(a);
      const totalB = usageTotal(b);
      if (totalA !== totalB) return totalB - totalA;
      return nameA.localeCompare(nameB);
    });
  }, [all, search, useFilter, originFilter, sortBy]);

  // Available origins in the current dataset — feeds the filter
  // dropdown so we only surface origins that are actually present.
  // Avoids showing "Okta" on accounts that only have JWT/API groups,
  // etc. Always-present "All" sentinel is prepended by the renderer.
  const availableOrigins = useMemo<Origin[]>(() => {
    const seen = new Set<Origin>();
    for (const g of all) seen.add(getGroupOrigin(g));
    // Preserve a stable order regardless of insertion sequence.
    return (["api", "jwt", "okta", "google", "azure", "integration"] as Origin[])
      .filter((o) => seen.has(o));
  }, [all]);

  useEffect(() => {
    setPagination((prev) => ({ ...prev, pageIndex: 0 }));
  }, [search, useFilter, originFilter]);

  // Clear any selected ids that the filters no longer expose — the
  // bulk bar would otherwise lie about a count the user can't see.
  useEffect(() => {
    if (Object.keys(rowSelection).length === 0) return;
    const visible = new Set(filtered.map((g) => g.id ?? ""));
    let dirty = false;
    const next: RowSelectionState = {};
    for (const [id, selected] of Object.entries(rowSelection)) {
      if (visible.has(id)) {
        next[id] = selected;
      } else {
        dirty = true;
      }
    }
    if (dirty) setRowSelection(next);
    // rowSelection intentionally omitted — we want this to fire only
    // when the filtered set shrinks; selection-driven changes don't
    // need to recompute themselves.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filtered]);

  const columns = useMemo<ColumnDef<GroupUsage>[]>(
    () => [
      {
        id: "select",
        size: 44,
        enableSorting: false,
        header: ({ table }) => {
          // Page-level select-all only flips rows that are *selectable*
          // (i.e. deletable). The TanStack helper respects each row's
          // `getCanSelect` result, so the All / IdP / in-use rows stay
          // untouched even when the header checkbox toggles.
          const selectableRows = table
            .getRowModel()
            .rows.filter((r) => r.getCanSelect());
          const allSelected =
            selectableRows.length > 0 &&
            selectableRows.every((r) => r.getIsSelected());
          const someSelected =
            !allSelected && selectableRows.some((r) => r.getIsSelected());
          return (
            <Checkbox
              checked={allSelected}
              indeterminate={someSelected}
              onChange={(checked) => {
                for (const r of selectableRows) r.toggleSelected(checked);
              }}
              disabled={selectableRows.length === 0}
              aria-label="Select all deletable groups on this page"
            />
          );
        },
        cell: ({ row }) => {
          if (!row.getCanSelect()) {
            // Non-deletable row — keep the slot blank so the checkbox
            // column visually aligns with the rows that do offer one.
            return <span className="block h-4 w-4" aria-hidden />;
          }
          return (
            <Checkbox
              checked={row.getIsSelected()}
              onChange={(checked) => row.toggleSelected(checked)}
              aria-label={`Select ${row.original.name}`}
            />
          );
        },
      },
      {
        id: "name",
        accessorFn: (g) => g.name ?? "",
        enableSorting: false,
        header: () => (
          <span className="font-mono text-[11.5px] font-semibold uppercase tracking-widest text-oz2-text-muted">
            Name
          </span>
        ),
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
      // Single "Used in" column replacing the previous 7 per-type
      // count columns. The old layout (icon-only header + per-cell
      // GroupsCountCell, repeated 7 times) made even moderately
      // populated rows look like a sparse bar chart with most slots
      // empty. The pill stack here only renders the non-zero
      // dimensions, so a group used in just two places shows two
      // pills instead of "2 + 5 dashes". Sorting by individual
      // dimensions moves to the Sort-by combobox in the toolbar.
      {
        id: "usage",
        enableSorting: false,
        header: () => (
          <span className="font-mono text-[11.5px] font-semibold uppercase tracking-widest text-oz2-text-muted">
            Used in
          </span>
        ),
        cell: ({ row }) => <UsagePillStack g={row.original} />,
      },
      {
        id: "actions",
        size: 64,
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
    state: { sorting, pagination, rowSelection },
    onSortingChange: setSorting,
    onPaginationChange: setPagination,
    onRowSelectionChange: setRowSelection,
    enableRowSelection: (row) => isDeletable(row.original),
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
            Identity drives policy. Humans, machines, and the groups that scope
            their access — managed in one place.
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
          <>
            {/* Toolbar — handoff (TeamScreen): row of controls between
                the tabs and the card. Search input (32×280), used/
                unused filter (kept as segmented per the working
                agreement), refresh icon, and the mono count text
                right-aligned. Sits outside the card so the card is
                flush around the table itself. */}
            <div className="flex flex-wrap items-center gap-2.5">
              <div className="inline-flex h-8 w-[280px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-2.5">
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

              {availableOrigins.length > 1 && (
                <OriginCombobox
                  value={originFilter}
                  onChange={setOriginFilter}
                  options={availableOrigins}
                />
              )}

              <SortCombobox value={sortBy} onChange={setSortBy} />

              <PageSizeCombobox
                value={pageInfo.pageSize}
                onChange={(n) => table.setPageSize(n)}
              />

              <button
                type="button"
                onClick={refreshClick}
                aria-label="Refresh groups"
                className="grid h-8 w-8 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 hover:border-oz2-border-strong hover:bg-oz2-hover"
              >
                <span className={refreshing ? "animate-spin text-oz2-acc" : ""}>
                  {ICONS.refresh}
                </span>
              </button>

              <span className="ml-auto font-mono text-[11px] uppercase tracking-[0.04em] text-oz2-text-faint">
                {counts.total} groups
              </span>
            </div>

            <OzCard flush>
              {/* Bulk-action bar — only renders when at least one
                  selectable row is checked. Self-contained: confirms,
                  fans out the N DELETEs, refreshes /groups, and
                  clears the selection via onCanceled. */}
              <GroupsBulkActionsV2
                selectedIds={rowSelection}
                onCanceled={() => table.resetRowSelection()}
              />

              {/* List rendering — replaces the previous OzTable shape.
                  A table with 2 wide cells looked sparse; cards give
                  each group room to carry name + origin caption +
                  pill stack + actions without the columnar gridlines
                  dictating the layout. Selection + bulk action
                  semantics are preserved via the row's tanstack
                  handle (row.toggleSelected / row.getCanSelect). */}
              <div role="list" className="divide-y divide-oz2-border-soft">
                {/* Page-level select-all chip — only shows when at
                    least one row on this page is selectable. */}
                {(() => {
                  const selectableRows = table
                    .getRowModel()
                    .rows.filter((r) => r.getCanSelect());
                  if (selectableRows.length === 0) return null;
                  const allSelected = selectableRows.every((r) =>
                    r.getIsSelected(),
                  );
                  const someSelected =
                    !allSelected &&
                    selectableRows.some((r) => r.getIsSelected());
                  return (
                    <div className="flex items-center gap-3 bg-oz2-bg-sunken px-[18px] py-2 text-[12.5px] text-oz2-text-muted">
                      <Checkbox
                        checked={allSelected}
                        indeterminate={someSelected}
                        onChange={(c) => {
                          for (const r of selectableRows) r.toggleSelected(c);
                        }}
                        aria-label="Select all deletable groups on this page"
                      />
                      <span className="font-mono text-[11.5px] uppercase tracking-[0.04em]">
                        {selectableRows.length} deletable on this page
                      </span>
                    </div>
                  );
                })()}

                {table.getRowModel().rows.length === 0 ? (
                  <div className="px-[18px] py-12 text-center text-oz2-text-muted">
                    {isLoading
                      ? "Loading groups…"
                      : "No groups match your filter."}
                  </div>
                ) : (
                  table.getRowModel().rows.map((row) => (
                    <GroupListItem key={row.id} row={row} />
                  ))
                )}
              </div>

              <div className="flex flex-wrap items-center justify-between gap-3 border-t border-oz2-border-soft bg-oz2-bg-sunken px-[18px] py-3 text-[13.5px]">
                <span className="text-oz2-text-muted">
                  {total === 0
                    ? "0 groups"
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

// ─── List item card ────────────────────────────────────────────────────

// GroupListItem — single-row card for the list-style /team/groups
// layout. Replaces what would otherwise be a table row with a
// purpose-built horizontal stack: checkbox · badge · name+caption ·
// pill stack · actions. Two lines of text in the middle (name + meta)
// give the card visual weight without needing extra columns.
function GroupListItem({
  row,
}: {
  row: ReturnType<ReturnType<typeof useReactTable<GroupUsage>>["getRowModel"]>["rows"][number];
}) {
  const g = row.original;
  const inUse = isInUse(g);
  const origin = getGroupOrigin(g);
  const total = usageTotal(g);

  const canSelect = row.getCanSelect();
  const selected = row.getIsSelected();

  // Caption line: "Issued by X · N references". Mirrors the
  // information density of the legacy two-column layout without
  // needing the dedicated columns.
  const captionParts: string[] = [];
  switch (origin) {
    case "api":
      captionParts.push("Created via API");
      break;
    case "jwt":
      captionParts.push("Issued via JWT");
      break;
    default:
      captionParts.push(`Issued by ${ORIGIN_LABEL[origin]}`);
  }
  if (total > 0) {
    captionParts.push(`${total} reference${total === 1 ? "" : "s"}`);
  } else {
    captionParts.push("unused");
  }

  return (
    <div
      role="listitem"
      className={
        "flex items-center gap-3 px-[18px] py-3 transition-colors " +
        (selected ? "bg-oz2-acc-soft/40" : "hover:bg-oz2-hover")
      }
    >
      {canSelect ? (
        <Checkbox
          checked={selected}
          onChange={(c) => row.toggleSelected(c)}
          aria-label={`Select ${g.name}`}
        />
      ) : (
        <span className="block h-4 w-4 shrink-0" aria-hidden />
      )}

      {/* Avatar / origin badge — provided by GroupsNameCell upstream
          but exposed here at a fixed slot so the layout stays tight
          even when the group name overflows. */}
      <div className="shrink-0">
        <GroupsNameCell
          active={inUse}
          group={{ id: g.id, issued: g.issued, name: g.name }}
        />
      </div>

      <div className="min-w-0 flex-1">
        <div className="truncate font-mono text-[11.5px] uppercase tracking-[0.04em] text-oz2-text-faint">
          {captionParts.join(" · ")}
        </div>
      </div>

      <UsagePillStack g={g} />

      <GroupsActionCellV2 group={g} in_use={inUse} />
    </div>
  );
}

// ─── Usage pill stack ──────────────────────────────────────────────────

// Per-type display + colour map for the pill stack. Each entry maps
// a GroupUsage numeric field to (icon, short label, pill colour). Only
// non-zero counts render — a group used in one place shows one pill,
// not a 7-slot grid of "1 + 6 dashes" like the previous column layout
// produced.
const USAGE_DIMENSIONS: ReadonlyArray<{
  field: keyof GroupUsage & string;
  label: string;
  icon: React.ReactNode;
  className: string;
}> = [
  {
    field: "peers_count",
    label: "peers",
    icon: <PeerIcon size={11} />,
    className:
      "bg-sky-100 text-sky-700 dark:bg-sky-950/50 dark:text-sky-200",
  },
  {
    field: "users_count",
    label: "users",
    icon: <TeamIcon size={11} />,
    className:
      "bg-indigo-100 text-indigo-700 dark:bg-indigo-950/50 dark:text-indigo-200",
  },
  {
    field: "policies_count",
    label: "policies",
    icon: <AccessControlIcon size={11} />,
    className:
      "bg-emerald-100 text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-200",
  },
  {
    field: "routes_count",
    label: "routes",
    icon: <NetworkRoutesIcon size={11} />,
    className:
      "bg-amber-100 text-amber-700 dark:bg-amber-950/50 dark:text-amber-200",
  },
  {
    field: "resources_count",
    label: "resources",
    icon: <Layers3Icon size={11} />,
    className:
      "bg-violet-100 text-violet-700 dark:bg-violet-950/50 dark:text-violet-200",
  },
  {
    field: "setup_keys_count",
    label: "setup keys",
    icon: <SetupKeysIcon size={11} />,
    className: "bg-oz2-bg-sunken text-oz2-text-2",
  },
  {
    field: "nameservers_count",
    label: "dns",
    icon: <DNSIcon size={11} />,
    className: "bg-oz2-bg-sunken text-oz2-text-2",
  },
];

function UsagePillStack({ g }: { g: GroupUsage }) {
  const present = USAGE_DIMENSIONS.filter(
    (d) => ((g[d.field] as number | undefined) ?? 0) > 0,
  );
  if (present.length === 0) {
    return (
      <span className="font-mono text-[11.5px] uppercase tracking-[0.06em] text-oz2-text-faint">
        unused
      </span>
    );
  }
  return (
    <div className="flex flex-wrap items-center gap-1">
      {present.map((d) => {
        const count = (g[d.field] as number) ?? 0;
        return (
          <Tooltip key={d.field}>
            <TooltipTrigger asChild>
              <span
                className={
                  "inline-flex h-6 items-center gap-1 rounded-full px-2 text-[11.5px] font-medium " +
                  d.className
                }
              >
                {d.icon}
                <span className="font-mono tabular-nums">{count}</span>
              </span>
            </TooltipTrigger>
            <TooltipContent>
              {count} {d.label}
            </TooltipContent>
          </Tooltip>
        );
      })}
    </div>
  );
}

// ─── Sort-by combobox ──────────────────────────────────────────────────

function SortCombobox({
  value,
  onChange,
}: {
  value: SortBy;
  onChange: (next: SortBy) => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  const choices: SortBy[] = ["name-asc", "name-desc", "most-used"];
  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="inline-flex h-[34px] items-center gap-1.5 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 text-[14px] font-medium text-oz2-text-2 hover:bg-oz2-hover hover:border-oz2-border-strong"
      >
        <span className="text-oz2-text-faint">Sort:</span>
        <span>{SORT_BY_LABEL[value]}</span>
        <span className="text-oz2-text-faint">{ICONS.chevDown}</span>
      </button>
      {open && (
        <div className="absolute right-0 top-full z-30 mt-1 min-w-[180px] overflow-hidden rounded-oz2-input border border-oz2-border bg-oz2-bg-elev shadow-oz2-md">
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
                  {SORT_BY_LABEL[c]}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

// ─── Origin filter combobox + page-size + Pager ───────────────────────

function OriginCombobox({
  value,
  onChange,
  options,
}: {
  value: OriginFilter;
  onChange: (next: OriginFilter) => void;
  options: Origin[];
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  const label = value === "all" ? "All origins" : ORIGIN_LABEL[value];

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="inline-flex h-[34px] items-center gap-1.5 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 text-[14px] font-medium text-oz2-text-2 hover:bg-oz2-hover hover:border-oz2-border-strong"
      >
        <span className="text-oz2-text-faint">Origin:</span>
        <span>{label}</span>
        <span className="text-oz2-text-faint">{ICONS.chevDown}</span>
      </button>
      {open && (
        <div className="absolute right-0 top-full z-30 mt-1 min-w-[200px] overflow-hidden rounded-oz2-input border border-oz2-border bg-oz2-bg-elev shadow-oz2-md">
          <ul className="py-1">
            <li>
              <button
                type="button"
                onClick={() => {
                  onChange("all");
                  setOpen(false);
                }}
                className={
                  "flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left text-[13.5px] hover:bg-oz2-hover " +
                  (value === "all" ? "text-oz2-acc-text" : "text-oz2-text")
                }
              >
                All origins
              </button>
            </li>
            {options.map((o) => (
              <li key={o}>
                <button
                  type="button"
                  onClick={() => {
                    onChange(o);
                    setOpen(false);
                  }}
                  className={
                    "flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left text-[13.5px] hover:bg-oz2-hover " +
                    (value === o ? "text-oz2-acc-text" : "text-oz2-text")
                  }
                >
                  {ORIGIN_LABEL[o]}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

// ─── Checkbox ──────────────────────────────────────────────────────────

function Checkbox({
  checked,
  indeterminate,
  onChange,
  disabled,
  ...props
}: {
  checked: boolean;
  indeterminate?: boolean;
  onChange: (next: boolean) => void;
  disabled?: boolean;
} & Omit<
  React.InputHTMLAttributes<HTMLInputElement>,
  "checked" | "onChange" | "type"
>) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = !!indeterminate && !checked;
  }, [indeterminate, checked]);

  const showFill = checked || indeterminate;

  return (
    <label
      className={
        "inline-flex h-4 w-4 shrink-0 items-center justify-center " +
        (disabled ? "cursor-not-allowed opacity-40" : "cursor-pointer")
      }
    >
      <input
        ref={ref}
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
        className="peer sr-only"
        {...props}
      />
      <span
        aria-hidden="true"
        className={
          "grid h-4 w-4 place-items-center rounded border transition-colors " +
          (showFill
            ? "border-transparent bg-oz2-acc text-oz2-text-on-acc"
            : "border-oz2-border bg-oz2-surface peer-hover:border-oz2-border-strong")
        }
      >
        {checked && !indeterminate && (
          <svg
            width={10}
            height={10}
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth={3}
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="m5 12 5 5L20 7" />
          </svg>
        )}
        {indeterminate && (
          <svg
            width={10}
            height={2}
            viewBox="0 0 10 2"
            fill="currentColor"
            aria-hidden="true"
          >
            <rect width="10" height="2" rx="1" />
          </svg>
        )}
      </span>
    </label>
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
