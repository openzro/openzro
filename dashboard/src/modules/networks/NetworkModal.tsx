"use client";

import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { notify } from "@components/Notification";
import Separator from "@components/Separator";
import { useApiCall } from "@utils/api";
import { ExternalLinkIcon, PlusCircle } from "lucide-react";
import React, { useState } from "react";
import NetworkRoutesIcon from "@/assets/icons/NetworkRoutesIcon";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import OzTextarea from "@/components/v2/OzTextarea";
import { Network } from "@/interfaces/Network";

type Props = {
  open: boolean;
  setOpen?: (open: boolean) => void;
  network?: Network;
  onCreated?: (network: Network) => void;
  onUpdated?: (network: Network) => void;
};

export default function NetworkModal({
  open,
  setOpen,
  network,
  onCreated,
  onUpdated,
}: Readonly<Props>) {
  return (
    <Modal open={open} onOpenChange={setOpen}>
      <Content
        network={network}
        onCreated={(network) => {
          setOpen?.(false);
          onCreated?.(network);
        }}
        onUpdated={(network) => {
          setOpen?.(false);
          onUpdated?.(network);
        }}
        key={open ? "1" : "0"}
      />
    </Modal>
  );
}

type ContentProps = {
  onCreated?: (network: Network) => void;
  onUpdated?: (network: Network) => void;
  network?: Network;
};

const Content = ({ network, onCreated, onUpdated }: ContentProps) => {
  const [name, setName] = useState(network?.name || "");
  const [description, setDescription] = useState(network?.description || "");
  const create = useApiCall<Network>("/networks").post;
  const update = useApiCall<Network>("/networks").put;

  const updateNetwork = async () => {
    notify({
      title: name,
      description: "Network updated successfully.",
      loadingMessage: "Updating network...",
      promise: update({ name, description }, `/${network?.id}`).then((n) => {
        onUpdated?.(n);
      }),
    });
  };

  const createNetwork = async () => {
    notify({
      title: name,
      description: "Network created successfully.",
      loadingMessage: "Creating network...",
      promise: create({ name, description }).then((n) => {
        onCreated?.(n);
      }),
    });
  };

  return (
    <ModalContent maxWidthClass={"max-w-xl"}>
      <ModalHeader
        icon={<NetworkRoutesIcon className={"fill-openzro"} />}
        title={network ? "Update Network" : "Add Network"}
        description={
          network
            ? network.name
            : "Access internal resources in LANs and VPC by adding a network."
        }
        color={"openzro"}
      />
      <Separator />
      <div className={"px-8 flex flex-col gap-6 py-6"}>
        <div>
          <OzLabel htmlFor="network-name">Network Name</OzLabel>
          <OzHelpText className="mb-2">
            Provide a unique name for the network.
          </OzHelpText>
          <OzInput
            id="network-name"
            tabIndex={0}
            placeholder={"e.g., Office Network"}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <div>
          <OzLabel htmlFor="network-description" optional>
            Description
          </OzLabel>
          <OzHelpText className="mb-2">
            Write a short description to add more context to this network.
          </OzHelpText>
          <OzTextarea
            id="network-description"
            placeholder={"e.g., Production database subnet (HQ datacenter)"}
            value={description}
            rows={3}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>
      </div>

      <ModalFooter className={"items-center"}>
        <div className={"w-full"}>
          <p className={"text-sm mt-auto text-oz2-text-muted"}>
            Learn more about{" "}
            <a
              href={"https://docs.openzro.io/how-to/networks"}
              target={"_blank"}
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Networks
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
            disabled={!name}
            onClick={network ? updateNetwork : createNetwork}
          >
            {network ? (
              "Save Changes"
            ) : (
              <>
                <PlusCircle size={16} />
                Add Network
              </>
            )}
          </OzButton>
        </div>
      </ModalFooter>
    </ModalContent>
  );
};
