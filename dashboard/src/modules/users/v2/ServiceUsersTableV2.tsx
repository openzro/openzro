"use client";

import { Modal } from "@components/modal/Modal";
import { notify } from "@components/Notification";
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
import { useApiCall } from "@utils/api";
import {
  BookOpen,
  Bot,
  Info,
  KeyRound,
  PencilLine,
  PlusCircle,
  ShieldCheck,
  Trash2,
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
import { useDialog } from "@/contexts/DialogProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { User } from "@/interfaces/User";
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import TeamTabs from "@/modules/team/v2/TeamTabs";
import { ServiceUserModalContent } from "@/modules/users/ServiceUserModal";
import ServiceUserNameCellV2 from "@/modules/users/v2/ServiceUserNameCellV2";
import UserRoleCellV2 from "@/modules/users/v2/UserRoleCellV2";

// ServiceUsersTableV2 — v2 paint over /users?service_user=true. Sister
// table to UsersTableV2 sharing the same chrome (header + TeamTabs +
// toolbar + TanStack table + OzEmptyState cold-start). Service users
// don't get a role filter (the role surface is much smaller) and don't
// surface auto_groups / is_blocked / last_login — those columns are
// human-user concerns. The row-click navigates to /team/user with the
// service_user flag so the detail page knows which list to mutate on
// edit.

interface Props {
  users: User[] | undefined;
  isLoading: boolean;
}

const noopFilter: FilterFn<unknown> = () => true;
const noopSort: SortingFn<unknown> = () => 0;
const NOOP_FILTER_FNS = {
  fuzzy: noopFilter,
  dateRange: noopFilter,
  exactMatch: noopFilter,
  arrIncludesSomeExact: noopFilter,
};
const NOOP_SORTING_FNS = { checkbox: noopSort };

export default function ServiceUsersTableV2({ users, isLoading }: Props) {
  const { mutate } = useSWRConfig();
  const { permission } = usePermissions();
  const router = useRouter();

  const [createOpen, setCreateOpen] = useState(false);

  useV2TopbarRight(
    <OzButton
      variant="primary"
      type="button"
      onClick={() => setCreateOpen(true)}
      disabled={!permission.users.create}
    >
      <PlusCircle size={14} />
      Create Service User
    </OzButton>,
  );

  const [search, setSearch] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const [sorting, setSorting] = useState<SortingState>([
    { id: "name", desc: false },
  ]);
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  });

  const all = useMemo(() => users ?? [], [users]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return all;
    return all.filter((u) => {
      const haystack = [u.name, u.role].filter(Boolean).join(" ").toLowerCase();
      return haystack.includes(q);
    });
  }, [all, search]);

  useEffect(() => {
    setPagination((prev) => ({ ...prev, pageIndex: 0 }));
  }, [search]);

  const columns = useMemo<ColumnDef<User>[]>(
    () => [
      {
        id: "name",
        accessorFn: (u) => u.name ?? u.id ?? "",
        sortingFn: "text",
        header: ({ column }) => (
          <SortHeader column={column} label="Service user" />
        ),
        cell: ({ row }) => <ServiceUserNameCellV2 user={row.original} />,
      },
      {
        id: "role",
        accessorFn: (u) => u.role ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Role" />,
        cell: ({ row }) => <UserRoleCellV2 user={row.original} />,
      },
      {
        id: "actions",
        // Two 28-square icon buttons + gutter — narrow column.
        size: 110,
        enableSorting: false,
        header: () => null,
        cell: ({ row }) => <ServiceUserActions user={row.original} />,
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
    getRowId: (u) => u.id ?? "",
    filterFns: NOOP_FILTER_FNS,
    sortingFns: NOOP_SORTING_FNS,
  });

  const refreshClick = () => {
    setRefreshing(true);
    Promise.all([mutate("/users?service_user=true"), mutate("/groups")]).finally(
      () => setRefreshing(false),
    );
  };

  const handleRowClick = (row: Row<User>, event: React.MouseEvent) => {
    const target = event.target as HTMLElement;
    if (
      target.closest(
        "button, a, [role='switch'], input, select, [data-stop-row-click]",
      )
    ) {
      return;
    }
    router.push(`/team/user?id=${row.original.id}&service_user=true`);
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
          <h1 className="text-[24px] font-semibold tracking-tight">
            Users &amp; Groups
          </h1>
          <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
            Identity drives policy. Humans, machines, and the groups that scope
            their access — managed in one place.
          </p>
        </header>

        <TeamTabs />

        <Modal
          open={createOpen}
          onOpenChange={setCreateOpen}
          key={createOpen ? "create-open" : "create-closed"}
        >
          {createOpen && (
            <ServiceUserModalContent onSuccess={() => setCreateOpen(false)} />
          )}
        </Modal>

        {isColdStart ? (
          <ServiceUsersEmptyState
            canCreate={permission.users.create}
            onCreate={() => setCreateOpen(true)}
          />
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2.5">
              <div className="inline-flex h-8 w-[280px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-2.5">
                <span className="text-oz2-text-faint">{ICONS.search}</span>
                <input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Search by name or role…"
                  className="h-full flex-1 border-0 bg-transparent text-[12.5px] outline-none placeholder:text-oz2-text-faint"
                />
              </div>

              <PageSizeCombobox
                value={pageInfo.pageSize}
                onChange={(n) => table.setPageSize(n)}
              />

              <button
                type="button"
                onClick={refreshClick}
                aria-label="Refresh service users"
                className="grid h-8 w-8 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 hover:border-oz2-border-strong hover:bg-oz2-hover"
              >
                <span className={refreshing ? "animate-spin text-oz2-acc" : ""}>
                  {ICONS.refresh}
                </span>
              </button>

              <span className="ml-auto font-mono text-[11px] uppercase tracking-[0.04em] text-oz2-text-faint">
                {total} service {total === 1 ? "user" : "users"}
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
                          ? "Loading service users…"
                          : "No service users match your filter."}
                      </OzTableCell>
                    </OzTableRow>
                  )}
                </OzTableBody>
              </OzTable>

              <div className="flex flex-wrap items-center justify-between gap-3 border-t border-oz2-border-soft bg-oz2-bg-sunken px-[18px] py-3 text-[13.5px]">
                <span className="text-oz2-text-muted">
                  {total === 0
                    ? "0 service users"
                    : `Showing ${pageStart}–${pageEnd} of ${total}`}
                </span>
                <Pager
                  page={pageInfo.pageIndex + 1}
                  totalPages={Math.max(1, table.getPageCount())}
                  onChange={(p) => table.setPageIndex(p - 1)}
                />
              </div>
            </OzCard>

            <ServiceUsersInfoBanner />
          </>
        )}
      </div>
    </TooltipProvider>
  );
}

// ServiceUserActions — icon-only Edit + Delete pair. Edit (pencil)
// navigates to the detail screen mirroring the row-click target;
// Delete (trash) runs the confirm-dialog flow used by UserActionCellV2.
// Buttons carry `data-stop-row-click` so they don't double-fire the
// row's onClick.
function ServiceUserActions({ user }: { user: User }) {
  const { confirm } = useDialog();
  const { permission } = usePermissions();
  const router = useRouter();
  const userRequest = useApiCall<User>("/users");
  const { mutate } = useSWRConfig();

  const onEdit = () => {
    router.push(`/team/user?id=${user.id}&service_user=true`);
  };

  const onDelete = async () => {
    const name = user.name || "Service user";
    const choice = await confirm({
      title: `Delete '${name}'?`,
      description:
        "Deleting this service user will revoke its API tokens and remove any peers it owns. This action cannot be undone.",
      confirmText: "Delete",
      cancelText: "Cancel",
      type: "danger",
    });
    if (!choice) return;
    notify({
      title: `'${name}' deleted`,
      description: "Service user was successfully deleted.",
      promise: userRequest.del("", `/${user.id}`).then(() => {
        mutate(`/users?service_user=true`);
      }),
      loadingMessage: "Deleting the service user...",
    });
  };

  return (
    <div
      className="flex items-center justify-end gap-1.5 pr-3"
      data-stop-row-click
    >
      <button
        type="button"
        onClick={onEdit}
        aria-label={`Edit ${user.name || "service user"}`}
        className="grid h-7 w-7 place-items-center rounded-[8px] border border-oz2-border bg-oz2-surface text-oz2-text-2 transition-colors hover:border-oz2-border-strong hover:bg-oz2-hover hover:text-oz2-text"
      >
        <PencilLine size={13} />
      </button>
      <button
        type="button"
        onClick={onDelete}
        disabled={!permission.users.delete}
        data-cy="delete-user"
        aria-label={`Delete ${user.name || "service user"}`}
        className="grid h-7 w-7 place-items-center rounded-[8px] border border-oz2-border bg-transparent text-oz2-err transition-colors hover:border-oz2-err hover:bg-oz2-err-bg disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:border-oz2-border disabled:hover:bg-transparent"
      >
        <Trash2 size={13} />
      </button>
    </div>
  );
}

// ServiceUsersInfoBanner — soft violet banner at the footer of the
// list. The handoff plants this hint inline with the table so a brand
// new operator scanning the page understands the distinction between
// human and service users without leaving for the docs.
function ServiceUsersInfoBanner() {
  return (
    <div className="flex items-start gap-3 rounded-oz2-card border border-oz2-acc-soft-2 bg-oz2-acc-soft px-4 py-3 text-[13px] text-oz2-acc-text">
      <span aria-hidden className="mt-0.5 shrink-0">
        <Info size={14} />
      </span>
      <p className="leading-[1.55]">
        <strong className="font-semibold">Service users</strong> are machine
        identities — bots, CI runners, automations. They join the mesh with API
        tokens instead of SSO. Assign them to groups to inherit the right
        policies.
      </p>
    </div>
  );
}

function ServiceUsersEmptyState({
  canCreate,
  onCreate,
}: {
  canCreate: boolean;
  onCreate: () => void;
}) {
  return (
    <OzEmptyState
      title="Create Service User"
      description="Service users are non-login identities — perfect for API tokens, automations, and CI/CD pipelines that need to authenticate without a human."
      primaryAction={
        <OzButton
          variant="primary"
          type="button"
          onClick={onCreate}
          disabled={!canCreate}
        >
          <PlusCircle size={14} />
          Create Service User
        </OzButton>
      }
      learnMore={
        <>
          Learn more about{" "}
          <a
            href="https://docs.openzro.io/how-to/access-openzro-public-api"
            target="_blank"
            rel="noopener noreferrer"
            className="font-medium text-oz2-acc-text underline-offset-2 hover:underline"
          >
            Service Users
          </a>
          .
        </>
      }
      helperCards={[
        {
          icon: <Bot size={16} />,
          title: "When to use a service user",
          description:
            "Long-lived automations, CI pipelines, infra-as-code agents — anywhere a human session would expire.",
          href: "https://docs.openzro.io/how-to/access-openzro-public-api",
        },
        {
          icon: <KeyRound size={16} />,
          title: "Issue an API token",
          description:
            "Each service user can mint scoped Personal Access Tokens to call the openZro public API.",
          href: "https://docs.openzro.io/how-to/access-openzro-public-api",
        },
        {
          icon: <ShieldCheck size={16} />,
          title: "Role-bound access",
          description:
            "Assign Admin or User role so the token inherits the right policy surface — least privilege by default.",
          href: "https://docs.openzro.io/how-to/manage-users",
        },
        {
          icon: <BookOpen size={16} />,
          title: "Public API reference",
          description:
            "Explore the full set of endpoints a service user can drive against your workspace.",
          href: "https://docs.openzro.io/how-to/access-openzro-public-api",
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
