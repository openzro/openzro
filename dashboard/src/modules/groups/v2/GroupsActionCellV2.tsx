import FullTooltip from "@components/FullTooltip";
import { notify } from "@components/Notification";
import { useApiCall } from "@utils/api";
import { PenSquare, Trash2 } from "lucide-react";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import { useDialog } from "@/contexts/DialogProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { SetupKey } from "@/interfaces/SetupKey";
import RenameGroupModal from "@/modules/groups/RenameGroupModal";
import { useGroupIdentification } from "@/modules/groups/useGroupIdentification";
import { GroupUsage } from "@/modules/groups/useGroupsUsage";

// GroupsActionCellV2 — v2 paint for the row-end Edit + Delete buttons
// on /team/groups. Behavior is preserved verbatim from
// GroupsActionCell:
//
//   - Edit opens RenameGroupModal (gated on permission.groups.update,
//     blocked for the system "All" group, JWT-issued groups, and
//     SCIM-issued groups via useGroupIdentification).
//   - Delete fires the confirm dialog and DELETE /groups/:id
//     (gated on permission.groups.delete + same group-source rules
//     + the in_use flag derived from any non-zero usage count).
//   - Each disabled state surfaces its specific reason via the same
//     FullTooltip pattern; the message strings stay verbatim.
//
// Visual pass: 28px row buttons with v2 tokens. Edit is a neutral
// outline; Delete uses the same hover-red pattern UserActionCellV2
// landed on (neutral border + red text at rest, soft-red fill +
// red border on hover). Tokens-only — no alpha modifiers.

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

  // One-line breakdown of where the group is referenced, e.g.
  // "2 policies · 5 peers · 1 setup key" — surfaces inside the
  // disabled tooltip when the group is in_use.
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

  // The "All" group is a system default — every peer is implicitly
  // a member, and the bootstrapped "Allow Mesh Traffic" policy
  // references it by name. The provider silently no-ops PUTs to it,
  // so allowing the buttons to fire produces a "click Save, nothing
  // happens" UX. Block both Edit and Delete with an explicit tooltip.
  const isAllGroup = group.name === "All";

  const isDeleteDisabled =
    isAllGroup || in_use || !isRegularGroup || !permission.groups.delete;
  const isEditDisabled =
    isAllGroup || !isRegularGroup || !permission.groups.update;

  const getDeleteDisabledText = () => {
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
  };

  const getEditDisabledText = () => {
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
  };

  return (
    <div
      className="flex items-center justify-end gap-2 pr-3"
      data-stop-row-click
    >
      {renameModal && (
        <RenameGroupModal
          group={group}
          open={renameModal}
          onOpenChange={setRenameModal}
        />
      )}

      <FullTooltip
        content={<div className="max-w-xs text-xs">{getEditDisabledText()}</div>}
        interactive={false}
        disabled={!isEditDisabled}
      >
        <button
          type="button"
          onClick={() => setRenameModal(true)}
          disabled={isEditDisabled}
          data-cy="rename-group"
          aria-label={`Rename ${group.name}`}
          className="inline-flex h-7 items-center gap-1.5 whitespace-nowrap rounded-[8px] border border-oz2-border bg-transparent px-2.5 text-[12.5px] font-medium text-oz2-text-2 transition-colors hover:bg-oz2-hover hover:border-oz2-border-strong disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent disabled:hover:border-oz2-border"
        >
          <PenSquare size={13} />
          Edit
        </button>
      </FullTooltip>

      <FullTooltip
        content={
          <div className="max-w-xs text-xs">{getDeleteDisabledText()}</div>
        }
        interactive={false}
        disabled={!isDeleteDisabled}
      >
        <button
          type="button"
          onClick={handleConfirm}
          disabled={isDeleteDisabled}
          aria-label={`Delete ${group.name}`}
          className="inline-flex h-7 items-center gap-1.5 whitespace-nowrap rounded-[8px] border border-oz2-border bg-transparent px-2.5 text-[12.5px] font-medium text-oz2-err transition-colors hover:border-oz2-err hover:bg-oz2-err-bg disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:border-oz2-border disabled:hover:bg-transparent"
        >
          <Trash2 size={13} />
          Delete
        </button>
      </FullTooltip>
    </div>
  );
}
