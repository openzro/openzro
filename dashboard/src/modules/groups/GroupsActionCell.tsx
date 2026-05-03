import Button from "@components/Button";
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

type Props = {
  group: GroupUsage;
  in_use: boolean;
};
export default function GroupsActionCell({ group, in_use }: Readonly<Props>) {
  const { permission } = usePermissions();
  const { confirm } = useDialog();
  const deleteRequest = useApiCall<SetupKey>("/groups/" + group.id);
  const { mutate } = useSWRConfig();
  const [renameModal, setRenameModal] = useState(false);

  const handleRevoke = async () => {
    notify({
      title: "Group: " + group.name,
      description: "Group was successfully deleted.",
      promise: deleteRequest.del().then(() => {
        mutate("/groups");
      }),
      loadingMessage: "Deleting the group...",
    });
  };

  // Build a one-line breakdown of where the group is referenced, e.g.
  // "2 policies · 5 peers · 1 setup key". Used in both the disabled
  // tooltip and (defensively) the delete confirm dialog.
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
    handleRevoke().then();
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
    isAllGroup ||
    in_use ||
    !isRegularGroup ||
    !permission.groups.delete;
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
    if (isJWTGroup) return "This group is issued by JWT and cannot be renamed.";
    if (!isRegularGroup) return "This group is issued by an IdP and cannot be renamed.";
    return "You don't have permission to rename groups.";
  };

  return (
    <div className={"flex justify-end gap-2 pr-4"}>
      {renameModal && (
        <RenameGroupModal
          group={group}
          open={renameModal}
          onOpenChange={setRenameModal}
        />
      )}
      <FullTooltip
        content={
          <div className={"text-xs max-w-xs"}>{getEditDisabledText()}</div>
        }
        interactive={false}
        disabled={!isEditDisabled}
      >
        <Button
          variant={"default-outline"}
          size={"sm"}
          onClick={() => setRenameModal(true)}
          disabled={isEditDisabled}
          data-cy={"rename-group"}
        >
          <PenSquare size={16} />
          Edit
        </Button>
      </FullTooltip>
      <FullTooltip
        content={
          <div className={"text-xs max-w-xs"}>{getDeleteDisabledText()}</div>
        }
        interactive={false}
        disabled={!isDeleteDisabled}
      >
        <Button
          variant={"danger-outline"}
          size={"sm"}
          onClick={handleConfirm}
          disabled={isDeleteDisabled}
        >
          <Trash2 size={16} />
          Delete
        </Button>
      </FullTooltip>
    </div>
  );
}
