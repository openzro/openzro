"use client";

import Button from "@components/Button";
import FancyToggleSwitch from "@components/FancyToggleSwitch";
import HelpText from "@components/HelpText";
import InlineLink from "@components/InlineLink";
import { Input } from "@components/Input";
import { Label } from "@components/Label";
import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import Paragraph from "@components/Paragraph";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@components/Tabs";
import { Textarea } from "@components/Textarea";
import InputDomain, { domainReducer } from "@components/ui/InputDomain";
import { cn } from "@utils/helpers";
import cidr from "ip-cidr";
import {
  ArrowDownWideNarrow,
  ExternalLinkIcon,
  GlobeIcon,
  NetworkIcon,
  PlusIcon,
  Power,
  RouteIcon,
  Settings2,
  Text,
} from "lucide-react";
import React, { useMemo, useReducer, useRef, useState } from "react";
import NetworkRoutesIcon from "@/assets/icons/NetworkRoutesIcon";
import { useRoutes } from "@/contexts/RoutesProvider";
import { Domain } from "@/interfaces/Domain";
import { GroupedRoute } from "@/interfaces/Route";

type Props = {
  groupedRoute: GroupedRoute;
  open: boolean;
  onOpenChange?: (open: boolean) => void;
};

export default function GroupedRouteUpdateModal({
  groupedRoute,
  open,
  onOpenChange,
}: Props) {
  return (
    <Modal open={open} onOpenChange={onOpenChange} key={open ? 1 : 0}>
      {open && (
        <GroupedRouteUpdateModalContent
          groupedRoute={groupedRoute}
          onSuccess={() => onOpenChange && onOpenChange(false)}
        />
      )}
    </Modal>
  );
}

type ContentProps = {
  groupedRoute: GroupedRoute;
  onSuccess?: () => void;
};

function GroupedRouteUpdateModalContent({
  groupedRoute,
  onSuccess,
}: ContentProps) {
  const { updateGroupedRoute } = useRoutes();

  const isUsingDomains =
    groupedRoute.domains && groupedRoute.domains.length > 0;
  const routeType: "ip-range" | "domains" = isUsingDomains
    ? "domains"
    : "ip-range";

  const isExitNode = useMemo(
    () => groupedRoute.network === "0.0.0.0/0",
    [groupedRoute.network],
  );

  // Network range — editable only for ip-range groups (not exit nodes,
  // since changing 0.0.0.0/0 to anything else turns it into a regular
  // route and we don't want to model that switch through this UI).
  const [networkRange, setNetworkRange] = useState<string>(
    groupedRoute.network || "",
  );

  // Domains — editable only for domain groups. Seed the reducer from
  // the existing list. We mark each existing domain with `is_selected`
  // === undefined so the InputDomain treats it as user-entered.
  const seedDomains: Domain[] = useMemo(() => {
    const list = groupedRoute.domains ?? [];
    return list.map((name, i) => ({
      name,
      id: `existing-${i}`,
    })) as Domain[];
  }, [groupedRoute.domains]);

  const [domainRoutes, setDomainRoutes] = useReducer(domainReducer, seedDomains);
  const [domainError, setDomainError] = useState<boolean>(false);

  // Description, enabled, metric — applied uniformly across children.
  const [description, setDescription] = useState<string>(
    groupedRoute.description || "",
  );
  const [enabled, setEnabled] = useState<boolean>(groupedRoute.enabled);

  // Metric: GroupedRoute doesn't carry it; seed from the first child.
  const initialMetric = useMemo(() => {
    const first = groupedRoute.routes?.[0];
    return first?.metric?.toString() ?? "9999";
  }, [groupedRoute.routes]);
  const [metric, setMetric] = useState<string>(initialMetric);

  const networkRangeRef = useRef<HTMLInputElement>(null);

  const cidrError = useMemo(() => {
    if (routeType !== "ip-range") return "";
    if (networkRange === "") return "";
    return cidr.isValidAddress(networkRange) ? "" : "Please enter a valid CIDR";
  }, [networkRange, routeType]);

  const metricError = useMemo(() => {
    const n = parseInt(metric.toString());
    return n < 1 || n > 9999 ? "Metric must be between 1 and 9999" : "";
  }, [metric]);

  const isDirty = useMemo(() => {
    if (description !== (groupedRoute.description || "")) return true;
    if (enabled !== groupedRoute.enabled) return true;
    if (metric !== initialMetric) return true;
    if (routeType === "ip-range" && networkRange !== (groupedRoute.network || ""))
      return true;
    if (routeType === "domains") {
      const before = (groupedRoute.domains || []).join(",");
      const after = domainRoutes
        .map((d) => d.name)
        .filter((n) => n !== "")
        .join(",");
      if (before !== after) return true;
    }
    return false;
  }, [
    description,
    enabled,
    metric,
    initialMetric,
    networkRange,
    routeType,
    domainRoutes,
    groupedRoute,
  ]);

  const isDisabled = useMemo(() => {
    if (!isDirty) return true;
    if (cidrError) return true;
    if (metricError) return true;
    if (routeType === "ip-range" && networkRange === "") return true;
    if (routeType === "domains") {
      if (domainError) return true;
      const cleaned = domainRoutes.map((d) => d.name).filter((n) => n !== "");
      if (cleaned.length === 0) return true;
    }
    return false;
  }, [
    isDirty,
    cidrError,
    metricError,
    routeType,
    networkRange,
    domainError,
    domainRoutes,
  ]);

  const handleSave = () => {
    const cleanedDomains =
      routeType === "domains"
        ? domainRoutes.map((d) => d.name).filter((n) => n !== "")
        : undefined;

    updateGroupedRoute(
      groupedRoute,
      {
        description,
        enabled,
        metric: Number(metric) || 9999,
        network: routeType === "ip-range" ? networkRange : undefined,
        domains: cleanedDomains,
      },
      onSuccess,
    );
  };

  const headerSubtitle = useMemo(() => {
    if (isUsingDomains && groupedRoute.domains)
      return groupedRoute.domains.join(", ");
    return groupedRoute.network || "";
  }, [groupedRoute.network, groupedRoute.domains, isUsingDomains]);

  const [tab, setTab] = useState("network");

  return (
    <ModalContent maxWidthClass={"max-w-2xl"}>
      <ModalHeader
        icon={<NetworkRoutesIcon className={"fill-openzro"} />}
        title={"Update " + groupedRoute.network_id}
        description={headerSubtitle}
        color={"openzro"}
        truncate={true}
      />

      <Tabs defaultValue={tab} onValueChange={(v) => setTab(v)} value={tab}>
        <TabsList justify={"start"} className={"px-8"}>
          <TabsTrigger
            value={"network"}
            onClick={() => networkRangeRef.current?.focus()}
          >
            <RouteIcon
              size={16}
              className={
                "text-nb-gray-500 group-data-[state=active]/trigger:text-openzro transition-all"
              }
            />
            Route
          </TabsTrigger>
          <TabsTrigger value={"general"}>
            <Text
              size={16}
              className={
                "text-nb-gray-500 group-data-[state=active]/trigger:text-openzro transition-all"
              }
            />
            Description
          </TabsTrigger>
          <TabsTrigger value={"settings"} disabled={isExitNode}>
            <Settings2
              size={16}
              className={
                "text-nb-gray-500 group-data-[state=active]/trigger:text-openzro transition-all"
              }
            />
            Settings
          </TabsTrigger>
        </TabsList>

        <TabsContent value={"network"} className={"pb-8"}>
          <div className={"px-8 flex-col flex gap-4 pt-2"}>
            <div>
              <Label className={"flex items-center gap-2"}>
                {routeType === "ip-range" ? (
                  <>
                    <NetworkIcon size={14} />
                    Network Range
                  </>
                ) : (
                  <>
                    <GlobeIcon size={14} />
                    Domains
                  </>
                )}
              </Label>
              <HelpText>
                {routeType === "ip-range"
                  ? "Change the private IPv4 range. The new range will be applied to every routing peer attached to this network."
                  : "Change the domain list. The new list will be applied to every routing peer attached to this network."}
              </HelpText>
            </div>

            {routeType === "ip-range" && (
              <Input
                ref={networkRangeRef}
                customPrefix={<NetworkIcon size={16} />}
                placeholder={"e.g., 172.16.0.0/16"}
                value={networkRange}
                data-cy={"grouped-network-range"}
                disabled={isExitNode}
                className={"font-mono !text-[13px]"}
                error={cidrError}
                onChange={(e) => setNetworkRange(e.target.value)}
              />
            )}

            {routeType === "domains" && (
              <div>
                {domainRoutes.length > 0 && (
                  <div className={"flex flex-col gap-2 w-full mb-3"}>
                    {domainRoutes.map((domain, i) => (
                      <InputDomain
                        key={domain.id}
                        value={domain}
                        data-cy={`grouped-domain-input-${i}`}
                        onChange={(d) =>
                          setDomainRoutes({
                            type: "UPDATE",
                            index: i,
                            d,
                          })
                        }
                        onError={setDomainError}
                        onRemove={() =>
                          setDomainRoutes({ type: "REMOVE", index: i })
                        }
                      />
                    ))}
                  </div>
                )}
                <Button
                  variant={"dotted"}
                  className={"w-full"}
                  size={"sm"}
                  disabled={domainRoutes.length === 32}
                  data-cy={"grouped-add-domain"}
                  onClick={() => setDomainRoutes({ type: "ADD" })}
                >
                  <PlusIcon size={14} />
                  Add Domain
                </Button>
              </div>
            )}
          </div>
        </TabsContent>

        <TabsContent value={"general"} className={"px-8 pb-6"}>
          <div className={"flex flex-col gap-6"}>
            <div>
              <Label>Description (optional)</Label>
              <HelpText>
                Write a short description to add more context to this network.
                The same description is applied to every routing peer.
              </HelpText>
              <Textarea
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
                  Enable Network
                </>
              }
              helpText={
                "Enable or disable the network. Applies to every routing peer attached."
              }
            />
            <div className={cn("flex justify-between")}>
              <div>
                <Label>Metric</Label>
                <HelpText className={"max-w-[260px]"}>
                  A lower metric indicates a higher priority route. Applied
                  uniformly across all routing peers.
                </HelpText>
              </div>

              <Input
                min={1}
                max={9999}
                maxWidthClass={"max-w-[200px]"}
                value={metric}
                error={metricError}
                errorTooltip={true}
                type={"number"}
                onChange={(e) => setMetric(e.target.value)}
                customPrefix={
                  <ArrowDownWideNarrow
                    size={16}
                    className={"text-nb-gray-300"}
                  />
                }
              />
            </div>
          </div>
        </TabsContent>
      </Tabs>

      <ModalFooter className={"items-center"}>
        <div className={"w-full"}>
          <Paragraph className={"text-sm mt-auto"}>
            Learn more about
            <InlineLink
              href={
                "https://docs.openzro.io/how-to/routing-traffic-to-private-networks"
              }
              target={"_blank"}
            >
              Network Routes
              <ExternalLinkIcon size={12} />
            </InlineLink>
          </Paragraph>
        </div>
        <div className={"flex gap-3 w-full justify-end"}>
          <ModalClose asChild={true}>
            <Button variant={"secondary"}>Cancel</Button>
          </ModalClose>
          <Button
            variant={"primary"}
            disabled={isDisabled}
            onClick={handleSave}
          >
            Save Changes
          </Button>
        </div>
      </ModalFooter>
    </ModalContent>
  );
}
