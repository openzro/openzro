"use client";

import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { notify } from "@components/Notification";
import { FolderGit2Icon, FolderPlus } from "lucide-react";
import React, { useMemo, useRef, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import { useGroups } from "@/contexts/GroupsProvider";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

export default function CreateGroupModal({ open, onOpenChange }: Props) {
  return (
    <Modal open={open} onOpenChange={onOpenChange} key={open ? 1 : 0}>
      {open && (
        <CreateGroupModalContent onSuccess={() => onOpenChange(false)} />
      )}
    </Modal>
  );
}

function CreateGroupModalContent({ onSuccess }: { onSuccess: () => void }) {
  const { createOrUpdate, groups } = useGroups();
  const { mutate } = useSWRConfig();

  const [name, setName] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  // Names are normalized server-side, but match upstream's case-
  // sensitive comparison so we surface the duplicate before the
  // request rather than after a 4xx round-trip.
  const duplicateError = useMemo(() => {
    const trimmed = name.trim();
    if (trimmed === "") return "";
    if (trimmed.toLowerCase() === "all")
      return "The name 'All' is reserved.";
    const exists = groups?.some(
      (g) => g.name.toLowerCase() === trimmed.toLowerCase(),
    );
    return exists ? "A group with this name already exists." : "";
  }, [name, groups]);

  const isDisabled = name.trim() === "" || duplicateError !== "";

  const handleCreate = async () => {
    const trimmed = name.trim();
    notify({
      title: "Group: " + trimmed,
      description: "Group was created successfully.",
      promise: createOrUpdate({
        name: trimmed,
        peers: [],
        resources: [],
      }).then(() => {
        mutate("/groups");
        onSuccess();
      }),
      loadingMessage: "Creating the group...",
    });
  };

  return (
    <ModalContent maxWidthClass={"max-w-md"}>
      <ModalHeader
        icon={<FolderGit2Icon size={18} />}
        title={"New Group"}
        description={
          "Groups bundle peers, resources and users so policies can target them by name."
        }
        color={"openzro"}
      />

      <div className={"px-8 pb-6 pt-2"}>
        <OzLabel htmlFor="create-group-name">Name</OzLabel>
        <OzHelpText className="mb-2">
          Pick a short, lowercase identifier — e.g. <code>devs</code>,{" "}
          <code>prod-servers</code>.
        </OzHelpText>
        <OzInput
          id="create-group-name"
          ref={inputRef}
          autoFocus
          prefix={<FolderPlus size={16} />}
          placeholder={"e.g., devs"}
          value={name}
          data-cy={"group-name"}
          error={duplicateError}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !isDisabled) {
              e.preventDefault();
              handleCreate();
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
            onClick={handleCreate}
            data-cy={"create-group-submit"}
          >
            Create Group
          </OzButton>
        </div>
      </ModalFooter>
    </ModalContent>
  );
}
