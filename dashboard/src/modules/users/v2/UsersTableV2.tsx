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
import { isLocalDev, isOpenzroHosted } from "@utils/openzro";
import dayjs from "dayjs";
import {
  BookOpen,
  MailPlus,
  ShieldCheck,
  UserCheck,
  Users as UsersIcon,
} from "lucide-react";
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
import { User } from "@/interfaces/User";
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import LastTimeRow from "@/modules/common-table-rows/LastTimeRow";
import TeamTabs from "@/modules/team/v2/TeamTabs";
import UserActionCell from "@/modules/users/table-cells/UserActionCell";
import UserBlockCell from "@/modules/users/table-cells/UserBlockCell";
import UserGroupCell from "@/modules/users/table-cells/UserGroupCell";
import UserNameCell from "@/modules/users/table-cells/UserNameCell";
import UserRoleCell from "@/modules/users/table-cells/UserRoleCell";
import UserStatusCell from "@/modules/users/table-cells/UserStatusCell";
import { UserInviteModalContent } from "@/modules/users/UserInviteModal";

// UsersTableV2 — phase-5.8 v2 paint over real /api/users data.
// Mirrors AccessControlTableV2 chrome (header + stats + toolbar +
// TanStack table + cold-start hero) with the User columns. Cells
// reused unchanged from the legacy module.
//
// Invitations are gated behind the same isLocalDev || isOpenzroHosted
// flag the legacy table used — self-hosted prod doesn't expose the
// invite endpoint, so the topbar CTA and the cold-start CTA both
// hide outside those environments.

interface Props {
  users: User[] | undefined;
  isLoading: boolean;
}

type RoleFilter = "all" | "owner" | "admin" | "user";

const noopFilter: FilterFn<unknown> = () => true;
const noopSort: SortingFn<unknown> = () => 0;
const NOOP_FILTER_FNS = {
  fuzzy: noopFilter,
  dateRange: noopFilter,
  exactMatch: noopFilter,
  arrIncludesSomeExact: noopFilter,
};
const NOOP_SORTING_FNS = { checkbox: noopSort };

export default function UsersTableV2({ users, isLoading }: Props) {
  const { mutate } = useSWRConfig();
  const { permission } = usePermissions();
  const router = useRouter();

  const inviteAvailable = isLocalDev() || isOpenzroHosted();
  const [inviteOpen, setInviteOpen] = useState(false);

  useV2TopbarRight(
    inviteAvailable ? (
      <OzButton
        variant="primary"
        type="button"
        onClick={() => setInviteOpen(true)}
        disabled={!permission.users.create}
      >
        <MailPlus size={14} />
        Invite User
      </OzButton>
    ) : null,
  );

  const [search, setSearch] = useState("");
  const [roleFilter, setRoleFilter] = useState<RoleFilter>("all");
  const [refreshing, setRefreshing] = useState(false);
  const [sorting, setSorting] = useState<SortingState>([
    { id: "is_current", desc: true },
    { id: "name", desc: true },
  ]);
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  });

  const all = useMemo(() => users ?? [], [users]);

  const counts = useMemo(() => {
    let owner = 0;
    let admin = 0;
    let user = 0;
    for (const u of all) {
      const r = (u.role ?? "").toLowerCase();
      if (r === "owner") owner += 1;
      else if (r === "admin") admin += 1;
      else user += 1;
    }
    return { total: all.length, owner, admin, user };
  }, [all]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return all.filter((u) => {
      if (roleFilter !== "all") {
        const r = (u.role ?? "").toLowerCase();
        if (r !== roleFilter) return false;
      }
      if (!q) return true;
      const haystack = [u.name, u.email, u.role].filter(Boolean).join(" ").toLowerCase();
      return haystack.includes(q);
    });
  }, [all, search, roleFilter]);

  useEffect(() => {
    setPagination((prev) => ({ ...prev, pageIndex: 0 }));
  }, [search, roleFilter]);

  const columns = useMemo<ColumnDef<User>[]>(
    () => [
      {
        id: "name",
        accessorFn: (u) => `${u.name ?? ""} ${u.email ?? ""}`,
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Name" />,
        cell: ({ row }) => <UserNameCell user={row.original} />,
      },
      {
        id: "is_current",
        accessorFn: (u) => Boolean(u.is_current),
        sortingFn: "basic",
        enableHiding: true,
        // Hidden — present only so default sorting can pin the
        // logged-in user to the top, mirroring the legacy table.
      },
      {
        id: "role",
        accessorFn: (u) => u.role ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Role" />,
        cell: ({ row }) => <UserRoleCell user={row.original} />,
      },
      {
        id: "status",
        accessorFn: (u) => u.status ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Status" />,
        cell: ({ row }) => <UserStatusCell user={row.original} />,
      },
      {
        id: "auto_groups",
        accessorFn: (u) => u.auto_groups?.length ?? 0,
        sortingFn: "basic",
        header: ({ column }) => <SortHeader column={column} label="Groups" />,
        cell: ({ row }) => <UserGroupCell user={row.original} />,
      },
      {
        id: "is_blocked",
        accessorFn: (u) => Boolean(u.is_blocked),
        sortingFn: "basic",
        header: ({ column }) => (
          <SortHeader column={column} label="Block User" />
        ),
        cell: ({ row }) => <UserBlockCell user={row.original} />,
      },
      {
        id: "last_login",
        accessorFn: (u) => u.last_login ?? "",
        sortingFn: "datetime",
        header: ({ column }) => (
          <SortHeader column={column} label="Last Login" />
        ),
        cell: ({ row }) => (
          <LastTimeRow
            date={dayjs(row.original.last_login).toDate()}
            text="Last login on"
          />
        ),
      },
      {
        id: "actions",
        size: 40,
        enableSorting: false,
        header: () => null,
        cell: ({ row }) => <UserActionCell user={row.original} />,
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
      columnVisibility: { is_current: false },
    },
    onSortingChange: setSorting,
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getRowId: (u) => u.id ?? "",
    filterFns: NOOP_FILTER_FNS,
    sortingFns: NOOP_SORTING_FNS,
  });

  const refreshClick = () => {
    setRefreshing(true);
    Promise.all([
      mutate("/users?service_user=false"),
      mutate("/groups"),
    ]).finally(() => setRefreshing(false));
  };

  // Row-click navigates to the user-detail screen, mirroring the
  // legacy onRowClick. Skip when the click landed on an interactive
  // child so toggles / kebab actions stay clickable without doubling
  // up as a navigation.
  const handleRowClick = (row: Row<User>, event: React.MouseEvent) => {
    const target = event.target as HTMLElement;
    if (
      target.closest(
        "button, a, [role='switch'], input, select, [data-stop-row-click]",
      )
    ) {
      return;
    }
    router.push(`/team/user?id=${row.original.id}`);
  };

  const pageInfo = table.getState().pagination;
  const total = filtered.length;
  const pageStart = total === 0 ? 0 : pageInfo.pageIndex * pageInfo.pageSize + 1;
  const pageEnd = Math.min(total, (pageInfo.pageIndex + 1) * pageInfo.pageSize);

  // Cold-start: no users registered yet. Page header + description
  // stay visible; stats + toolbar + table block are swapped for an
  // OzEmptyState hero.
  const isColdStart = !isLoading && all.length === 0;

  return (
    <TooltipProvider delayDuration={250} skipDelayDuration={100}>
      <div className="space-y-6 p-8">
        <header>
          <h1 className="text-[24px] font-semibold tracking-tight">
            Users &amp; Groups
          </h1>
          <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
            Manage users and their permissions. Same-domain email users are
            added automatically on first sign-in. Learn more about{" "}
            <a
              href="https://docs.openzro.io/how-to/add-users-to-your-network"
              target="_blank"
              rel="noopener noreferrer"
              className="text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Users
            </a>
            .
          </p>
        </header>

        <TeamTabs />

        {/* Invite modal — controlled, opened from the topbar CTA or
            the cold-start hero CTA. Rendered inline so the modal
            content resolves the page-level providers. */}
        {inviteAvailable && (
          <Modal
            open={inviteOpen}
            onOpenChange={setInviteOpen}
            key={inviteOpen ? "invite-open" : "invite-closed"}
          >
            {inviteOpen && (
              <UserInviteModalContent
                onSuccess={() => setInviteOpen(false)}
              />
            )}
          </Modal>
        )}

        {isColdStart ? (
          <UsersEmptyState
            inviteAvailable={inviteAvailable}
            canCreate={permission.users.create}
            onInvite={() => setInviteOpen(true)}
          />
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-[13.5px] text-oz2-text-muted">
              <span className="inline-flex items-center gap-2">
                <span className="font-medium text-oz2-text">{counts.total}</span>
                Total
              </span>
              <span className="inline-flex items-center gap-2 border-l border-oz2-border-soft pl-5">
                <span className="font-medium text-oz2-text">{counts.owner}</span>
                Owners
              </span>
              <span className="inline-flex items-center gap-2 border-l border-oz2-border-soft pl-5">
                <span className="font-medium text-oz2-text">{counts.admin}</span>
                Admins
              </span>
              <span className="inline-flex items-center gap-2 border-l border-oz2-border-soft pl-5">
                <span className="font-medium text-oz2-text">{counts.user}</span>
                Users
              </span>
            </div>

            <div className="flex flex-wrap items-center gap-3">
              <div className="inline-flex h-[34px] flex-1 min-w-[220px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3">
                <span className="text-oz2-text-faint">{ICONS.search}</span>
                <input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Search by name, email or role…"
                  className="h-full flex-1 border-0 bg-transparent text-[14px] outline-none placeholder:text-oz2-text-faint"
                />
              </div>

              <SegmentedTabs
                value={roleFilter}
                onChange={setRoleFilter}
                options={[
                  { id: "all", label: "All", count: counts.total },
                  { id: "owner", label: "Owners", count: counts.owner },
                  { id: "admin", label: "Admins", count: counts.admin },
                  { id: "user", label: "Users", count: counts.user },
                ]}
              />

              <PageSizeCombobox
                value={pageInfo.pageSize}
                onChange={(n) => table.setPageSize(n)}
              />

              <button
                type="button"
                onClick={refreshClick}
                aria-label="Refresh users"
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
                          ? "Loading users…"
                          : "No users match your filter."}
                      </OzTableCell>
                    </OzTableRow>
                  )}
                </OzTableBody>
              </OzTable>

              <div className="flex flex-wrap items-center justify-between gap-3 border-t border-oz2-border-soft bg-oz2-bg-sunken px-[18px] py-3 text-[13.5px]">
                <span className="text-oz2-text-muted">
                  {total === 0
                    ? "0 users"
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

function UsersEmptyState({
  inviteAvailable,
  canCreate,
  onInvite,
}: {
  inviteAvailable: boolean;
  canCreate: boolean;
  onInvite: () => void;
}) {
  return (
    <OzEmptyState
      title="Add New Users"
      description="It looks like you don't have any users yet. Get started by inviting users to your account."
      primaryAction={
        inviteAvailable ? (
          <OzButton
            variant="primary"
            type="button"
            onClick={onInvite}
            disabled={!canCreate}
          >
            <MailPlus size={14} />
            Invite User
          </OzButton>
        ) : null
      }
      learnMore={
        <>
          Learn more about{" "}
          <a
            href="https://docs.openzro.io/how-to/add-users-to-your-network"
            target="_blank"
            rel="noopener noreferrer"
            className="font-medium text-oz2-acc-text underline-offset-2 hover:underline"
          >
            Users
          </a>
          .
        </>
      }
      helperCards={[
        {
          icon: <BookOpen size={16} />,
          title: "Add users to the network",
          description:
            "Walk through invitations, JIT provisioning and IdP-synced accounts.",
          href: "https://docs.openzro.io/how-to/add-users-to-your-network",
        },
        {
          icon: <UserCheck size={16} />,
          title: "Roles & permissions",
          description:
            "Owner, admin and user — what each role can do across the workspace.",
          href: "https://docs.openzro.io/how-to/manage-users",
        },
        {
          icon: <ShieldCheck size={16} />,
          title: "Group membership",
          description:
            "Auto-assign groups so a user inherits the right policies from day one.",
          href: "https://docs.openzro.io/how-to/manage-groups",
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
  column: Column<User, unknown>;
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
