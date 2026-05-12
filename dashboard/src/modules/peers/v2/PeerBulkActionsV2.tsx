"use client";

import FancyToggleSwitch from "@components/FancyToggleSwitch";
import { Modal, ModalContent } from "@components/modal/Modal";
import { notify } from "@components/Notification";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import { RowSelectionState } from "@tanstack/react-table";
import { useApiCall } from "@utils/api";
import { uniq, uniqBy } from "lodash";
import { CirclePlus, FolderGit2, RedoDot, Trash2, X } from "lucide-react";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import { useDialog } from "@/contexts/DialogProvider";
import { usePeers } from "@/contexts/PeersProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Group, GroupPeer } from "@/interfaces/Group";
import { Peer } from "@/interfaces/Peer";
import useGroupHelper from "@/modules/groups/useGroupHelper";

// PeerBulkActionsV2 — inline bulk-action bar for /peers v2. Lives at
// the top of the table card (instead of the legacy floating bar) and
// reuses the legacy mass-assignment logic verbatim — same merge/
// replace flow for groups, same delete-all confirm path. The legacy
// PeerMultiSelect component stays available for any non-migrated
// surface.

interface Props {
  selectedPeers: RowSelectionState;
  onCanceled: () => void;
}

export default function PeerBulkActionsV2({
  selectedPeers,
  onCanceled,
}: Props) {
  const { mutate } = useSWRConfig();
  const { confirm } = useDialog();
  const { permission } = usePermissions();
  const { peers } = usePeers();

  const groupCall = useApiCall<Group>("/groups");
  const getAllGroups = useApiCall<Group[]>("/groups").get;
  const peerCall = useApiCall<Peer>("/peers", true);

  const [showGroupModal, setShowGroupModal] = useState(false);
  const [selectedGroups, setSelectedGroups, { getAllGroupCalls }] =
    useGroupHelper({ initial: [] });
  const [replaceAllGroups, setReplaceAllGroups] = useState(false);
  const [isLoading, setIsLoading] = useState(false);

  const peerCount = useMemo(
    () => Object.keys(selectedPeers).length,
    [selectedPeers],
  );

  const closeGroupModal = () => {
    setShowGroupModal(false);
    setSelectedGroups([]);
    setReplaceAllGroups(false);
  };

  // addGroupsToPeers — copy of the legacy PeerMultiSelect flow.
  // Walks every selected group, computes the resulting peer set
  // (respecting replaceAllGroups), and PUTs each updated group.
  // The legacy comment chain inside this body is preserved for
  // parity since the merge/overwrite math is non-obvious.
  const addGroupsToPeers = async () => {
    if (replaceAllGroups) {
      const choice = await confirm({
        title: `Overwrite existing groups?`,
        description: `Are you sure you want to overwrite the existing groups of your ${peerCount} selected peer(s)? This action cannot be undone.`,
        confirmText: "Overwrite",
        cancelText: "Cancel",
        type: "warning",
      });
      if (!choice) return;
    }
    setIsLoading(true);

    try {
      const allGroups = await getAllGroups();
      const selectedGroupCalls = getAllGroupCalls();
      const selectedPeerIDs = Object.keys(selectedPeers);
      let currentSelectedGroups = await Promise.all(selectedGroupCalls);
      currentSelectedGroups = currentSelectedGroups
        .map((g) => allGroups?.find((group) => group.id === g.id) ?? g)
        .filter((g) => g !== undefined);
      let selectedPeerGroups: Group[] = [];

      if (replaceAllGroups) {
        // Get all the groups of the selected peers
        selectedPeerGroups = uniqBy(
          Object.keys(selectedPeers)
            .map((id) => peers?.find((p) => p.id === id)?.groups ?? [])
            .flat()
            .filter((g) => g !== undefined),
          "id",
        );
        // Map back to fresh group objects from the all-groups fetch
        selectedPeerGroups =
          allGroups?.filter((group) =>
            selectedPeerGroups.find((g) => g.id === group.id),
          ) ?? [];
        // Remove the selected peers from those groups
        selectedPeerGroups = selectedPeerGroups.map((group) => {
          const previousPeers = group?.peers as GroupPeer[];
          const previousPeerIDs = previousPeers
            ?.map((p) => p.id)
            .filter((id) => !selectedPeerIDs.includes(id))
            .filter((id) => id !== "" && id !== null && id !== undefined);
          return { ...group, peers: previousPeerIDs };
        }) as Group[];
      }

      // Add selected peers to the chosen target groups
      currentSelectedGroups = currentSelectedGroups
        .map((group) => {
          const previousPeers = (group?.peers as GroupPeer[]) ?? [];
          const previousPeerIDs = previousPeers.map((p) => p.id);
          const merged = uniq(
            [...previousPeerIDs, ...selectedPeerIDs].filter(
              (p) => p !== "" && p !== null && p !== undefined,
            ),
          );
          return { ...group, peers: merged };
        })
        .filter((g) => g !== undefined) as Group[];

      currentSelectedGroups = uniqBy(
        [...currentSelectedGroups, ...selectedPeerGroups],
        "id",
      ).filter((group) => group.name !== "All");

      const updateGroupCalls = () =>
        Promise.all(
          currentSelectedGroups.map((group) =>
            groupCall.put(
              {
                name: group.name,
                peers: group.peers,
                resources: group.resources,
              },
              "/" + group.id,
            ),
          ),
        );

      notify({
        title: "Assign Groups to Peers",
        description: "Groups were successfully assigned to the peers",
        promise: updateGroupCalls()
          .then(() => {
            if (currentSelectedGroups.length > 0) {
              mutate("/groups");
              mutate("/peers");
              closeGroupModal();
              onCanceled();
            }
          })
          .finally(() => setIsLoading(false)),
        loadingMessage: "Updating the groups of the selected peers...",
      });
    } catch {
      setIsLoading(false);
    }
  };

  const deleteAllPeers = async () => {
    const choice = await confirm({
      title: `Delete '${peerCount}' ${peerCount > 1 ? "peers" : "peer"}?`,
      description: `Are you sure you want to delete these peers? This action cannot be undone.`,
      confirmText: "Delete All",
      cancelText: "Cancel",
      type: "danger",
    });
    if (!choice) return;

    const batchDeleteCalls = () =>
      Object.keys(selectedPeers).map((id) => peerCall.del({}, `/${id}`));

    notify({
      title: "Delete Peers",
      description: "Peers were successfully deleted",
      promise: Promise.all(batchDeleteCalls()).then(() => {
        mutate("/peers");
        onCanceled();
      }),
      loadingMessage: "Deleting the selected peers...",
    });
  };

  if (peerCount === 0) return null;

  const canAssign = permission.peers.read && permission.groups.update;
  const canDelete = permission.peers.delete;

  return (
    <>
      <div className="flex items-center justify-between gap-3 border-b border-oz2-border-soft bg-oz2-acc-soft px-[18px] py-2.5 text-[13.5px]">
        <span className="font-medium text-oz2-acc-text">
          {peerCount} {peerCount === 1 ? "peer" : "peers"} selected
        </span>
        <div className="flex items-center gap-2">
          <OzButton
            type="button"
            variant="default"
            disabled={!canAssign}
            onClick={() => setShowGroupModal(true)}
          >
            <FolderGit2 size={14} />
            Assign groups
          </OzButton>
          <OzButton
            type="button"
            variant="default"
            disabled={!canDelete}
            onClick={deleteAllPeers}
            className="text-oz2-err"
          >
            <Trash2 size={14} />
            Delete
          </OzButton>
          <button
            type="button"
            onClick={onCanceled}
            aria-label="Clear selection"
            className="grid h-7 w-7 cursor-pointer place-items-center rounded-md text-oz2-text-muted transition-colors hover:bg-oz2-hover hover:text-oz2-text"
          >
            <X size={14} />
          </button>
        </div>
      </div>

      <Modal open={showGroupModal} onOpenChange={setShowGroupModal}>
        <ModalContent
          showClose
          maxWidthClass="sm:max-w-[520px]"
          className="overflow-hidden rounded-[18px] border border-oz2-border bg-oz2-surface p-0 shadow-oz2-lg"
        >
          <div className="px-6 pt-7 pb-3">
            <h2 className="text-[18px] font-semibold tracking-tight text-oz2-text">
              Assign groups to {peerCount}{" "}
              {peerCount === 1 ? "peer" : "peers"}
            </h2>
            <p className="mt-1 text-[13.5px] leading-[1.5] text-oz2-text-muted">
              Selected groups will be added to the peers. Existing groups stay
              unless you choose to overwrite.
            </p>
          </div>

          <div className="px-6 pb-2">
            <PeerGroupSelector
              onChange={setSelectedGroups}
              values={selectedGroups}
            />
          </div>

          <div className="px-6 pb-5">
            <FancyToggleSwitch
              value={replaceAllGroups}
              onChange={setReplaceAllGroups}
              label={
                <div className="flex items-center gap-2">
                  <RedoDot size={14} />
                  Overwrite existing groups
                </div>
              }
              helpText="Remove the peers' previously assigned groups and replace them with the ones selected above."
            />
          </div>

          <div className="flex items-center justify-end gap-2 border-t border-oz2-border bg-oz2-bg-soft px-6 py-3.5">
            <OzButton
              type="button"
              variant="default"
              onClick={closeGroupModal}
              disabled={isLoading}
            >
              Cancel
            </OzButton>
            <OzButton
              type="button"
              variant="primary"
              onClick={addGroupsToPeers}
              disabled={selectedGroups.length === 0 || isLoading}
            >
              {replaceAllGroups ? <RedoDot size={14} /> : <CirclePlus size={14} />}
              {replaceAllGroups ? "Overwrite groups" : "Add groups"}
            </OzButton>
          </div>
        </ModalContent>
      </Modal>
    </>
  );
}

// The legacy PeerMultiSelect stays untouched in
// modules/peers/PeerMultiSelect.tsx for any surface that hasn't
// migrated.
