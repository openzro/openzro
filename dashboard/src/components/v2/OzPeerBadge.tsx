"use client";

import classNames from "classnames";
import { EyeIcon, MonitorSmartphoneIcon, SquarePen } from "lucide-react";
import * as React from "react";
import { useMemo, useState } from "react";
import { useGroups } from "@/contexts/GroupsProvider";
import { Group } from "@/interfaces/Group";
import { AssignPeerToGroupModal } from "@/modules/groups/AssignPeerToGroupModal";

// v2 paint of PeerBadge — chip that shows a group's peer count and
// opens AssignPeerToGroupModal on click. Same behavior as the legacy
// PeerBadge, painted with oz2 tokens.

type Props = {
  children?: React.ReactNode;
  group?: Group;
  useSave?: boolean;
  onAssignmentChange?: (group: Group) => void;
  className?: string;
} & React.HTMLAttributes<HTMLDivElement>;

export default function OzPeerBadge({
  children,
  group,
  className,
  useSave = true,
  onAssignmentChange,
}: Props) {
  const [editGroupPeersModal, setEditGroupPeersModal] = useState(false);
  const { dropdownOptions, addDropdownOptions } = useGroups();

  const currentGroup = useMemo(
    () => dropdownOptions?.find((g) => g.name === group?.name),
    [group, dropdownOptions],
  );

  const peerCount = useMemo(() => {
    let count = currentGroup?.peers_count ?? 0;
    const countedPeers = currentGroup?.peers?.length ?? 0;
    if (count !== countedPeers) count = countedPeers;
    return count;
  }, [currentGroup]);

  const updateGroupOptions = (g: Group) => {
    addDropdownOptions([g]);
    onAssignmentChange && onAssignmentChange(g);
  };

  const clickable = !!currentGroup;

  return (
    <>
      {currentGroup && editGroupPeersModal && (
        <AssignPeerToGroupModal
          useSave={useSave}
          group={currentGroup}
          onUpdate={(g) => updateGroupOptions(g)}
          open={editGroupPeersModal}
          setOpen={setEditGroupPeersModal}
        />
      )}
      <div
        onClick={(e) => {
          if (!clickable) return;
          e.stopPropagation();
          setEditGroupPeersModal(true);
        }}
        className={classNames(
          "inline-flex items-center gap-2 whitespace-nowrap rounded-full border px-3 py-[3px]",
          "border-oz2-border bg-oz2-surface text-oz2-text-2 text-[12px] font-medium",
          "transition-colors",
          clickable
            ? "cursor-pointer hover:bg-oz2-hover hover:border-oz2-border-strong"
            : "",
          className,
        )}
      >
        {!currentGroup && <MonitorSmartphoneIcon size={12} />}
        {currentGroup ? <>{peerCount} Peer(s)</> : children}
        {currentGroup &&
          (currentGroup.name === "All" ? (
            <EyeIcon size={12} className="opacity-70" />
          ) : (
            <SquarePen size={12} className="opacity-70" />
          ))}
      </div>
    </>
  );
}
