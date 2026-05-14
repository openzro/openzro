"use client";

import { Modal, ModalContent } from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { notify } from "@components/Notification";
import {
  ColumnDef,
  FilterFn,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  RowSelectionState,
  SortingFn,
  SortingState,
  useReactTable,
} from "@tanstack/react-table";
import useFetchApi, { useApiCall } from "@utils/api";
import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  FolderGit2,
  PencilLineIcon,
  Search,
} from "lucide-react";
import * as React from "react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzCheckbox from "@/components/v2/OzCheckbox";
import {
  OzTable,
  OzTableBody,
  OzTableCell,
  OzTableHead,
  OzTableHeader,
  OzTableRow,
} from "@/components/v2/OzTable";
import { Group, GroupPeer } from "@/interfaces/Group";
import { Peer } from "@/interfaces/Peer";
import { EditGroupNameModal } from "@/modules/groups/EditGroupNameModal";
import PeerAddressCell from "@/modules/peers/PeerAddressCell";
import PeerNameCell from "@/modules/peers/PeerNameCell";
import { PeerOSCell } from "@/modules/peers/PeerOSCell";

type Props = {
  group: Group;
  open: boolean;
  setOpen: (open: boolean) => void;
  onUpdate?: (g: Group) => void;
  useSave?: boolean;
};

export const AssignPeerToGroupModal = ({
  group,
  open = false,
  setOpen,
  onUpdate,
  useSave = true,
}: Props) => {
  return (
    <Modal open={open} onOpenChange={setOpen} key={open ? "1" : "0"}>
      {open && (
        <AssignGroupToPeerModalContent
          group={group}
          onSuccess={(g) => {
            setOpen(false);
            onUpdate && onUpdate(g);
          }}
          useSave={useSave}
        />
      )}
    </Modal>
  );
};

type ContentProps = {
  group: Group;
  onSuccess?: (g: Group) => void;
  useSave?: boolean;
};

// useReactTable's global FilterFns / SortingFns interface extensions
// (declared by the legacy DataTable) require every caller to supply
// these names. We do not use any of them here, so no-op stubs keep
// the typecheck happy without altering runtime behavior. Same pattern
// as PeersTableV2.
const noopFilter: FilterFn<unknown> = () => true;
const noopSort: SortingFn<unknown> = () => 0;
const NOOP_FILTER_FNS = {
  fuzzy: noopFilter,
  dateRange: noopFilter,
  exactMatch: noopFilter,
  arrIncludesSomeExact: noopFilter,
};
const NOOP_SORTING_FNS = {
  checkbox: noopSort,
};

export const AssignGroupToPeerModalContent = ({
  group,
  onSuccess,
  useSave,
}: ContentProps) => {
  const { data: peers, isLoading } = useFetchApi<Peer[]>("/peers");
  const { mutate } = useSWRConfig();
  const groupRequest = useApiCall<Group>("/groups");
  const [initialPeersSet, setInitialPeersSet] = useState(false);
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({});
  const isAllGroup = group.name === "All";
  const [sorting, setSorting] = useState<SortingState>([
    { id: "select", desc: false },
    { id: "name", desc: false },
  ]);
  const [search, setSearch] = useState("");

  const [groupNameModal, setGroupNameModal] = useState(false);
  const [groupName, setGroupName] = useState(group.name);

  const onGroupNameUpdate = (name: string) => {
    setGroupNameModal(false);
    setGroupName(name);
  };

  const getInitialSelectedPeers = useCallback(() => {
    if (!group || !peers) return undefined;
    const ids = group?.peers
      ?.map((p) => (typeof p === "string" ? p : p.id))
      .filter((p): p is string => p !== undefined);
    if (!ids) return {};
    return ids.reduce(
      (acc, peerId) => {
        acc[peerId] = true;
        return acc;
      },
      {} as Record<string, boolean>,
    );
  }, [group, peers]);

  useEffect(() => {
    if (initialPeersSet) return;
    const initial = getInitialSelectedPeers();
    if (initial === undefined) return;
    setRowSelection(initial);
    setInitialPeersSet(true);
  }, [getInitialSelectedPeers, initialPeersSet]);

  // Case-insensitive search across name, dns_label, ip, user email/name.
  // Avoids the legacy DataTable global filter (which used the custom
  // fuzzy filter declared at the project-wide TanStack module level).
  const filtered = useMemo(() => {
    if (!peers) return [] as Peer[];
    const q = search.trim().toLowerCase();
    if (!q) return peers;
    return peers.filter((peer) => {
      const haystacks = [
        peer.name,
        peer.dns_label,
        peer.ip,
        peer.user?.email,
        peer.user?.name,
      ];
      return haystacks.some((h) => h?.toLowerCase().includes(q));
    });
  }, [peers, search]);

  const columns = useMemo<ColumnDef<Peer>[]>(
    () => [
      {
        id: "select",
        size: 44,
        header: ({ table }) => (
          <OzCheckbox
            checked={
              table.getIsAllPageRowsSelected()
                ? true
                : table.getIsSomePageRowsSelected()
                  ? "indeterminate"
                  : false
            }
            onCheckedChange={(value) =>
              table.toggleAllPageRowsSelected(!!value)
            }
            aria-label="Select all"
          />
        ),
        cell: ({ row }) => (
          <OzCheckbox
            checked={row.getIsSelected()}
            onCheckedChange={(value) => row.toggleSelected(!!value)}
            aria-label={`Select ${row.original.name}`}
            onClick={(e) => e.stopPropagation()}
          />
        ),
        enableSorting: false,
      },
      {
        accessorKey: "name",
        header: ({ column }) => (
          <SortHeader column={column}>Name</SortHeader>
        ),
        sortingFn: (a, b) =>
          (a.original.name || "").localeCompare(b.original.name || ""),
        cell: ({ row }) => (
          <PeerNameCell peer={row.original} linkToPeer={false} />
        ),
      },
      {
        accessorKey: "dns_label",
        header: ({ column }) => (
          <SortHeader column={column}>Address</SortHeader>
        ),
        sortingFn: (a, b) =>
          (a.original.dns_label || "").localeCompare(
            b.original.dns_label || "",
          ),
        cell: ({ row }) => <PeerAddressCell peer={row.original} />,
      },
      {
        accessorKey: "os",
        header: ({ column }) => <SortHeader column={column}>OS</SortHeader>,
        size: 80,
        sortingFn: (a, b) =>
          (a.original.os || "").localeCompare(b.original.os || ""),
        cell: ({ row }) => (
          <PeerOSCell
            os={row.original.os}
            serial={row.original.serial_number}
          />
        ),
      },
    ],
    [],
  );

  const table = useReactTable({
    data: filtered,
    columns,
    state: { sorting, rowSelection },
    onSortingChange: setSorting,
    onRowSelectionChange: setRowSelection,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    enableRowSelection: !isAllGroup,
    getRowId: (peer) => peer.id ?? peer.name,
    filterFns: NOOP_FILTER_FNS,
    sortingFns: NOOP_SORTING_FNS,
  });

  const selectedCount = Object.keys(rowSelection).length;

  const handleOnSave = async () => {
    const selectedPeerIds = Object.keys(rowSelection);
    const selectedPeers = (peers ?? []).filter((p) =>
      selectedPeerIds.includes(p.id ?? ""),
    );

    if (!useSave) {
      onSuccess?.({
        ...group,
        name: groupName,
        peers: selectedPeers.map(
          (peer) => ({ id: peer.id, name: peer.name }) as GroupPeer,
        ),
        peers_count: selectedPeers.length,
        resources: group.resources,
        keepClientState: true,
      });
      return;
    }

    const hasGroupID = !!group?.id;
    const request = hasGroupID
      ? () =>
          groupRequest.put(
            {
              name: group.name,
              peers: selectedPeers.map((peer) => peer.id),
              resources: group.resources,
            },
            "/" + group?.id,
          )
      : () =>
          groupRequest.post({
            name: group.name,
            peers: selectedPeers.map((peer) => peer.id),
            resources: group.resources,
          });

    notify({
      title: "Saving changes",
      description: `${group?.name || "Group"} was successfully saved.`,
      promise: request()
        .then((g: Group) => {
          mutate("/groups");
          onSuccess?.(g);
        })
        .catch(() => {}),
      loadingMessage: "Updating group...",
    });
  };

  return (
    <ModalContent maxWidthClass={"max-w-4xl"} className={"pb-0"} showClose>
      {groupNameModal && (
        <EditGroupNameModal
          initialName={groupName}
          open={groupNameModal}
          onOpenChange={setGroupNameModal}
          onSuccess={onGroupNameUpdate}
        />
      )}

      <ModalHeader
        title={
          <span className="inline-flex items-center gap-2">
            <FolderGit2
              size={16}
              className="shrink-0 text-oz2-text-faint"
            />
            <span>{groupName}</span>
            {groupName !== "All" && (
              <button
                type="button"
                onClick={() => setGroupNameModal(true)}
                className="grid h-7 w-7 place-items-center rounded-oz2-input text-oz2-text-faint transition-colors hover:bg-oz2-hover hover:text-oz2-text"
                aria-label="Rename group"
              >
                <PencilLineIcon size={14} />
              </button>
            )}
          </span>
        }
        description={
          isAllGroup
            ? "View assigned peers for this group"
            : "Manage assigned peers for this group"
        }
      />

      {/* Toolbar: search on the left, selection counter + confirm on the
          right. Mirrors the layout the AuditTimelineV2 toolbar adopted. */}
      <div className="flex flex-wrap items-center gap-3 px-8 pb-4">
        <div className="inline-flex h-9 w-[320px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3">
          <Search size={14} className="shrink-0 text-oz2-text-faint" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search by name, IP or owner…"
            className="h-full flex-1 border-0 bg-transparent text-[13px] outline-none placeholder:text-oz2-text-faint"
          />
        </div>

        <div className="ml-auto flex items-center gap-4">
          {selectedCount > 0 && (
            <span className="text-[13px] text-oz2-text-muted">
              <span className="font-medium text-oz2-acc-text">
                {selectedCount}
              </span>{" "}
              {selectedCount === 1 ? "Peer" : "Peers"} selected
            </span>
          )}
          {!isAllGroup && (
            <OzButton
              variant={"primary"}
              disabled={peers?.length === 0}
              onClick={() => handleOnSave()}
            >
              Confirm Changes
            </OzButton>
          )}
        </div>
      </div>

      <div className="max-h-[60vh] overflow-y-auto border-t border-oz2-border-soft">
        {!initialPeersSet || isLoading ? (
          <div className="px-8 py-10 text-center text-[13px] text-oz2-text-muted">
            Loading peers…
          </div>
        ) : peers && peers.length === 0 ? (
          <div className="px-8 py-12 text-center text-[13px] text-oz2-text-muted">
            <p className="font-medium text-oz2-text">No peers yet</p>
            <p className="mt-1">
              In order to view or assign peers to a group, you need to have at
              least one peer.
            </p>
          </div>
        ) : (
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
                  data-state={row.getIsSelected() ? "selected" : undefined}
                  className={
                    !isAllGroup ? "cursor-pointer" : undefined
                  }
                  onClick={
                    !isAllGroup ? () => row.toggleSelected() : undefined
                  }
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
                    className="px-[18px] py-10 text-center text-oz2-text-muted"
                  >
                    No peers match your search.
                  </OzTableCell>
                </OzTableRow>
              )}
            </OzTableBody>
          </OzTable>
        )}
      </div>
    </ModalContent>
  );
};

function SortHeader<T>({
  column,
  children,
}: {
  column: import("@tanstack/react-table").Column<T, unknown>;
  children: React.ReactNode;
}) {
  const sorted = column.getIsSorted();
  return (
    <button
      type="button"
      onClick={() => column.toggleSorting()}
      className="inline-flex items-center gap-1.5 text-oz2-text-muted transition-colors hover:text-oz2-text"
    >
      {children}
      {sorted === "asc" ? (
        <ArrowUp size={11} />
      ) : sorted === "desc" ? (
        <ArrowDown size={11} />
      ) : (
        <ArrowUpDown size={11} className="opacity-60" />
      )}
    </button>
  );
}
