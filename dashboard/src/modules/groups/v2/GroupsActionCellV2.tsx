"use client";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@components/DropdownMenu";
import { notify } from "@components/Notification";
import { useApiCall } from "@utils/api";
import { MoreVertical, PencilLine, Trash2 } from "lucide-react";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import { useDialog } from "@/contexts/DialogProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { SetupKey } from "@/interfaces/SetupKey";
import RenameGroupModal from "@/modules/groups/RenameGroupModal";
import { useGroupIdentification } from "@/modules/groups/useGroupIdentification";
import { GroupUsage } from "@/modules/groups/useGroupsUsage";

// GroupsActionCellV2 — v2 kebab dropdown for /team/groups rows.
// Matches the canonical pattern (NameserverActionCellV2,
// SetupKeyActionCellV2): one MoreVertical button opens a menu with
// Edit + Delete. Behavior is preserved verbatim — same permission
// + group-source + in_use gating. Disable-reason text from the old
// FullTooltip is dropped onto the item's `title` attribute as a
// browser hint, which is the dropdown-friendly equivalent.

type Props = {
  group: GroupUsage;
  in_use: boolean;
};

export default function GroupsActionCellV2({
  group,
  in_use,
}: Readonly<Props>) {
  const { permission } = usePermissions();
  const { confirm } = useDialog();
  const deleteRequest = useApiCall<SetupKey>("/groups/" + group.id);
  const { mutate } = useSWRConfig();
  const [renameModal, setRenameModal] = useState(false);

  const handleDelete = async () => {
    notify({
      title: "Group: " + group.name,
      description: "Group was successfully deleted.",
      promise: deleteRequest.del().then(() => {
        mutate("/groups");
      }),
      loadingMessage: "Deleting the group...",
    });
  };

  const usageBreakdown = useMemo(() => {
    const parts: string[] = [];
    const push = (count: number, singular: string, plural: string) => {
      if (count > 0) parts.push(`${count} ${count === 1 ? singular : plural}`);
    };
    push(group.peers_count, "peer", "peers");
    push(group.policies_count, "policy", "policies");
    push(group.routes_count, "route", "routes");
    push(group.setup_keys_count, "setup key", "setup keys");
    push(group.nameservers_count, "nameserver", "nameservers");
    push(group.resources_count, "resource", "resources");
    push(group.users_count, "user", "users");
    return parts.join(" · ");
  }, [group]);

  const handleConfirm = async () => {
    const choice = await confirm({
      title: `Delete '${group.name}'?`,
      description:
        "Are you sure you want to delete this group? This action cannot be undone.",
      confirmText: "Delete",
      cancelText: "Cancel",
      type: "danger",
    });
    if (!choice) return;
    handleDelete().then();
  };

  const { isRegularGroup, isJWTGroup } = useGroupIdentification({
    id: group?.id,
    issued: group?.issued,
  });

  const isAllGroup = group.name === "All";

  const isDeleteDisabled =
    isAllGroup || in_use || !isRegularGroup || !permission.groups.delete;
  const isEditDisabled =
    isAllGroup || !isRegularGroup || !permission.groups.update;

  const deleteDisabledText = useMemo(() => {
    if (!isDeleteDisabled) return undefined;
    if (isAllGroup) {
      return "The All group is a system default and cannot be deleted.";
    }
    if (!isRegularGroup) {
      return isJWTGroup
        ? "This group is issued by JWT and cannot be deleted."
        : "This group is issued by an IdP and cannot be deleted.";
    }
    if (in_use && usageBreakdown) {
      return `In use by ${usageBreakdown}. Remove these references first.`;
    }
    return "Remove dependencies to this group to delete it.";
  }, [
    isDeleteDisabled,
    isAllGroup,
    isRegularGroup,
    isJWTGroup,
    in_use,
    usageBreakdown,
  ]);

  const editDisabledText = useMemo(() => {
    if (!isEditDisabled) return undefined;
    if (isAllGroup) {
      return "The All group is a system default and cannot be renamed.";
    }
    if (isJWTGroup) {
      return "This group is issued by JWT and cannot be renamed.";
    }
    if (!isRegularGroup) {
      return "This group is issued by an IdP and cannot be renamed.";
    }
    return "You don't have permission to rename groups.";
  }, [isEditDisabled, isAllGroup, isJWTGroup, isRegularGroup]);

  return (
    <div className="flex justify-end pr-2" data-stop-row-click>
      {renameModal && (
        <RenameGroupModal
          group={group}
          open={renameModal}
          onOpenChange={setRenameModal}
        />
      )}

      <DropdownMenu modal={false}>
        <DropdownMenuTrigger
          asChild
          onClick={(e) => {
            e.stopPropagation();
            e.preventDefault();
          }}
        >
          <button
            type="button"
            aria-label="Row actions"
            className="grid h-8 w-8 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 transition-colors hover:bg-oz2-hover hover:border-oz2-border-strong"
          >
            <MoreVertical size={14} className="shrink-0" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-44">
          <DropdownMenuItem
            disabled={isEditDisabled}
            title={editDisabledText}
            data-cy="rename-group"
            onClick={(e) => {
              e.stopPropagation();
              setRenameModal(true);
            }}
          >
            <div className="flex items-center gap-3">
              <PencilLine size={14} className="shrink-0" />
              Edit
            </div>
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={isDeleteDisabled}
            title={deleteDisabledText}
            onClick={(e) => {
              e.stopPropagation();
              handleConfirm();
            }}
            variant="danger"
          >
            <div className="flex items-center gap-3">
              <Trash2 size={14} className="shrink-0" />
              Delete
            </div>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
