"use client";

import Code from "@components/Code";
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
import { IconApi } from "@tabler/icons-react";
import { useApiCall } from "@utils/api";
import { trim } from "lodash";
import {
  AlarmClock,
  CopyIcon,
  ExternalLinkIcon,
  PlusCircle,
} from "lucide-react";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import useCopyToClipboard from "@/hooks/useCopyToClipboard";
import { AccessToken } from "@/interfaces/AccessToken";
import { User } from "@/interfaces/User";

type Props = {
  children: React.ReactNode;
  user: User;
};
const copyMessage = "Access token was copied to your clipboard!";
export default function CreateAccessTokenModal({
  children,
  user,
}: Readonly<Props>) {
  const [modal, setModal] = useState(false);
  const [successModal, setSuccessModal] = useState(false);
  const [token, setToken] = useState<string>("");
  const [, copy] = useCopyToClipboard(token);

  return (
    <>
      <Modal open={modal} onOpenChange={setModal} key={modal ? 1 : 0}>
        <ModalTrigger asChild>{children}</ModalTrigger>
        <AccessTokenModalContent
          onSuccess={(token) => {
            setToken(token);
            setSuccessModal(true);
          }}
          user={user}
        />
      </Modal>
      <Modal
        open={successModal}
        onOpenChange={(open) => {
          setSuccessModal(open);
          setModal(open);
        }}
      >
        <ModalContent
          maxWidthClass={"max-w-lg"}
          className={"mt-20"}
          showClose={false}
          onEscapeKeyDown={(e) => e.preventDefault()}
          onInteractOutside={(e) => e.preventDefault()}
          onPointerDownOutside={(e) => e.preventDefault()}
        >
          <div className={"pb-6 px-8"}>
            <div className={"flex flex-col items-center justify-center gap-3"}>
              <div>
                <h2 className={"text-2xl text-center mb-2"}>
                  Access token created successfully!
                </h2>
                <p className={"mt-0 text-sm text-center text-oz2-text-muted"}>
                  This token will not be shown again, so be sure to copy it and
                  store in a secure location.
                </p>
              </div>
            </div>
          </div>

          <div className={"px-8 pb-6"}>
            <Code message={copyMessage}>
              <Code.Line>
                {token || "Setup key could not be created..."}
              </Code.Line>
            </Code>
          </div>
          <ModalFooter className={"items-center"}>
            <div className={"flex gap-3 w-full"}>
              <ModalClose asChild={true}>
                <OzButton
                  variant={"default"}
                  className={"w-full"}
                  tabIndex={-1}
                  data-cy={"access-token-copy-close"}
                >
                  Close
                </OzButton>
              </ModalClose>

              <OzButton
                variant={"primary"}
                className={"w-full"}
                onClick={() => copy(copyMessage)}
              >
                <CopyIcon size={14} />
                Copy to clipboard
              </OzButton>
            </div>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </>
  );
}

type ModalProps = {
  onSuccess?: (token: string) => void;
  user: User;
};

export function AccessTokenModalContent({
  onSuccess,
  user,
}: Readonly<ModalProps>) {
  const tokenRequest = useApiCall<AccessToken>(`/users/${user.id}/tokens`);
  const { mutate } = useSWRConfig();

  const [name, setName] = useState("");
  const [expiresIn, setExpiresIn] = useState("30");

  const isDisabled = useMemo(() => {
    const trimmedName = trim(name);
    return trimmedName.length === 0;
  }, [name]);

  const submit = () => {
    const expiration = parseInt(expiresIn);
    notify({
      title: "Creating access token",
      description: name + " was created successfully",
      promise: tokenRequest
        .post({
          name,
          expires_in: expiration != 0 ? expiration : 30,
        })
        .then((res) => {
          onSuccess && onSuccess(res.plain_token as string);
          mutate(`/users/${user.id}/tokens`);
        }),
      loadingMessage: "Creating access token...",
    });
  };

  return (
    <ModalContent maxWidthClass={"max-w-lg"}>
      <ModalHeader
        icon={<IconApi />}
        title={"Create Access Token"}
        description={"Use this token to access Openzro's public API"}
        color={"openzro"}
      />

      <Separator />

      <div className={"px-8 py-6 flex flex-col gap-8"}>
        <div>
          <OzLabel htmlFor="access-token-name">Name</OzLabel>
          <OzHelpText className="mb-2">
            Set an easily identifiable name for your token
          </OzHelpText>
          <OzInput
            id="access-token-name"
            data-cy={"access-token-name"}
            placeholder={"e.g., Infra token"}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>

        <div className={"flex justify-between"}>
          <div>
            <OzLabel htmlFor="access-token-expires-in">Expires in</OzLabel>
            <OzHelpText>Should be between 1 and 365 days.</OzHelpText>
          </div>
          <OzInput
            id="access-token-expires-in"
            wrapperClassName="max-w-[200px]"
            placeholder={"30"}
            data-cy={"access-token-expires-in"}
            min={1}
            max={365}
            value={expiresIn}
            type={"number"}
            onChange={(e) => setExpiresIn(e.target.value)}
            prefix={<AlarmClock size={16} />}
            suffix={
              <span className="text-[12.5px] text-oz2-text-faint">Day(s)</span>
            }
          />
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
              Access Tokens
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
            onClick={submit}
            disabled={isDisabled}
            data-cy={"create-access-token"}
          >
            <PlusCircle size={16} />
            Create Token
          </OzButton>
        </div>
      </ModalFooter>
    </ModalContent>
  );
}
