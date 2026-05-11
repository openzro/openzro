"use client";

import FancyToggleSwitch from "@components/FancyToggleSwitch";
import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
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

  const [networkRange, setNetworkRange] = useState<string>(
    groupedRoute.network || "",
  );

  const seedDomains: Domain[] = useMemo(() => {
    const list = groupedRoute.domains ?? [];
    return list.map((name, i) => ({
      name,
      id: `existing-${i}`,
    })) as Domain[];
  }, [groupedRoute.domains]);

  const [domainRoutes, setDomainRoutes] = useReducer(domainReducer, seedDomains);
  const [domainError, setDomainError] = useState<boolean>(false);

  const [description, setDescription] = useState<string>(
    groupedRoute.description || "",
  );
  const [enabled, setEnabled] = useState<boolean>(groupedRoute.enabled);

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
        <div className="px-8 pb-3 pt-1">
          <TabsList>
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
            <TabsTrigger value={"general"}>
              <Text
                size={16}
                className="text-oz2-text-faint group-data-[state=active]/trigger:text-oz2-acc transition-colors"
              />
              Description
            </TabsTrigger>
            <TabsTrigger value={"settings"} disabled={isExitNode}>
              <Settings2
                size={16}
                className="text-oz2-text-faint group-data-[state=active]/trigger:text-oz2-acc transition-colors"
              />
              Settings
            </TabsTrigger>
          </TabsList>
        </div>

        <TabsContent value={"network"} className={"pb-8"}>
          <div className={"px-8 flex flex-col gap-4 pt-2"}>
            <div>
              <OzLabel className="inline-flex items-center gap-2">
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
              </OzLabel>
              <OzHelpText className="mb-2">
                {routeType === "ip-range"
                  ? "Change the private IPv4 range. The new range will be applied to every routing peer attached to this network."
                  : "Change the domain list. The new list will be applied to every routing peer attached to this network."}
              </OzHelpText>
            </div>

            {routeType === "ip-range" && (
              <OzInput
                ref={networkRangeRef}
                prefix={<NetworkIcon size={16} />}
                placeholder={"e.g., 172.16.0.0/16"}
                value={networkRange}
                data-cy={"grouped-network-range"}
                disabled={isExitNode}
                mono
                error={cidrError}
                onChange={(e) => setNetworkRange(e.target.value)}
              />
            )}

            {routeType === "domains" && (
              <div>
                {domainRoutes.length > 0 && (
                  <div className={"mb-3 flex w-full flex-col gap-2"}>
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
                <button
                  type="button"
                  disabled={domainRoutes.length === 32}
                  data-cy={"grouped-add-domain"}
                  onClick={() => setDomainRoutes({ type: "ADD" })}
                  className="inline-flex h-[34px] w-full items-center justify-center gap-2 rounded-oz2-input border border-dashed border-oz2-border-strong bg-transparent px-3 text-[13px] font-medium text-oz2-text-muted transition-colors hover:border-oz2-acc hover:bg-oz2-acc-soft/50 hover:text-oz2-acc-text disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <PlusIcon size={14} />
                  Add Domain
                </button>
              </div>
            )}
          </div>
        </TabsContent>

        <TabsContent value={"general"} className={"px-8 pb-6"}>
          <div className={"flex flex-col gap-6"}>
            <div>
              <OzLabel htmlFor="grouped-route-description" optional>
                Description
              </OzLabel>
              <OzHelpText className="mb-2">
                Write a short description to add more context to this network.
                The same description is applied to every routing peer.
              </OzHelpText>
              <OzTextarea
                id="grouped-route-description"
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
            <div className={cn("flex items-start justify-between gap-6")}>
              <div className="flex-1 min-w-0">
                <OzLabel htmlFor="grouped-route-metric">Metric</OzLabel>
                <OzHelpText className="mt-1">
                  A lower metric indicates a higher priority route. Applied
                  uniformly across all routing peers.
                </OzHelpText>
              </div>
              <div className="w-[200px] shrink-0">
                <OzInput
                  id="grouped-route-metric"
                  min={1}
                  max={9999}
                  value={metric}
                  error={metricError}
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
                "https://docs.openzro.io/how-to/routing-traffic-to-private-networks"
              }
              target={"_blank"}
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Network Routes
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
            onClick={handleSave}
          >
            Save Changes
          </OzButton>
        </div>
      </ModalFooter>
    </ModalContent>
  );
}
