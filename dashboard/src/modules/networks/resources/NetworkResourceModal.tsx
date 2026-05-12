"use client";

import FancyToggleSwitch from "@components/FancyToggleSwitch";
import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { notify } from "@components/Notification";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import Separator from "@components/Separator";
import { useApiCall } from "@utils/api";
import {
  ExternalLinkIcon,
  PlusCircle,
  Power,
  WorkflowIcon,
} from "lucide-react";
import React, { useMemo, useState } from "react";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import OzTextarea from "@/components/v2/OzTextarea";
import { Network, NetworkResource } from "@/interfaces/Network";
import useGroupHelper from "@/modules/groups/useGroupHelper";
import { ResourceSingleAddressInput } from "@/modules/networks/resources/ResourceSingleAddressInput";

type Props = {
  open?: boolean;
  setOpen?: (open: boolean) => void;
  network: Network;
  resource?: NetworkResource;
  onCreated?: (r: NetworkResource) => void;
  onUpdated?: (r: NetworkResource) => void;
};

export default function NetworkResourceModal({
  network,
  open,
  setOpen,
  resource,
  onUpdated,
  onCreated,
}: Props) {
  return (
    <Modal open={open} onOpenChange={setOpen}>
      <ResourceModalContent
        key={open ? "1" : "0"}
        network={network}
        resource={resource}
        onCreated={onCreated}
        onUpdated={onUpdated}
      />
    </Modal>
  );
}

type ModalProps = {
  onCreated?: (r: NetworkResource) => void;
  onUpdated?: (r: NetworkResource) => void;
  network: Network;
  resource?: NetworkResource;
};

export function ResourceModalContent({
  onCreated,
  onUpdated,
  network,
  resource,
}: ModalProps) {
  const create = useApiCall<NetworkResource>(
    `/networks/${network.id}/resources`,
  ).post;
  const update = useApiCall<NetworkResource>(
    `/networks/${network.id}/resources/${resource?.id}`,
  ).put;

  const [name, setName] = useState(resource?.name || "");
  const [description, setDescription] = useState(resource?.description || "");
  const [address, setAddress] = useState(resource?.address || "");
  const [addressError, setAddressError] = useState("");
  const [groups, setGroups, { save: saveGroups }] = useGroupHelper({
    initial: resource?.groups || [],
  });
  const [enabled, setEnabled] = useState<boolean>(
    resource ? resource.enabled : true,
  );

  const createResource = async () => {
    const savedGroups = await saveGroups();
    notify({
      title: "Resource Created",
      description: `The resource "${name}" has been created successfully.`,
      loadingMessage: "Creating resource...",
      promise: create({
        name,
        description,
        address,
        groups: savedGroups.map((g) => g.id),
        enabled,
      }).then((r) => {
        onCreated?.(r);
      }),
    });
  };

  const updateResource = async () => {
    const savedGroups = await saveGroups();
    notify({
      title: "Resource Updated",
      description: `The resource "${name}" has been updated successfully.`,
      loadingMessage: "Updating resource...",
      promise: update({
        name,
        description,
        address,
        groups: savedGroups.map((g) => g.id),
        enabled,
      }).then((r) => {
        onUpdated?.(r);
      }),
    });
  };

  const canCreate = useMemo(() => {
    return (
      name.length > 0 &&
      address.length > 0 &&
      addressError === "" &&
      groups.length > 0
    );
  }, [name, address, addressError, groups]);

  return (
    <ModalContent maxWidthClass={"max-w-xl"}>
      <ModalHeader
        icon={<WorkflowIcon size={20} />}
        title={resource ? "Edit Resource" : "Add Resource"}
        description={
          resource
            ? `${resource.name}`
            : `Add new resource to "${network?.name}"`
        }
        color={"yellow"}
      />

      <Separator />

      <div className={"px-8 flex flex-col gap-6 py-6"}>
        <div>
          <OzLabel htmlFor="resource-name">Name</OzLabel>
          <OzHelpText className="mb-2">
            Provide a name for your resource
          </OzHelpText>
          <OzInput
            id="resource-name"
            tabIndex={0}
            placeholder={"e.g., Postgres Database"}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <div>
          <OzLabel htmlFor="resource-description" optional>
            Description
          </OzLabel>
          <OzHelpText className="mb-2">
            Write a short description to add more context to this resource.
          </OzHelpText>
          <OzTextarea
            id="resource-description"
            placeholder={"e.g., Production, Development"}
            value={description}
            rows={1}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>

        <ResourceSingleAddressInput
          value={address}
          onChange={setAddress}
          onError={setAddressError}
        />

        <div>
          <OzLabel>Destination Groups</OzLabel>
          <OzHelpText className="mb-2">
            Add this resource to groups and use them as destinations when
            creating policies
          </OzHelpText>
          <PeerGroupSelector onChange={setGroups} values={groups} />
        </div>
        <div className={"mt-3"}>
          <FancyToggleSwitch
            value={enabled}
            onChange={setEnabled}
            label={
              <>
                <Power size={15} />
                Enable Resource
              </>
            }
            helpText={"Use this switch to enable or disable the resource."}
          />
        </div>
      </div>

      <ModalFooter className={"items-center"}>
        <div className={"w-full"}>
          <p className={"text-sm mt-auto text-oz2-text-muted"}>
            Learn more about{" "}
            <a
              href={"https://docs.openzro.io/how-to/networks#resources"}
              target={"_blank"}
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Resources
              <ExternalLinkIcon size={12} />
            </a>
          </p>
        </div>
        <div className={"flex gap-3 w-full justify-end"}>
          <ModalClose asChild={true}>
            <OzButton variant={"default"}>Cancel</OzButton>
          </ModalClose>

          <OzButton
            variant={"primary"}
            data-cy={"submit-route"}
            onClick={resource ? updateResource : createResource}
            disabled={!canCreate}
          >
            {resource ? (
              <>Save Changes</>
            ) : (
              <>
                <PlusCircle size={16} />
                Add Resource
              </>
            )}
          </OzButton>
        </div>
      </ModalFooter>
    </ModalContent>
  );
}
