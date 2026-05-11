"use client";

import ButtonGroup from "@components/ButtonGroup";
import FancyToggleSwitch from "@components/FancyToggleSwitch";
import FullTooltip from "@components/FullTooltip";
import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
  ModalTrigger,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import { PeerSelector } from "@components/PeerSelector";
import { SegmentedTabs } from "@components/SegmentedTabs";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import {
  OzTabs as Tabs,
  OzTabsContent as TabsContent,
  OzTabsList as TabsList,
  OzTabsTrigger as TabsTrigger,
} from "@/components/v2/OzTabs";
import OzTextarea from "@/components/v2/OzTextarea";
import InputDomain, { domainReducer } from "@components/ui/InputDomain";
import { getOperatingSystem } from "@hooks/useOperatingSystem";
import { IconDirectionSign } from "@tabler/icons-react";
import { cn } from "@utils/helpers";
import cidr from "ip-cidr";
import { uniqBy } from "lodash";
import {
  ArrowDownWideNarrow,
  CircleHelp,
  ExternalLinkIcon,
  FolderGit2,
  GlobeIcon,
  GlobeLockIcon,
  MonitorSmartphoneIcon,
  NetworkIcon,
  PlusCircle,
  PlusIcon,
  Power,
  RouteIcon,
  Settings2,
  Text,
} from "lucide-react";
import { useRouter } from "next/navigation";
import React, { useEffect, useMemo, useReducer, useRef, useState } from "react";
import NetworkRoutesIcon from "@/assets/icons/NetworkRoutesIcon";
import { useDialog } from "@/contexts/DialogProvider";
import { useRoutes } from "@/contexts/RoutesProvider";
import { OperatingSystem } from "@/interfaces/OperatingSystem";
import { Peer } from "@/interfaces/Peer";
import { Policy } from "@/interfaces/Policy";
import { Route } from "@/interfaces/Route";
import useGroupHelper from "@/modules/groups/useGroupHelper";
import { RoutingPeerMasqueradeSwitch } from "@/modules/networks/routing-peers/RoutingPeerMasqueradeSwitch";

type Props = {
  children?: React.ReactNode;
  open?: boolean;
  setOpen?: (open: boolean) => void;
};

// sessionStorage key that /access-control/new reads on mount to
// pre-fill the form when the operator accepts the post-route prompt
// "Do you want to create a policy for this route?". Keeps the seed
// off the URL (the rules JSON would be ugly + leaky in browser
// history) without needing extra plumbing through router state.
const POLICY_SEED_KEY = "oz2-policy-seed";

export default function RouteModal({ children, open, setOpen }: Props) {
  const { confirm } = useDialog();
  const router = useRouter();

  const handleCreatePolicyPrompt = async (r: Route) => {
    if (!r?.access_control_groups) return;

    const choice = await confirm({
      title: `Do you want to create a new access control policy for the route '${r.network_id}'?`,
      description:
        "You have one or more access control groups added to this route. These groups allow you to limit access to this route by using them in access policies.",
      confirmText: "Create Policy",
      cancelText: "Later",
      type: "default",
    });
    if (!choice) return;

    const name = `${r.network_id} Policy`;
    const seed: Policy = {
      name,
      description: "",
      enabled: true,
      source_posture_checks: [],
      rules: [
        {
          name,
          description: "",
          sources: r?.groups || [],
          destinations: r?.access_control_groups || [],
          enabled: true,
          bidirectional: false,
          action: "accept",
          protocol: "all",
          ports: [],
        },
      ],
    };

    try {
      window.sessionStorage.setItem(POLICY_SEED_KEY, JSON.stringify(seed));
    } catch {
      // sessionStorage may be unavailable (private mode quotas, etc.).
      // The page-side hydration is best-effort, so falling back to a
      // blank /access-control/new is acceptable degradation.
    }
    router.push("/access-control/new");
  };

  return (
    <Modal open={open} onOpenChange={setOpen} key={open ? 1 : 0}>
      {children && <ModalTrigger asChild>{children}</ModalTrigger>}
      {open && (
        <RouteModalContent
          onSuccess={async (r) => {
            await handleCreatePolicyPrompt(r);
            setOpen?.(false);
          }}
        />
      )}
    </Modal>
  );
}

type ModalProps = {
  onSuccess?: (route: Route) => void;
  peer?: Peer;
  exitNode?: boolean;
  isFirstExitNode?: boolean;
};

export function RouteModalContent({
  onSuccess,
  peer,
  exitNode,
  isFirstExitNode = false,
}: ModalProps) {
  const { createRoute } = useRoutes();
  const [tab, setTab] = useState(
    exitNode && peer ? "access-control" : "network",
  );

  /**
   * Network Identifier, Description & Network Range
   */
  const [networkIdentifier, setNetworkIdentifier] = useState(
    exitNode
      ? peer
        ? `Exit Node (${
            peer.name.length > 25
              ? peer.name.substring(0, 25) + "..."
              : peer.name
          })`
        : "Exit Node"
      : "",
  );
  const [description, setDescription] = useState("");
  const [networkRange, setNetworkRange] = useState(exitNode ? "0.0.0.0/0" : "");
  const [routingPeer, setRoutingPeer] = useState<Peer | undefined>(peer);
  const [
    routingPeerGroups,
    setRoutingPeerGroups,
    { getGroupsToUpdate: getAllRoutingGroupsToUpdate },
  ] = useGroupHelper({
    initial: [],
  });

  /**
   * DNS Routes
   * IP Range or Domain Tab = ip-range or domains
   */
  const [domainRoutes, setDomainRoutes] = useReducer(domainReducer, []);
  const [domainError, setDomainError] = useState<boolean>(false);
  const [routeType, setRouteTyp] = useState<string>("ip-range");
  const [keepRoute, setKeepRoute] = useState<boolean>(true);

  const isMasqueradeDisabled = useMemo(() => {
    if (exitNode) return true;
    return routeType === "domains";
  }, [exitNode, routeType]);

  const isDomainOrRangeEntered = useMemo(() => {
    if (routeType === "ip-range") return networkRange !== "";
    const isEmptyDomain = domainRoutes.some((d) => d.name === "");
    const isAtLeastOneDomain = domainRoutes.length > 0;
    return !isEmptyDomain && isAtLeastOneDomain && !domainError;
  }, [domainRoutes, routeType, networkRange, domainError]);

  // Enable Masquerade if domain route type is selected
  useEffect(() => {
    if (routeType === "domains") setMasquerade(true);
  }, [routeType]);

  /**
   * Distribution Groups
   */
  const [groups, setGroups, { getGroupsToUpdate }] = useGroupHelper({
    initial: [],
  });

  /**
   * Access Control Groups
   */
  const [
    accessControlGroups,
    setAccessControlGroups,
    { getGroupsToUpdate: getAccessControlGroupsToUpdate },
  ] = useGroupHelper({
    initial: [],
  });

  /**
   * Additional Settings
   */
  const [enabled, setEnabled] = useState<boolean>(true);
  const [metric, setMetric] = useState("9999");
  const [masquerade, setMasquerade] = useState<boolean>(true);

  const isNonLinuxRoutingPeer = useMemo(() => {
    if (!routingPeer) return false;
    return getOperatingSystem(routingPeer.os) != OperatingSystem.LINUX;
  }, [routingPeer]);

  useEffect(() => {
    if (isNonLinuxRoutingPeer) setMasquerade(true);
  }, [isNonLinuxRoutingPeer]);

  /**
   * Create Route
   */
  const createRouteHandler = async () => {
    // Create groups that do not exist
    const g1 = getAllRoutingGroupsToUpdate();
    const g2 = getGroupsToUpdate();
    const g3 = getAccessControlGroupsToUpdate();
    const createOrUpdateGroups = uniqBy([...g1, ...g2, ...g3], "name").map(
      (g) => g.promise,
    );
    const createdGroups = await Promise.all(
      createOrUpdateGroups.map((call) => call()),
    );

    // Check if routing peer is selected
    const useSinglePeer = peerTab === "routing-peer";

    // Get group ids of peer groups
    let peerGroups: string[] = [];
    if (!useSinglePeer) {
      peerGroups = routingPeerGroups
        .map((g) => {
          const find = createdGroups.find((group) => group.name === g.name);
          return find?.id;
        })
        .filter((g) => g !== undefined) as string[];
    }

    // Get distribution group ids
    const groupIds = groups
      .map((g) => {
        const find = createdGroups.find((group) => group.name === g.name);
        return find?.id;
      })
      .filter((g) => g !== undefined) as string[];

    let accessControlGroupIds: string[] | undefined = undefined;
    if (accessControlGroups.length > 0) {
      accessControlGroupIds = accessControlGroups
        .map((g) => {
          const find = createdGroups.find((group) => group.name === g.name);
          return find?.id;
        })
        .filter((g) => g !== undefined) as string[];
    }

    const domainRouteNames =
      routeType === "domains"
        ? domainRoutes.map((d) => d.name).filter((d) => d !== "")
        : undefined;
    const useKeepRoute = routeType === "domains" ? keepRoute : undefined;

    createRoute(
      {
        network_id: networkIdentifier,
        description: description || "",
        enabled: enabled,
        peer: useSinglePeer ? routingPeer?.id : undefined,
        peer_groups: useSinglePeer ? undefined : peerGroups || undefined,
        network: routeType === "ip-range" ? networkRange : undefined,
        domains: domainRouteNames,
        keep_route: useKeepRoute,
        metric: Number(metric) || 9999,
        masquerade: useSinglePeer && isNonLinuxRoutingPeer ? true : masquerade,
        groups: groupIds,
        access_control_groups: accessControlGroupIds || undefined,
      },
      onSuccess,
    );
  };

  /**
   * Refs to manage input focus on tab change
   */
  const networkRangeRef = useRef<HTMLInputElement>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const [peerTab, setPeerTab] = useState("routing-peer");

  /**
   * Validate CIDR Range
   */
  const cidrError = useMemo(() => {
    if (networkRange == "") return "";
    const validCIDR = cidr.isValidAddress(networkRange);
    if (!validCIDR) return "Please enter a valid CIDR, e.g., 192.168.1.0/24";
  }, [networkRange]);

  const isGroupsEntered = useMemo(() => {
    return groups.length > 0;
  }, [groups]);

  /**
   * Allow to create route only when all fields are filled
   */
  const isNetworkEntered = useMemo(() => {
    return !(
      (cidrError && cidrError.length > 1) ||
      (peerTab === "peer-group" && routingPeerGroups.length == 0) ||
      (peerTab === "routing-peer" && !routingPeer) ||
      !isDomainOrRangeEntered
    );
  }, [
    cidrError,
    peerTab,
    routingPeerGroups.length,
    routingPeer,
    isDomainOrRangeEntered,
  ]);

  const networkIdentifierError = useMemo(() => {
    return (networkIdentifier?.length || 0) > 40
      ? "Network Identifier must be less than 40 characters"
      : "";
  }, [networkIdentifier]);

  const metricError = useMemo(() => {
    return parseInt(metric) < 1 || parseInt(metric) > 9999
      ? "Metric must be between 1 and 9999"
      : "";
  }, [metric]);

  const isNameEntered = useMemo(() => {
    return networkIdentifier != "" && networkIdentifierError == "";
  }, [networkIdentifier, networkIdentifierError]);

  const canCreateOrSave = useMemo(() => {
    return isNetworkEntered && isNameEntered && metricError == "";
  }, [isNetworkEntered, isNameEntered, metricError]);

  const singleRoutingPeerGroups = useMemo(() => {
    if (!routingPeer) return [];
    return routingPeer?.groups;
  }, [routingPeer]);

  return (
    <ModalContent maxWidthClass={"max-w-2xl"}>
      <ModalHeader
        icon={
          exitNode ? (
            <IconDirectionSign size={20} />
          ) : (
            <NetworkRoutesIcon className={"fill-openzro"} />
          )
        }
        title={
          exitNode
            ? isFirstExitNode
              ? "Set Up Exit Node"
              : "Add Exit Node"
            : "Create New  Route"
        }
        truncate={!!peer}
        description={
          exitNode
            ? peer
              ? `Route all traffic through the peer '${peer.name}'`
              : "Route all internet traffic through a peer"
            : "Access LANs and VPC by adding a network route."
        }
        color={exitNode ? "yellow" : "openzro"}
      />

      <Tabs defaultValue={tab} onValueChange={(v) => setTab(v)} value={tab}>
        <div className="px-8 pb-3 pt-1">
          <TabsList>
            {!(exitNode && peer) && (
              <TabsTrigger
                value={"network"}
                onClick={() => networkRangeRef.current?.focus()}
              >
                <RouteIcon
                  size={16}
                  className="text-oz2-text-faint group-data-[state=active]/trigger:text-oz2-acc transition-colors"
                />
                Route
              </TabsTrigger>
            )}

            <TabsTrigger value={"access-control"} disabled={!isNetworkEntered}>
              <FolderGit2
                size={16}
                className="text-oz2-text-faint group-data-[state=active]/trigger:text-oz2-acc transition-colors"
              />
              Groups
            </TabsTrigger>
            <TabsTrigger
              value={"general"}
              disabled={!isGroupsEntered}
              onClick={() => nameRef.current?.focus()}
            >
              <Text
                size={16}
                className="text-oz2-text-faint group-data-[state=active]/trigger:text-oz2-acc transition-colors"
              />
              Name & Description
            </TabsTrigger>
            <TabsTrigger
              value={"settings"}
              disabled={!isNetworkEntered || !isNameEntered || !isGroupsEntered}
            >
              <Settings2
                size={16}
                className="text-oz2-text-faint group-data-[state=active]/trigger:text-oz2-acc transition-colors"
              />
              Additional Settings
            </TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value={"network"} className={"pb-8"}>
          <div className={"px-8 flex-col flex gap-4"}>
            <div className={cn(exitNode && "hidden")}>
              <OzLabel>Route Type</OzLabel>
              <OzHelpText className="mb-2">
                Select your route type to add either a network range or a list
                of domains.
              </OzHelpText>
              <div className={"flex justify-between items-center w-full"}>
                <ButtonGroup className={"w-full"}>
                  <ButtonGroup.Button
                    variant={routeType == "ip-range" ? "tertiary" : "secondary"}
                    onClick={() => setRouteTyp("ip-range")}
                    className={"w-full"}
                  >
                    <NetworkIcon size={16} />
                    Network Range
                  </ButtonGroup.Button>
                  <ButtonGroup.Button
                    variant={routeType == "domains" ? "tertiary" : "secondary"}
                    onClick={() => setRouteTyp("domains")}
                    className={"w-full"}
                  >
                    <GlobeIcon size={16} />
                    Domains
                  </ButtonGroup.Button>
                </ButtonGroup>
              </div>

              <div
                className={cn(
                  "mt-5 mb-3",
                  routeType !== "ip-range" && "hidden",
                )}
              >
                <OzLabel>Network Range</OzLabel>
                <OzHelpText className="mb-2">
                  Add a private IPv4 address range
                </OzHelpText>
                <OzInput
                  ref={networkRangeRef}
                  prefix={<NetworkIcon size={16} />}
                  placeholder={"e.g., 172.16.0.0/16"}
                  value={networkRange}
                  data-cy={"network-range"}
                  mono
                  error={cidrError}
                  onChange={(e) => setNetworkRange(e.target.value)}
                />
              </div>

              <div
                className={cn("mt-5 mb-3", routeType !== "domains" && "hidden")}
              >
                <OzLabel>Domains</OzLabel>
                <OzHelpText className="mb-2">
                  Add domains that dynamically resolve to one or more IPv4
                  addresses. A maximum of 32 domains can be added.
                </OzHelpText>
                <div>
                  {domainRoutes.length > 0 && (
                    <div className={"mb-3 flex w-full flex-col gap-2"}>
                      {domainRoutes.map((domain, i) => (
                        <InputDomain
                          key={domain.id}
                          value={domain}
                          data-cy={`domain-input-${i}`}
                          onChange={(d) =>
                            setDomainRoutes({
                              type: "UPDATE",
                              index: i,
                              d,
                            })
                          }
                          onError={setDomainError}
                          onRemove={() =>
                            setDomainRoutes({
                              type: "REMOVE",
                              index: i,
                            })
                          }
                        />
                      ))}
                    </div>
                  )}
                  <button
                    type="button"
                    disabled={domainRoutes.length === 32}
                    data-cy={"add-domain"}
                    onClick={() => setDomainRoutes({ type: "ADD" })}
                    className="inline-flex h-[34px] w-full items-center justify-center gap-2 rounded-oz2-input border border-dashed border-oz2-border-strong bg-transparent px-3 text-[13px] font-medium text-oz2-text-muted transition-colors hover:border-oz2-acc hover:bg-oz2-acc-soft/50 hover:text-oz2-acc-text disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    <PlusIcon size={14} />
                    Add Domain
                  </button>
                </div>
                <div className={cn("mt-6 w-full")}>
                  <FullTooltip
                    side={"top"}
                    content={
                      <div className={"text-xs max-w-xs"}>
                        DNS records for load-balanced systems often change.
                        Keeping resolved addresses ensures ongoing connections
                        to active resources remain uninterrupted.
                      </div>
                    }
                    className={"w-full block"}
                  >
                    <FancyToggleSwitch
                      value={keepRoute}
                      onChange={setKeepRoute}
                      label={
                        <>
                          <div className={"flex gap-2"}>
                            <GlobeLockIcon size={14} />
                            Keep Routes
                            <CircleHelp
                              size={12}
                              className={"top-[1px] relative text-nb-gray-300"}
                            />
                          </div>
                        </>
                      }
                      helpText={
                        <div>
                          Retain previously resolved routes after IP address
                          updates to maintain stable connections.
                        </div>
                      }
                    />
                  </FullTooltip>
                </div>
              </div>
            </div>

            {exitNode && peer ? (
              <></>
            ) : (
              <SegmentedTabs
                value={peerTab}
                onChange={(state) => {
                  setPeerTab(state);
                  setRoutingPeer(undefined);
                  setRoutingPeerGroups([]);
                }}
              >
                <SegmentedTabs.List>
                  <SegmentedTabs.Trigger value={"routing-peer"}>
                    <MonitorSmartphoneIcon size={16} />
                    Routing Peer
                  </SegmentedTabs.Trigger>

                  <SegmentedTabs.Trigger value={"peer-group"} disabled={!!peer}>
                    <FolderGit2 size={16} />
                    Peer Group
                  </SegmentedTabs.Trigger>
                </SegmentedTabs.List>
                <SegmentedTabs.Content value={"routing-peer"}>
                  <div>
                    <OzHelpText className="mb-2">
                      Assign a single peer as a routing peer for the
                      {exitNode ? " exit node." : " network route."}
                    </OzHelpText>
                    <PeerSelector
                      onChange={setRoutingPeer}
                      value={routingPeer}
                      disabled={!!peer}
                    />
                  </div>
                </SegmentedTabs.Content>
                <SegmentedTabs.Content value={"peer-group"}>
                  <div>
                    <OzHelpText className="mb-2">
                      Assign a peer group with machines to be used as
                      {exitNode ? " exit nodes." : " routing peers."}
                    </OzHelpText>
                    <PeerGroupSelector
                      max={1}
                      onChange={setRoutingPeerGroups}
                      values={routingPeerGroups}
                    />
                  </div>
                </SegmentedTabs.Content>
              </SegmentedTabs>
            )}
          </div>
        </TabsContent>
        <TabsContent value={"access-control"} className={"pb-8"}>
          <div className={"px-8 flex-col flex gap-6"}>
            <div>
              <OzLabel>Distribution Groups</OzLabel>
              <OzHelpText className="mb-2">
                {exitNode
                  ? peer
                    ? `Route all internet traffic through this peer for the following groups`
                    : `Route all internet traffic through the peer(s) for the following groups`
                  : "Advertise this route to peers that belong to the following groups"}
              </OzHelpText>
              <PeerGroupSelector onChange={setGroups} values={groups} />
            </div>
            <div>
              <OzLabel optional>Access Control Groups</OzLabel>
              <OzHelpText className="mb-2">
                These groups allow you to limit access to this route. Simply use
                these groups as a destination when creating access policies.
              </OzHelpText>
              <PeerGroupSelector
                dataCy={"access-control-groups-selector"}
                onChange={setAccessControlGroups}
                values={accessControlGroups}
              />
            </div>
          </div>
        </TabsContent>
        <TabsContent value={"general"} className={"px-8 pb-6"}>
          <div className={"flex flex-col gap-6"}>
            <div>
              <OzLabel htmlFor="route-network-identifier" required>
                Network Identifier
              </OzLabel>
              <OzHelpText className="mb-2">
                Add a unique network identifier that is assigned to each device.
              </OzHelpText>
              <OzInput
                id="route-network-identifier"
                error={networkIdentifierError}
                autoFocus={true}
                data-cy={"network-identifier"}
                tabIndex={0}
                ref={nameRef}
                placeholder={"e.g., aws-eu-central-1-vpc"}
                value={networkIdentifier}
                onChange={(e) => setNetworkIdentifier(e.target.value)}
              />
            </div>
            <div>
              <OzLabel htmlFor="route-description" optional>
                Description
              </OzLabel>
              <OzHelpText className="mb-2">
                Write a short description to add more context to this route.
              </OzHelpText>
              <OzTextarea
                id="route-description"
                data-cy={"description"}
                placeholder={
                  "e.g., Route to access all devices in the AWS VPC, located in Frankfurt."
                }
                value={description}
                rows={3}
                onChange={(e) => setDescription(e.target.value)}
              />
            </div>
          </div>
        </TabsContent>
        <TabsContent value={"settings"} className={"pb-4"}>
          <div className={"px-8 flex flex-col gap-6"}>
            <FancyToggleSwitch
              value={enabled}
              onChange={setEnabled}
              label={
                <>
                  <Power size={15} />
                  Enable Route
                </>
              }
              helpText={"Use this switch to enable or disable the route."}
            />

            {!exitNode && (
              <RoutingPeerMasqueradeSwitch
                value={masquerade}
                onChange={setMasquerade}
                disabled={isNonLinuxRoutingPeer}
                routingPeerGroupId={routingPeerGroups?.[0]?.id}
              />
            )}

            <div className={cn("flex items-start justify-between gap-6")}>
              <div className="flex-1 min-w-0">
                <OzLabel htmlFor="route-metric">Metric</OzLabel>
                <OzHelpText className="mt-1">
                  A lower metric indicates higher priority routes.
                </OzHelpText>
              </div>
              <div className="w-[200px] shrink-0">
                <OzInput
                  id="route-metric"
                  min={1}
                  max={9999}
                  value={metric}
                  error={metricError}
                  data-cy={"metric"}
                  type={"number"}
                  onChange={(e) => setMetric(e.target.value)}
                  prefix={<ArrowDownWideNarrow size={16} />}
                />
              </div>
            </div>
          </div>
        </TabsContent>
      </Tabs>

      <ModalFooter className={"items-center"}>
        <div className={"w-full"}>
          <p className={"text-sm mt-auto text-oz2-text-muted"}>
            Learn more about{" "}
            <a
              href={
                exitNode
                  ? "https://docs.openzro.io/how-to/configuring-default-routes-for-internet-traffic"
                  : "https://docs.openzro.io/how-to/routing-traffic-to-private-networks"
              }
              target={"_blank"}
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
            >
              {exitNode ? "Exit Nodes" : "Network Routes"}
              <ExternalLinkIcon size={12} />
            </a>
          </p>
        </div>
        <div className={"flex gap-3 w-full justify-end"}>
          {(tab == "network" || (tab == "access-control" && exitNode)) && (
            <ModalClose asChild={true}>
              <OzButton variant={"default"}>Cancel</OzButton>
            </ModalClose>
          )}

          {tab == "access-control" && !exitNode && (
            <OzButton variant={"default"} onClick={() => setTab("network")}>
              Back
            </OzButton>
          )}

          {tab == "general" && (
            <OzButton
              variant={"default"}
              onClick={() => setTab("access-control")}
            >
              Back
            </OzButton>
          )}

          {tab == "settings" && (
            <OzButton variant={"default"} onClick={() => setTab("general")}>
              Back
            </OzButton>
          )}

          {tab == "network" && (
            <OzButton
              variant={"primary"}
              onClick={() => setTab("access-control")}
              disabled={!isNetworkEntered}
            >
              Continue
            </OzButton>
          )}
          {tab == "access-control" && (
            <OzButton
              variant={"primary"}
              onClick={() => setTab("general")}
              disabled={!isGroupsEntered}
            >
              Continue
            </OzButton>
          )}
          {tab == "general" && (
            <OzButton
              variant={"primary"}
              onClick={() => setTab("settings")}
              disabled={!isNameEntered || !isNetworkEntered}
            >
              Continue
            </OzButton>
          )}
          {tab == "settings" && (
            <OzButton
              variant={"primary"}
              disabled={!canCreateOrSave}
              data-cy={"submit-route"}
              onClick={createRouteHandler}
            >
              <PlusCircle size={16} />
              {exitNode ? "Add Exit Node" : "Add Route"}
            </OzButton>
          )}
        </div>
      </ModalFooter>
    </ModalContent>
  );
}
