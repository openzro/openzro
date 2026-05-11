import {
  Modal,
  ModalContent,
  ModalFooter,
  ModalTrigger,
} from "@components/modal/Modal";
import { notify } from "@components/Notification";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import { IconMailForward } from "@tabler/icons-react";
import { useApiCall } from "@utils/api";
import { cn, validator } from "@utils/helpers";
import { MailIcon, User2 } from "lucide-react";
import Image from "next/image";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import Avatar1 from "@/assets/avatars/009.jpg";
import Avatar2 from "@/assets/avatars/030.jpg";
import Avatar3 from "@/assets/avatars/063.jpg";
import Avatar4 from "@/assets/avatars/086.jpg";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import { Role, User } from "@/interfaces/User";
import useGroupHelper from "@/modules/groups/useGroupHelper";
import { UserRoleSelector } from "@/modules/users/UserRoleSelector";

type Props = {
  children: React.ReactNode;
};

export default function UserInviteModal({ children }: Readonly<Props>) {
  const [open, setOpen] = useState(false);
  const { mutate } = useSWRConfig();

  const handleOnSuccess = () => {
    setOpen(false);
    setTimeout(() => {
      mutate("/users?service_user=false");
    }, 1000);
  };

  return (
    <Modal open={open} onOpenChange={setOpen} key={open ? 1 : 0}>
      <ModalTrigger asChild={true}>{children}</ModalTrigger>
      <UserInviteModalContent onSuccess={handleOnSuccess} />
    </Modal>
  );
}

type ModalProps = {
  onSuccess: () => void;
};

export function UserInviteModalContent({ onSuccess }: Readonly<ModalProps>) {
  const userRequest = useApiCall<User>("/users");
  const { mutate } = useSWRConfig();

  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [role, setRole] = useState("user");
  const [selectedGroups, setSelectedGroups, { save: saveGroups }] =
    useGroupHelper({
      initial: [],
    });

  const sendInvite = async () => {
    const groups = await saveGroups();
    const groupIds = groups.map((group) => group.id) as string[];
    notify({
      title: "User Invitation",
      description: `${name} was invited to join your network.`,
      promise: userRequest
        .post({
          name,
          email,
          role,
          auto_groups: groupIds,
          is_service_user: false,
        })
        .then(() => {
          mutate("/users?service_user=false");
          onSuccess && onSuccess();
        }),
      loadingMessage: "Sending invite...",
    });
  };
  const isValidEmail = useMemo(() => {
    return email.length > 0 && validator.isValidEmail(email);
  }, [email]);

  const isDisabled = useMemo(() => {
    return name.length === 0 || !isValidEmail;
  }, [name, isValidEmail]);

  return (
    <ModalContent maxWidthClass={"max-w-md relative"} showClose={true}>
      <div
        className={
          "h-full w-full absolute left-0 top-0 rounded-md overflow-hidden z-0"
        }
      >
        <div
          className={
            "bg-gradient-to-b from-nb-gray-900/20 via-transparent to-transparent w-full h-full rounded-md"
          }
        ></div>
      </div>
      <UserAvatars />

      <div
        className={
          "mx-auto text-center flex flex-col items-center justify-center mt-6"
        }
      >
        <h2 className={"text-lg my-0 leading-[1.5 text-center]"}>
          Invite User
        </h2>
        <p className={cn("text-sm text-center max-w-xs text-oz2-text-muted")}>
          Invite a user to your network and set their permissions.
        </p>
      </div>

      <div className={"px-8 py-3 flex flex-col gap-6 mt-4"}>
        <div className={"flex flex-col gap-4"}>
          <OzInput
            prefix={<User2 size={16} />}
            placeholder={"John Doe"}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <OzInput
            type={"email"}
            prefix={<MailIcon size={16} />}
            placeholder={"hello@openzro.io"}
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
          <UserRoleSelector
            value={role as Role}
            onChange={setRole}
            hideOwner={true}
          />
        </div>

        <div className={"mb-4"}>
          <OzLabel>Auto-assigned groups</OzLabel>
          <OzHelpText className="mb-2">
            Groups will be assigned to peers added by this user.
          </OzHelpText>
          <PeerGroupSelector
            onChange={setSelectedGroups}
            values={selectedGroups}
            hideAllGroup={true}
          />
        </div>
      </div>

      <ModalFooter className={"items-center"}>
        <OzButton
          variant={"primary"}
          className={"w-full"}
          disabled={isDisabled}
          onClick={sendInvite}
        >
          Send Invitation
          <IconMailForward size={16} />
        </OzButton>
      </ModalFooter>
    </ModalContent>
  );
}

function UserAvatars() {
  return (
    <div className={"flex items-center justify-center relative"}>
      <div
        className={
          "flex items-center justify-center absolute left-0 top-0 w-full h-full -z-10"
        }
      >
        <div
          className={
            "w-10 h-10 shrink-0 bg-openzro/20 rounded-full inline-flex animate-ping duration-3000"
          }
        />
      </div>
      <div
        className={
          "w-14 h-14 relative top-2 overflow-hidden -right-8 bg-nb-gray-950 rounded-full flex items-center justify-center border-4 border-nb-gray-950 outline-2 outline-openzro"
        }
      >
        <Image src={Avatar1} alt={"MS"} />
      </div>
      <div
        className={
          "w-14 h-14 relative top-1 overflow-hidden -right-4 bg-nb-gray-950 rounded-full flex items-center justify-center border-4 border-nb-gray-950 outline-2 outline-openzro"
        }
      >
        <Image src={Avatar2} alt={"MS"} />
      </div>

      <div
        className={
          "w-14 h-14 z-20 relative overflow-hidden bg-nb-gray-930 rounded-full flex items-center justify-center border-4 border-nb-gray-950"
        }
      >
        <User2 size={24} className={"text-openzro"} />
      </div>
      <div
        className={
          "w-14 h-14 relative overflow-hidden z-10 top-1 -left-4 bg-nb-gray-950 rounded-full flex items-center justify-center border-4 border-nb-gray-950"
        }
      >
        <Image src={Avatar3} alt={"MS"} />
      </div>
      <div
        className={
          "w-14 h-14 relative overflow-hidden z-0 top-2 -left-8 bg-nb-gray-950 rounded-full flex items-center justify-center border-4 border-nb-gray-950"
        }
      >
        <Image src={Avatar4} alt={"MS"} />
      </div>
    </div>
  );
}
