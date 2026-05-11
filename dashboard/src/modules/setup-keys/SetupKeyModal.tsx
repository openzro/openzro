import Code from "@components/Code";
import FancyToggleSwitch from "@components/FancyToggleSwitch";
import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
  ModalTrigger,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { notify } from "@components/Notification";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import Separator from "@components/Separator";
import { IconRepeat } from "@tabler/icons-react";
import { useApiCall } from "@utils/api";
import { cn } from "@utils/helpers";
import { trim } from "lodash";
import {
  AlarmClock,
  DownloadIcon,
  ExternalLinkIcon,
  GlobeIcon,
  MonitorSmartphoneIcon,
  PlusCircle,
  PowerOffIcon,
} from "lucide-react";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import SetupKeysIcon from "@/assets/icons/SetupKeysIcon";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import { SetupKey } from "@/interfaces/SetupKey";
import useGroupHelper from "@/modules/groups/useGroupHelper";
import SetupModal from "@/modules/setup-openzro-modal/SetupModal";

type Props = {
  children?: React.ReactNode;
  open: boolean;
  setOpen: (open: boolean) => void;
  name?: string;
  showOnlyRoutingPeerOS?: boolean;
};

const copyMessage = "Setup-Key was copied to your clipboard!";

export default function SetupKeyModal({
  children,
  open,
  setOpen,
  name,
  showOnlyRoutingPeerOS,
}: Readonly<Props>) {
  const [successModal, setSuccessModal] = useState(false);
  const [setupKey, setSetupKey] = useState<SetupKey>();
  const [installModal, setInstallModal] = useState(false);
  const handleSuccess = (setupKey: SetupKey) => {
    setSetupKey(setupKey);
    setSuccessModal(true);
  };

  return (
    <>
      <Modal open={open} onOpenChange={setOpen} key={open ? 1 : 0}>
        {children && <ModalTrigger asChild>{children}</ModalTrigger>}
        <SetupKeyModalContent onSuccess={handleSuccess} predefinedName={name} />
      </Modal>

      <Modal
        open={installModal}
        onOpenChange={(state) => {
          setInstallModal(state);
          setOpen(false);
        }}
        key={installModal ? 2 : 3}
      >
        <SetupModal
          showClose={true}
          setupKey={setupKey?.key}
          showOnlyRoutingPeerOS={showOnlyRoutingPeerOS}
        />
      </Modal>

      <Modal
        open={successModal}
        onOpenChange={(open) => {
          setSuccessModal(open);
          setOpen(open);
        }}
      >
        <ModalContent
          onEscapeKeyDown={(e) => e.preventDefault()}
          onInteractOutside={(e) => e.preventDefault()}
          onPointerDownOutside={(e) => e.preventDefault()}
          maxWidthClass={"max-w-md"}
          className={"mt-20"}
          showClose={false}
        >
          <div className={"pb-6 px-8"}>
            <div className={"flex flex-col items-center justify-center gap-3"}>
              <div>
                <h2 className={"text-2xl text-center mb-2"}>
                  Setup key created successfully!
                </h2>
                <p className={"mt-0 text-sm text-center text-oz2-text-muted"}>
                  This key will not be shown again, so be sure to copy it and
                  store in a secure location.
                </p>
              </div>
            </div>
          </div>

          <div
            className={"px-8 pb-6"}
            data-cy={"setup-key-copy-input"}
            data-cy-setup-key-value={setupKey?.key || ""}
          >
            <Code message={copyMessage}>
              <Code.Line>
                {setupKey?.key || "Setup key could not be created..."}
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
                  data-cy={"setup-key-close"}
                >
                  Close
                </OzButton>
              </ModalClose>
              <OzButton
                variant={"primary"}
                className={"w-full"}
                onClick={() => setInstallModal(true)}
              >
                <DownloadIcon size={14} />
                Install Openzro
              </OzButton>
            </div>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </>
  );
}

type ModalProps = {
  onSuccess?: (setupKey: SetupKey) => void;
  predefinedName?: string;
};

export function SetupKeyModalContent({
  onSuccess,
  predefinedName = "",
}: Readonly<ModalProps>) {
  const setupKeyRequest = useApiCall<SetupKey>("/setup-keys", true);
  const { mutate } = useSWRConfig();

  const [name, setName] = useState(predefinedName);
  const [reusable, setReusable] = useState(false);
  const [usageLimit, setUsageLimit] = useState("");
  const [expiresIn, setExpiresIn] = useState("7");
  const [ephemeralPeers, setEphemeralPeers] = useState(false);
  const [allowExtraDNSLabels, setAllowExtraDNSLabels] = useState(false);

  const [selectedGroups, setSelectedGroups, { save: saveGroups }] =
    useGroupHelper({
      initial: [],
    });

  const usageLimitPlaceholder = useMemo(() => {
    return reusable ? "Unlimited" : "1";
  }, [reusable]);

  const isDisabled = useMemo(() => {
    const trimmedName = trim(name);
    return trimmedName.length === 0;
  }, [name]);

  const submit = () => {
    if (!selectedGroups) return;

    notify({
      title: "Create Setup Key",
      description:
        "Setup key created successfully. You can now enroll peers with your new key.",
      promise: saveGroups().then(async (groups) => {
        return setupKeyRequest
          .post({
            name,
            type: reusable ? "reusable" : "one-off",
            expires_in: parseInt(expiresIn || "0") * 24 * 60 * 60, // Days to seconds, defaults to 7 days
            revoked: false,
            auto_groups: groups.map((group) => group.id),
            usage_limit: reusable ? parseInt(usageLimit) : 1,
            ephemeral: ephemeralPeers,
            allow_extra_dns_labels: allowExtraDNSLabels,
          })
          .then((setupKey) => {
            onSuccess && onSuccess(setupKey);
            mutate("/setup-keys");
            mutate("/groups");
          });
      }),
      loadingMessage: "Creating your setup key...",
    });
  };

  return (
    <ModalContent maxWidthClass={"max-w-xl"}>
      <ModalHeader
        icon={<SetupKeysIcon className={"fill-openzro"} />}
        title={"Create New Setup Key"}
        description={"Use this key to register new machines in your network"}
        color={"openzro"}
      />

      <Separator />

      <div className={"px-8 py-6 flex flex-col gap-8"}>
        <div>
          <OzLabel htmlFor="setup-key-name">Name</OzLabel>
          <OzHelpText className="mb-2">
            Set an easily identifiable name for your key
          </OzHelpText>
          <OzInput
            id="setup-key-name"
            placeholder={"e.g., AWS Servers"}
            value={name}
            data-cy={"setup-key-name"}
            onChange={(e) => setName(e.target.value)}
          />
        </div>

        <div>
          <FancyToggleSwitch
            value={reusable}
            onChange={setReusable}
            label={
              <>
                <IconRepeat size={15} />
                Make this key reusable
              </>
            }
            helpText={"Use this type to enroll multiple peers"}
          />
        </div>

        <div className={cn("flex justify-between", !reusable && "opacity-50")}>
          <div>
            <OzLabel htmlFor="setup-key-usage-limit">Usage limit</OzLabel>
            <OzHelpText className="max-w-[200px]">
              For example, set to 30 if you want to enroll 30 peers
            </OzHelpText>
          </div>

          <OzInput
            id="setup-key-usage-limit"
            min={1}
            wrapperClassName="max-w-[200px]"
            disabled={!reusable}
            value={usageLimit}
            type={"number"}
            data-cy={"setup-key-usage-limit"}
            onChange={(e) => setUsageLimit(e.target.value)}
            placeholder={usageLimitPlaceholder}
            prefix={<MonitorSmartphoneIcon size={16} />}
            suffix={
              <span className="text-[12.5px] text-oz2-text-faint">Peer(s)</span>
            }
          />
        </div>

        <div className={"flex justify-between"}>
          <div>
            <OzLabel htmlFor="setup-key-expire">Expires in</OzLabel>
            <OzHelpText>
              Days until the key expires.
              <br />
              Leave empty for no expiration.
            </OzHelpText>
          </div>
          <OzInput
            id="setup-key-expire"
            wrapperClassName="max-w-[202px]"
            placeholder={"Unlimited"}
            min={1}
            value={expiresIn}
            type={"number"}
            data-cy={"setup-key-expire-in-days"}
            onChange={(e) => setExpiresIn(e.target.value)}
            prefix={<AlarmClock size={16} />}
            suffix={
              <span className="text-[12.5px] text-oz2-text-faint">Day(s)</span>
            }
          />
        </div>

        <div>
          <FancyToggleSwitch
            value={ephemeralPeers}
            onChange={setEphemeralPeers}
            label={
              <>
                <PowerOffIcon size={15} />
                Ephemeral Peers
              </>
            }
            helpText={
              "Peers that are offline for over 10 minutes will be removed automatically"
            }
          />
        </div>

        <div>
          <FancyToggleSwitch
            value={allowExtraDNSLabels}
            onChange={setAllowExtraDNSLabels}
            label={
              <>
                <GlobeIcon size={15} />
                Allow Extra DNS Labels
              </>
            }
            helpText={
              "Enable multiple subdomain labels when enrolling peers (e.g., host.dev.example.com)."
            }
          />
        </div>

        <div>
          <OzLabel>Auto-assigned groups</OzLabel>
          <OzHelpText className="mb-2">
            These groups will be automatically assigned to peers enrolled with
            this key
          </OzHelpText>
          <PeerGroupSelector
            onChange={setSelectedGroups}
            values={selectedGroups}
            hideAllGroup={true}
          />
        </div>
      </div>

      <ModalFooter className={"items-center"}>
        <div className={"w-full"}>
          <p className={"text-sm mt-auto text-oz2-text-muted"}>
            Learn more about{" "}
            <a
              href={
                "https://docs.openzro.io/how-to/register-machines-using-setup-keys"
              }
              target={"_blank"}
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Setup Keys
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
            data-cy={"create-setup-key"}
          >
            <PlusCircle size={16} />
            Create Setup Key
          </OzButton>
        </div>
      </ModalFooter>
    </ModalContent>
  );
}
