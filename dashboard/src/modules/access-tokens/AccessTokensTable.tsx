"use client";

import { notify } from "@components/Notification";
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
  getSortedRowModel,
  SortingFn,
  SortingState,
  useReactTable,
} from "@tanstack/react-table";
import useFetchApi, { useApiCall } from "@utils/api";
import dayjs from "dayjs";
import { Calendar, History, KeyRound, Trash2 } from "lucide-react";
import { usePathname } from "next/navigation";
import React from "react";
import { useSWRConfig } from "swr";
import OzCard from "@/components/v2/OzCard";
import OzPill from "@/components/v2/OzPill";
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
import { useLocalStorage } from "@/hooks/useLocalStorage";
import { AccessToken } from "@/interfaces/AccessToken";
import { SetupKey } from "@/interfaces/SetupKey";
import { User } from "@/interfaces/User";

// AccessTokensTable — v2 paint over the legacy DataTable/Card pair.
// Lists every Personal Access Token issued to the user, with sort
// state persisted in localStorage (same key the legacy used so
// existing operators keep their preference). Functionality
// unchanged: read /users/{id}/tokens, delete via the
// useApiCall.del() path, refresh the SWR cache on success.

type Props = {
  user: User;
};

const noopFilter: FilterFn<unknown> = () => true;
const noopSort: SortingFn<unknown> = () => 0;
const NOOP_FILTER_FNS = {
  fuzzy: noopFilter,
  dateRange: noopFilter,
  exactMatch: noopFilter,
  arrIncludesSomeExact: noopFilter,
};
const NOOP_SORTING_FNS = { checkbox: noopSort };

export default function AccessTokensTable({ user }: Readonly<Props>) {
  const { data: tokens } = useFetchApi<AccessToken[]>(
    `/users/${user.id}/tokens`,
    true,
  );
  const path = usePathname();

  const [sorting, setSorting] = useLocalStorage<SortingState>(
    "openzro-table-sort" + path,
    [{ id: "name", desc: true }],
  );

  const columns = React.useMemo<ColumnDef<AccessToken>[]>(
    () => [
      {
        id: "name",
        accessorFn: (t) => t.name ?? "",
        sortingFn: "text",
        header: ({ column }) => <SortHeader column={column} label="Name" />,
        cell: ({ row }) => <TokenNameCell token={row.original} />,
      },
      {
        id: "expiration_date",
        accessorFn: (t) => t.expiration_date,
        sortingFn: "datetime",
        header: ({ column }) => <SortHeader column={column} label="Expires" />,
        cell: ({ row }) => <ExpiresCell token={row.original} />,
      },
      {
        id: "last_used",
        accessorFn: (t) => t.last_used ?? "",
        sortingFn: "datetime",
        header: ({ column }) => (
          <SortHeader column={column} label="Last used" />
        ),
        cell: ({ row }) => <LastUsedCell date={row.original.last_used} />,
      },
      {
        id: "actions",
        size: 80,
        enableSorting: false,
        header: () => null,
        cell: ({ row }) => (
          <DeleteCell token={row.original} userId={user.id} />
        ),
      },
    ],
    [user.id],
  );

  const table = useReactTable({
    data: tokens ?? [],
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getRowId: (t) => t.id,
    filterFns: NOOP_FILTER_FNS,
    sortingFns: NOOP_SORTING_FNS,
  });

  if (!tokens || tokens.length === 0) {
    return (
      <OzCard className="border-dashed">
        <div className="flex flex-col items-center gap-3 px-6 py-10 text-center">
          <div
            aria-hidden
            className="grid h-10 w-10 place-items-center rounded-full border border-oz2-border-soft bg-oz2-bg-sunken text-oz2-text-2"
          >
            <KeyRound size={18} />
          </div>
          <div>
            <p className="text-[14px] font-medium text-oz2-text">
              No access tokens
            </p>
            <p className="mt-1 text-[12.5px] text-oz2-text-muted">
              You don&apos;t have any access tokens yet. Create one to call the
              openZro public API on this user&apos;s behalf.
            </p>
          </div>
        </div>
      </OzCard>
    );
  }

  return (
    <TooltipProvider delayDuration={250} skipDelayDuration={100}>
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
          </OzTableBody>
        </OzTable>
      </OzCard>
    </TooltipProvider>
  );
}

function TokenNameCell({ token }: { token: AccessToken }) {
  const isValid = dayjs(token.expiration_date).isAfter(dayjs());
  return (
    <div className="flex items-center gap-3 py-1">
      <div
        aria-hidden
        className="grid h-8 w-8 shrink-0 place-items-center rounded-[8px] border border-oz2-border-soft bg-oz2-bg-sunken text-oz2-text-2"
      >
        <KeyRound size={14} />
      </div>
      <div className="flex min-w-0 flex-col">
        <div className="flex items-center gap-2">
          <span className="truncate text-[13.5px] font-medium text-oz2-text">
            {token.name}
          </span>
          {!isValid && <OzPill variant="err">Expired</OzPill>}
        </div>
      </div>
    </div>
  );
}

function ExpiresCell({ token }: { token: AccessToken }) {
  return (
    <span className="inline-flex items-center gap-2 text-[12.5px] text-oz2-text-2">
      <Calendar size={13} className="text-oz2-text-faint" />
      {dayjs(token.expiration_date).format("ddd, D MMMM YYYY")}
    </span>
  );
}

function LastUsedCell({ date }: { date: Date | undefined }) {
  if (!date) {
    return <span className="text-[12.5px] text-oz2-text-faint">—</span>;
  }
  const neverUsed = dayjs(date).isBefore(dayjs().subtract(2000, "years"));
  if (neverUsed) {
    return <span className="text-[12.5px] text-oz2-text-faint">Never</span>;
  }

  const formatted = dayjs(date).format("ddd, D MMMM YYYY [at] HH:mm");

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex cursor-default items-center gap-2 text-[12.5px] text-oz2-text-2">
          <History size={13} className="text-oz2-text-faint" />
          {dayjs(date).fromNow()}
        </span>
      </TooltipTrigger>
      <TooltipContent>
        <span className="text-[12px]">Last used on {formatted}</span>
      </TooltipContent>
    </Tooltip>
  );
}

function DeleteCell({
  token,
  userId,
}: {
  token: AccessToken;
  userId: string;
}) {
  const { permission } = usePermissions();
  const { confirm } = useDialog();
  const { mutate } = useSWRConfig();
  const deleteRequest = useApiCall<SetupKey>(
    `/users/${userId}/tokens/${token.id}`,
  );

  const handleRevoke = async () => {
    notify({
      title: token.name,
      description: "Access token was successfully deleted.",
      promise: deleteRequest.del().then(() => {
        mutate(`/users/${userId}/tokens`);
      }),
      loadingMessage: "Deleting the access token...",
    });
  };

  const handleConfirm = async () => {
    const choice = await confirm({
      title: `Delete '${token.name}'?`,
      description:
        "Are you sure you want to delete this token? This action cannot be undone.",
      confirmText: "Delete",
      cancelText: "Cancel",
      type: "danger",
    });
    if (!choice) return;
    handleRevoke().then();
  };

  return (
    <div
      className="flex items-center justify-end gap-1.5 pr-3"
      data-stop-row-click
    >
      <button
        type="button"
        onClick={handleConfirm}
        disabled={!permission.pats.delete}
        data-cy="access-token-delete"
        aria-label={`Delete ${token.name}`}
        className="grid h-7 w-7 place-items-center rounded-[8px] border border-oz2-border bg-transparent text-oz2-err transition-colors hover:border-oz2-err hover:bg-oz2-err-bg disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:border-oz2-border disabled:hover:bg-transparent"
      >
        <Trash2 size={13} />
      </button>
    </div>
  );
}

function SortHeader({
  column,
  label,
}: {
  column: Column<AccessToken, unknown>;
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
