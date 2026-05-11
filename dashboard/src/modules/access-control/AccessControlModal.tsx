"use client";

import { Callout } from "@components/Callout";
import FancyToggleSwitch from "@components/FancyToggleSwitch";
import {
  ModalClose,
  ModalContent,
  ModalFooter,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import { PortSelector } from "@components/PortSelector";
import PolicyDirection from "@components/ui/PolicyDirection";
import { cn } from "@utils/helpers";
import {
  AlertCircleIcon,
  ArrowRightLeft,
  ExternalLinkIcon,
  FolderDown,
  FolderInput,
  PlusCircle,
  Power,
  Share2,
  Shield,
  Text,
} from "lucide-react";
import React, { useMemo, useState } from "react";
// useState is still used inside AccessControlModalContent (tab + form state).
import AccessControlIcon from "@/assets/icons/AccessControlIcon";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import {
  OzSelect,
  OzSelectContent,
  OzSelectItem,
  OzSelectTrigger,
  OzSelectValue,
} from "@/components/v2/OzSelect";
import {
  OzTabs,
  OzTabsContent,
  OzTabsList,
  OzTabsTrigger,
} from "@/components/v2/OzTabs";
import OzTextarea from "@/components/v2/OzTextarea";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Group } from "@/interfaces/Group";
import { Policy, Protocol } from "@/interfaces/Policy";
import { PostureCheck } from "@/interfaces/PostureCheck";
import { useAccessControl } from "@/modules/access-control/useAccessControl";
import { PostureCheckTab } from "@/modules/posture-checks/ui/PostureCheckTab";
import { PostureCheckTabTrigger } from "@/modules/posture-checks/ui/PostureCheckTabTrigger";

// Only AccessControlModalContent is exported now. The standalone
// AccessControlModal + AccessControlUpdateModal wrappers were used by
// the legacy /access-control table — both are gone. Networks' multi-
// step wizard (NetworkProvider) still composes this body inline
// inside its own Modal so the wizard's post-policy "ask for routing
// peer" continuation stays in the same modal stack instead of
// navigating away to /access-control/new.

type ModalProps = {
  onSuccess?: (p: Policy) => void;
  policy?: Policy;
  initialDestinationGroups?: Group[] | string[];
  initialName?: string;
  initialDescription?: string;
  cell?: string;
  postureCheckTemplates?: PostureCheck[];
  useSave?: boolean;
  allowEditPeers?: boolean;
};

export function AccessControlModalContent({
  onSuccess,
  policy,
  cell,
  postureCheckTemplates,
  useSave = true,
  allowEditPeers = false,
  initialDestinationGroups,
  initialName,
  initialDescription,
}: Readonly<ModalProps>) {
  const { permission } = usePermissions();

  const {
    portDisabled,
    destinationGroups,
    direction,
    ports,
    sourceGroups,
    destinationHasResources,
    destinationOnlyResources,
    setSourceGroups,
    setDestinationGroups,
    setPorts,
    setDirection,
    setProtocol,
    enabled,
    setEnabled,
    setName,
    setDescription,
    setPostureChecks,
    name,
    protocol,
    description,
    postureChecks,
    submit,
    isPostureChecksLoading,
    getPolicyData,
    destinationResource,
    setDestinationResource,
    portRanges,
    setPortRanges,
    hasPortSupport,
  } = useAccessControl({
    policy,
    postureCheckTemplates,
    onSuccess,
    initialDestinationGroups,
    initialName,
    initialDescription,
  });

  const [tab, setTab] = useState(() => {
    if (!cell) return "policy";
    if (cell == "posture_checks") return "posture_checks";
    return "policy";
  });

  const continuePostureChecksDisabled = useMemo(() => {
    if (sourceGroups.length > 0 && destinationResource) return false;
    if (sourceGroups.length == 0 || destinationGroups.length == 0) return true;
  }, [sourceGroups, destinationGroups, destinationResource]);

  const submitDisabled = useMemo(() => {
    if (name.length == 0) return true;
    if (continuePostureChecksDisabled) return true;
  }, [name, continuePostureChecksDisabled]);

  const handleProtocolChange = (p: Protocol) => {
    setProtocol(p);
    if (!hasPortSupport(p)) {
      setPorts([]);
      setPortRanges([]);
    }
  };

  const close = () => {
    const data = getPolicyData();
    onSuccess && onSuccess(data);
  };

  return (
    <ModalContent maxWidthClass={"max-w-3xl"}>
      <ModalHeader
        icon={<AccessControlIcon className={"fill-openzro"} />}
        title={
          policy
            ? "Update Access Control Policy"
            : "Create New Access Control Policy"
        }
        description={
          "Use this policy to restrict access to groups of resources."
        }
        color={"openzro"}
      />

      <OzTabs defaultValue={tab} onValueChange={(v) => setTab(v)} value={tab}>
        <div className="px-8">
          <OzTabsList>
            <OzTabsTrigger value={"policy"}>
              <ArrowRightLeft size={16} />
              Policy
            </OzTabsTrigger>
            <PostureCheckTabTrigger disabled={continuePostureChecksDisabled} />
            <OzTabsTrigger
              value={"general"}
              disabled={continuePostureChecksDisabled}
            >
              <Text size={16} />
              Name & Description
            </OzTabsTrigger>
          </OzTabsList>
        </div>

        <OzTabsContent value={"policy"} className={"pb-8 pt-3"}>
          <div className={"px-8 flex-col flex gap-6"}>
            <div
              className={"flex justify-between items-center"}
              data-cy={"protocol-wrapper"}
            >
              <div>
                <OzLabel>Protocol</OzLabel>
                <OzHelpText className="max-w-sm mt-1">
                  Allow only specified network protocols. To change traffic
                  direction and ports, select{" "}
                  <b className={"text-oz2-text"}>TCP</b> or{" "}
                  <b className={"text-oz2-text"}>UDP</b> protocol.
                </OzHelpText>
              </div>
              <div className="w-[110px] shrink-0">
                <OzSelect
                  value={protocol}
                  onValueChange={(v) => handleProtocolChange(v as Protocol)}
                  disabled={
                    !permission.policies.update || !permission.policies.create
                  }
                >
                  <OzSelectTrigger>
                    <div
                      className={"flex items-center gap-2"}
                      data-cy={"protocol-select-button"}
                    >
                      <Share2 size={14} className={"text-oz2-text-faint"} />
                      <OzSelectValue placeholder="Protocol" />
                    </div>
                  </OzSelectTrigger>
                  <OzSelectContent data-cy={"protocol-selection"}>
                    <OzSelectItem value="all">ALL</OzSelectItem>
                    <OzSelectItem value="tcp">TCP</OzSelectItem>
                    <OzSelectItem value="udp">UDP</OzSelectItem>
                    <OzSelectItem value="icmp">ICMP</OzSelectItem>
                  </OzSelectContent>
                </OzSelect>
              </div>
            </div>

            <div className={"flex gap-6 items-center"}>
              <div className={"w-full self-start"}>
                <OzLabel className={"mb-2"}>
                  <FolderDown size={15} />
                  Source
                </OzLabel>
                <PeerGroupSelector
                  dataCy={"source-group-selector"}
                  showPeerCount={allowEditPeers}
                  disableInlineRemoveGroup={false}
                  popoverWidth={500}
                  showRoutes={false}
                  onChange={setSourceGroups}
                  values={sourceGroups}
                  saveGroupAssignments={useSave}
                  showResourceCounter={false}
                  disabled={
                    !permission.policies.update || !permission.policies.create
                  }
                />
              </div>
              <PolicyDirection
                value={direction}
                onChange={setDirection}
                disabled={destinationOnlyResources}
                destinationResource={destinationResource}
              />

              <div className={"w-full self-start"}>
                <OzLabel className={"mb-2"}>
                  <FolderInput size={15} />
                  Destination
                </OzLabel>
                <PeerGroupSelector
                  dataCy={"destination-group-selector"}
                  showRoutes={true}
                  showPeerCount={allowEditPeers}
                  disableInlineRemoveGroup={false}
                  popoverWidth={500}
                  onChange={setDestinationGroups}
                  values={destinationGroups}
                  saveGroupAssignments={useSave}
                  resource={destinationResource}
                  onResourceChange={setDestinationResource}
                  showResources={true}
                  placeholder={"Select destination(s)..."}
                  disabled={
                    !permission.policies.update || !permission.policies.create
                  }
                />
              </div>
            </div>

            {destinationHasResources &&
              !destinationOnlyResources &&
              direction === "bi" && (
                <Callout
                  variant={"warning"}
                  icon={
                    <AlertCircleIcon
                      size={14}
                      className={"shrink-0 relative top-[3px] text-oz2-acc"}
                    />
                  }
                  className="mb-4"
                >
                  Some destination groups contain resources. Resources only
                  support incoming traffic and cannot initiate connections.
                </Callout>
              )}

            {protocol === "all" && direction !== "bi" && (
              <Callout
                variant={"warning"}
                icon={
                  <AlertCircleIcon
                    size={14}
                    className={"shrink-0 relative top-[3px] text-oz2-acc"}
                  />
                }
                className="mb-4"
                data-cy={"unidirectional-all-warning"}
              >
                Unidirectional ALL is experimental. Reply traffic relies on
                the firewall&apos;s stateful conntrack — fine for
                request/response protocols (HTTP, SSH, DNS), but apps that
                push unsolicited messages from the destination back to the
                source (SNMP traps, syslog UDP outbound, server-initiated
                heartbeats) will be dropped. Operationally-asymmetric ICMP
                (destination-unreachable, fragmentation-needed) is dropped
                too. Keep ALL bidirectional or split into per-protocol
                rules if your apps depend on those.{" "}
                <a
                  className="underline"
                  href="https://github.com/openzro/openzro/blob/main/docs/operator/unidirectional-policies.md"
                  target="_blank"
                  rel="noreferrer noopener"
                >
                  Operator guide
                </a>
                .
              </Callout>
            )}

            <div
              className={cn(
                "mb-2",
                portDisabled && "opacity-30 pointer-events-none",
              )}
            >
              <div>
                <OzLabel className={"flex items-center gap-2"}>
                  <Shield size={14} />
                  Ports
                </OzLabel>
                <OzHelpText className="mt-1">
                  Allow network traffic and access only to specified ports.
                  Select ports or port ranges between 1 and 65535.
                </OzHelpText>
              </div>
              <div>
                <PortSelector
                  showAll={true}
                  ports={ports}
                  onPortsChange={setPorts}
                  portRanges={portRanges}
                  onPortRangesChange={setPortRanges}
                  disabled={portDisabled}
                />
              </div>
            </div>

            <FancyToggleSwitch
              value={enabled}
              onChange={setEnabled}
              disabled={
                !permission.policies.update || !permission.policies.create
              }
              label={
                <>
                  <Power size={15} />
                  Enable Policy
                </>
              }
              helpText={"Use this switch to enable or disable the policy."}
            />
          </div>
        </OzTabsContent>
        <PostureCheckTab
          isLoading={isPostureChecksLoading}
          postureChecks={postureChecks}
          setPostureChecks={setPostureChecks}
        />
        <OzTabsContent value={"general"} className={"px-8 pb-6 pt-3"}>
          <div className={"flex flex-col gap-6"}>
            <div>
              <OzLabel htmlFor="policy-name">Name of the Rule</OzLabel>
              <OzHelpText className="mb-2">
                Set an easily identifiable name for your policy.
              </OzHelpText>
              <OzInput
                id="policy-name"
                autoFocus={true}
                tabIndex={0}
                value={name}
                data-cy={"policy-name"}
                onChange={(e) => setName(e.target.value)}
                placeholder={"e.g., Devs to Servers"}
                disabled={
                  !permission.policies.update || !permission.policies.create
                }
              />
            </div>
            <div>
              <OzLabel htmlFor="policy-description" optional>
                Description
              </OzLabel>
              <OzHelpText className="mb-2">
                Write a short description to add more context to this policy.
              </OzHelpText>
              <OzTextarea
                id="policy-description"
                value={description}
                data-cy={"policy-description"}
                onChange={(e) => setDescription(e.target.value)}
                placeholder={
                  "e.g., Devs are allowed to access servers and servers are allowed to access Devs."
                }
                rows={3}
                disabled={
                  !permission.policies.update || !permission.policies.create
                }
              />
            </div>
          </div>
        </OzTabsContent>
      </OzTabs>

      <ModalFooter className={"items-center"}>
        <div className={"w-full"}>
          <p className={"text-sm mt-auto text-oz2-text-muted"}>
            Learn more about{" "}
            <a
              href={"https://docs.openzro.io/how-to/manage-network-access"}
              target={"_blank"}
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Access Controls
              <ExternalLinkIcon size={12} />
            </a>
          </p>
        </div>
        <div className={"flex gap-3 w-full justify-end"}>
          {!policy ? (
            <>
              {tab == "policy" && (
                <ModalClose asChild={true}>
                  <OzButton variant={"default"}>Cancel</OzButton>
                </ModalClose>
              )}

              {tab == "posture_checks" && (
                <OzButton
                  variant={"default"}
                  onClick={() => setTab("policy")}
                >
                  Back
                </OzButton>
              )}

              {tab == "policy" && (
                <OzButton
                  variant={"primary"}
                  onClick={() => setTab("posture_checks")}
                  disabled={continuePostureChecksDisabled}
                >
                  Continue
                </OzButton>
              )}

              {tab == "posture_checks" && (
                <OzButton
                  variant={"primary"}
                  onClick={() => setTab("general")}
                  disabled={continuePostureChecksDisabled}
                >
                  Continue
                </OzButton>
              )}

              {tab == "general" && (
                <>
                  <OzButton
                    variant={"default"}
                    onClick={() => setTab("posture_checks")}
                  >
                    Back
                  </OzButton>

                  <OzButton
                    variant={"primary"}
                    disabled={submitDisabled || !permission.policies.create}
                    onClick={submit}
                    data-cy={"submit-policy"}
                  >
                    <PlusCircle size={16} />
                    Add Policy
                  </OzButton>
                </>
              )}
            </>
          ) : (
            <>
              <ModalClose asChild={true}>
                <OzButton variant={"default"}>Cancel</OzButton>
              </ModalClose>
              <OzButton
                variant={"primary"}
                disabled={submitDisabled || !permission.policies.update}
                onClick={() => {
                  if (useSave) {
                    submit();
                  } else {
                    close();
                  }
                }}
              >
                Save Changes
              </OzButton>
            </>
          )}
        </div>
      </ModalFooter>
    </ModalContent>
  );
}
