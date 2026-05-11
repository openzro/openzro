"use client";

import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { notify } from "@components/Notification";
import { FolderGit2Icon, FolderInput } from "lucide-react";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import { useGroups } from "@/contexts/GroupsProvider";
import { GroupUsage } from "@/modules/groups/useGroupsUsage";

type Props = {
  group: GroupUsage;
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

export default function RenameGroupModal({ group, open, onOpenChange }: Props) {
  return (
    <Modal open={open} onOpenChange={onOpenChange} key={open ? 1 : 0}>
      {open && (
        <RenameGroupModalContent
          group={group}
          onSuccess={() => onOpenChange(false)}
        />
      )}
    </Modal>
  );
}

function RenameGroupModalContent({
  group,
  onSuccess,
}: {
  group: GroupUsage;
  onSuccess: () => void;
}) {
  const { createOrUpdate, groups } = useGroups();
  const { mutate } = useSWRConfig();

  const [name, setName] = useState(group.name);

  const duplicateError = useMemo(() => {
    const trimmed = name.trim();
    if (trimmed === "") return "";
    if (trimmed.toLowerCase() === "all")
      return "The name 'All' is reserved.";
    const collision = groups?.some(
      (g) =>
        g.id !== group.id &&
        g.name.toLowerCase() === trimmed.toLowerCase(),
    );
    return collision ? "A group with this name already exists." : "";
  }, [name, groups, group.id]);

  const isUnchanged = name.trim() === group.name;
  const isDisabled =
    name.trim() === "" || duplicateError !== "" || isUnchanged;

  // Look the full Group up so we carry peers/resources through the
  // PUT — `createOrUpdate` issues a full-body update; sending empty
  // arrays would drop every member from the renamed group.
  const fullGroup = useMemo(
    () => groups?.find((g) => g.id === group.id),
    [groups, group.id],
  );

  const handleRename = async () => {
    const trimmed = name.trim();
    notify({
      title: `Group: ${trimmed}`,
      description: "Group was renamed successfully.",
      promise: createOrUpdate({
        id: group.id,
        name: trimmed,
        peers: fullGroup?.peers,
        resources: fullGroup?.resources,
      }).then(() => {
        mutate("/groups");
        onSuccess();
      }),
      loadingMessage: "Renaming the group...",
    });
  };

  return (
    <ModalContent maxWidthClass={"max-w-md"}>
      <ModalHeader
        icon={<FolderGit2Icon size={18} />}
        title={`Rename '${group.name}'`}
        description={
          "Renaming applies everywhere the group is referenced (policies, peers, setup keys, …)."
        }
        color={"openzro"}
      />

      <div className={"px-8 pb-6 pt-2"}>
        <OzLabel htmlFor="rename-group-name">Name</OzLabel>
        <OzHelpText className="mb-2">
          Pick a short, lowercase identifier — e.g. <code>devs</code>,{" "}
          <code>prod-servers</code>.
        </OzHelpText>
        <OzInput
          id="rename-group-name"
          autoFocus
          prefix={<FolderInput size={16} />}
          placeholder={"e.g., devs"}
          value={name}
          data-cy={"group-name"}
          error={duplicateError}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !isDisabled) {
              e.preventDefault();
              handleRename();
            }
          }}
        />
      </div>

      <ModalFooter className={"items-center"}>
        <div className={"w-full"} />
        <div className={"flex gap-3 w-full justify-end"}>
          <ModalClose asChild={true}>
            <OzButton variant={"default"}>Cancel</OzButton>
          </ModalClose>
          <OzButton
            variant={"primary"}
            disabled={isDisabled}
            onClick={handleRename}
            data-cy={"rename-group-submit"}
          >
            Save Changes
          </OzButton>
        </div>
      </ModalFooter>
    </ModalContent>
  );
}
