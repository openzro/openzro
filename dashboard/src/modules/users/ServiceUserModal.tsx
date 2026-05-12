"use client";

import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
  ModalTrigger,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { notify } from "@components/Notification";
import Separator from "@components/Separator";
import { IconSettings2 } from "@tabler/icons-react";
import { useApiCall } from "@utils/api";
import { ExternalLinkIcon, PlusCircle, User2 } from "lucide-react";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import { Role, User } from "@/interfaces/User";
import { UserRoleSelector } from "@/modules/users/UserRoleSelector";

type Props = {
  children: React.ReactNode;
};

export default function ServiceUserModal({ children }: Readonly<Props>) {
  const [modal, setModal] = useState(false);

  return (
    <Modal open={modal} onOpenChange={setModal} key={modal ? 1 : 0}>
      <ModalTrigger asChild>{children}</ModalTrigger>
      <ServiceUserModalContent onSuccess={() => setModal(false)} />
    </Modal>
  );
}

type ModalProps = {
  onSuccess?: () => void;
};

export function ServiceUserModalContent({ onSuccess }: Readonly<ModalProps>) {
  const userRequest = useApiCall<User>("/users");
  const { mutate } = useSWRConfig();
  const [name, setName] = useState("");
  const [role, setRole] = useState("user");

  const create = async () => {
    notify({
      title: "Service user created",
      description: `${name} was successfully created.`,
      promise: userRequest
        .post({
          name,
          role,
          auto_groups: [],
          is_service_user: true,
        })
        .then(() => {
          onSuccess && onSuccess();
          mutate("/users?service_user=true");
        }),
      loadingMessage: "Creating service user...",
    });
  };

  const isDisabled = useMemo(() => {
    return name.length === 0;
  }, [name]);

  return (
    <ModalContent maxWidthClass={"max-w-lg"}>
      <ModalHeader
        icon={<IconSettings2 />}
        title={"Create Service User"}
        description={
          "Service users are non-login users that are not associated with any specific person."
        }
        color={"openzro"}
      />

      <Separator />

      <div className={"px-8 py-6 flex flex-col gap-8"}>
        <div className={"flex gap-4"}>
          <div className={"w-full"}>
            <OzInput
              prefix={<User2 size={16} />}
              placeholder={"John Doe"}
              value={name}
              data-cy={"service-user-name"}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className={"w-[330px]"}>
            <UserRoleSelector
              value={role as Role}
              onChange={setRole}
              hideOwner={true}
            />
          </div>
        </div>
      </div>

      <ModalFooter className={"items-center"}>
        <div className={"w-full"}>
          <p className={"text-sm mt-auto text-oz2-text-muted"}>
            Learn more about{" "}
            <a
              href={"https://docs.openzro.io/how-to/access-openzro-public-api"}
              target={"_blank"}
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Service Users
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
            disabled={isDisabled}
            onClick={create}
            data-cy={"create-service-user"}
          >
            <PlusCircle size={16} />
            Create Service User
          </OzButton>
        </div>
      </ModalFooter>
    </ModalContent>
  );
}
