"use client";

import InlineLink from "@components/InlineLink";
import SkeletonTable, {
  SkeletonTableHeader,
} from "@components/skeletons/SkeletonTable";
import FullScreenLoading from "@components/ui/FullScreenLoading";
import useRedirect from "@hooks/useRedirect";
import useFetchApi from "@utils/api";
import { cn } from "@utils/helpers";
import {
  ArrowUpRightIcon,
  DatabaseIcon,
  HelpCircle,
  PencilLineIcon,
  PlusCircle,
  ServerIcon,
  ShieldCheckIcon,
  ShieldXIcon,
} from "lucide-react";
import { useSearchParams } from "next/navigation";
import React, { Suspense, useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";
import {
  OzTabs,
  OzTabsContent,
  OzTabsList,
  OzTabsTrigger,
} from "@/components/v2/OzTabs";
import { usePermissions } from "@/contexts/PermissionsProvider";
import {
  Network,
  NetworkResource,
  NetworkRouter,
} from "@/interfaces/Network";
import { NetworkInformationSquare } from "@/modules/networks/misc/NetworkInformationSquare";
import NetworkModal from "@/modules/networks/NetworkModal";
import {
  NetworkProvider,
  useNetworksContext,
} from "@/modules/networks/NetworkProvider";
import ResourcesTable from "@/modules/networks/resources/ResourcesTable";
import NetworkRoutingPeersTable from "@/modules/networks/routing-peers/NetworkRoutingPeersTable";

// /network — v2 chrome entry. Body groups Resources + Routing Peers
// into a single OzTabs so the operator scans one list at a time
// instead of scrolling through both stacked. Tab labels carry the
// counts so the "is this peer/resource set healthy" question is
// answerable without entering the tab.

export default function NetworkDetailPage() {
  const queryParameter = useSearchParams();
  const networkId = queryParameter.get("id");
  const { data: network, isLoading } = useFetchApi<Network>(
    `/networks/${networkId}`,
    true,
  );

  useRedirect("/networks", false, !networkId);

  return network && !isLoading ? (
    <NetworkOverview network={network} />
  ) : (
    <FullScreenLoading />
  );
}

function NetworkOverview({ network }: Readonly<{ network: Network }>) {
  const { permission } = usePermissions();
  const [networkModal, setNetworkModal] = useState(false);
  const { mutate } = useSWRConfig();

  const isActive = !!(
    network?.routing_peers_count && network.routing_peers_count > 0
  );

  return (
    <NetworkProvider network={network}>
      <div className="space-y-6 p-8 pb-0">
        <header className="flex items-center gap-3">
          <NetworkInformationSquare
            name={network.name}
            active={isActive}
            size={"lg"}
            description={network.description}
          />
          {permission.networks.update && (
            <button
              type="button"
              onClick={() => setNetworkModal(true)}
              aria-label={`Edit ${network.name}`}
              className="grid h-8 w-8 place-items-center rounded-[8px] border border-oz2-border bg-oz2-surface text-oz2-text-2 transition-colors hover:border-oz2-border-strong hover:bg-oz2-hover hover:text-oz2-text"
            >
              <PencilLineIcon size={14} />
            </button>
          )}
          <NetworkModal
            open={networkModal}
            setOpen={setNetworkModal}
            onUpdated={() => {
              mutate(`/networks/${network.id}`);
            }}
            network={network}
          />
        </header>

        <NetworkInformationCard network={network} />
      </div>

      <NetworkSectionTabs network={network} />
    </NetworkProvider>
  );
}

function NetworkSectionTabs({ network }: Readonly<{ network: Network }>) {
  const { data: resources, isLoading: resLoading } = useFetchApi<
    NetworkResource[]
  >(`/networks/${network.id}/resources`);
  const { data: routers, isLoading: rtLoading } = useFetchApi<NetworkRouter[]>(
    `/networks/${network.id}/routers`,
  );

  return (
    <OzTabs defaultValue="resources">
      <div className="px-8 pb-3 pt-6">
        <OzTabsList>
          <OzTabsTrigger value="resources">
            <DatabaseIcon
              size={14}
              className="text-oz2-text-faint group-data-[state=active]/trigger:text-oz2-acc transition-colors"
            />
            Resources
            <span className="font-mono text-[11px] text-oz2-text-faint">
              {resources?.length ?? 0}
            </span>
          </OzTabsTrigger>
          <OzTabsTrigger value="routing-peers">
            <ServerIcon
              size={14}
              className="text-oz2-text-faint group-data-[state=active]/trigger:text-oz2-acc transition-colors"
            />
            Routing Peers
            <span className="font-mono text-[11px] text-oz2-text-faint">
              {routers?.length ?? 0}
            </span>
          </OzTabsTrigger>
        </OzTabsList>
      </div>

      <OzTabsContent value="resources" className="px-8 pb-8">
        <ResourcesTabPanel
          network={network}
          resources={resources}
          isLoading={resLoading}
        />
      </OzTabsContent>
      <OzTabsContent
        value="routing-peers"
        id="routing-peers"
        className="px-8 pb-8"
      >
        <RoutingPeersTabPanel
          network={network}
          routers={routers}
          isLoading={rtLoading}
        />
      </OzTabsContent>
    </OzTabs>
  );
}

function ResourcesTabPanel({
  network,
  resources,
  isLoading,
}: {
  network: Network;
  resources: NetworkResource[] | undefined;
  isLoading: boolean;
}) {
  return (
    <div className="max-w-6xl space-y-4">
      <p className="max-w-2xl text-[13px] leading-[1.55] text-oz2-text-muted">
        Add and manage the addresses (single IP, subnet, or domain) that peers
        in this network are allowed to reach.
      </p>
      <Suspense
        fallback={
          <div>
            <SkeletonTableHeader className="!p-0" />
            <div className="mt-8 w-full">
              <SkeletonTable withHeader={false} />
            </div>
          </div>
        }
      >
        <ResourcesTable isLoading={isLoading} resources={resources} />
      </Suspense>
    </div>
  );
}

function RoutingPeersTabPanel({
  network,
  routers,
  isLoading,
}: {
  network: Network;
  routers: NetworkRouter[] | undefined;
  isLoading: boolean;
}) {
  const { permission } = usePermissions();
  const { openAddRoutingPeerModal } = useNetworksContext();

  return (
    <div className="max-w-6xl space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <p className="max-w-2xl text-[13px] leading-[1.55] text-oz2-text-muted">
          Peers that proxy traffic from the mesh into the resources of this
          network. Run 2+ in different fault domains for high availability.
        </p>
        <OzButton
          variant="primary"
          type="button"
          onClick={() => openAddRoutingPeerModal(network)}
          disabled={!permission.networks.update}
        >
          <PlusCircle size={14} />
          Add Routing Peer
        </OzButton>
      </div>
      <Suspense
        fallback={
          <div>
            <SkeletonTableHeader className="!p-0" />
            <div className="mt-8 w-full">
              <SkeletonTable withHeader={false} />
            </div>
          </div>
        }
      >
        <NetworkRoutingPeersTable isLoading={isLoading} routers={routers} />
      </Suspense>
    </div>
  );
}

function NetworkInformationCard({ network }: Readonly<{ network: Network }>) {
  const isHighlyAvailable = !!(
    network?.routing_peers_count && network?.routing_peers_count >= 2
  );

  const haText = useMemo(
    () =>
      isHighlyAvailable ? (
        <>
          High availability is{" "}
          <span className="font-medium text-oz2-ok">active</span> for this
          network. You can add more routing peers to increase the availability.
        </>
      ) : (
        <>
          High availability is currently{" "}
          <span className="font-medium text-oz2-warn">inactive</span> for this
          network. Add more routing peers or groups with routing peers to
          enable high availability.
        </>
      ),
    [isHighlyAvailable],
  );

  const policyCount = network.policies?.length ?? 0;

  return (
    <OzCard flush className="max-w-3xl">
      <ul className="divide-y divide-oz2-border-soft">
        <li className="flex flex-wrap items-center justify-between gap-3 px-[18px] py-3.5 text-[13.5px]">
          <span className="inline-flex items-center gap-2 text-oz2-text-muted">
            <ServerIcon size={14} />
            High Availability
          </span>
          <span
            className="inline-flex cursor-help items-center gap-2 text-[13px] text-oz2-text-2"
            title={
              typeof haText === "string"
                ? haText
                : isHighlyAvailable
                  ? "High availability is active. You can add more routing peers."
                  : "High availability is inactive. Add more routing peers."
            }
          >
            <span
              className={cn(
                "h-2 w-2 rounded-full",
                isHighlyAvailable ? "bg-oz2-ok" : "bg-oz2-warn",
              )}
            />
            {isHighlyAvailable ? "Active" : "Inactive"}
            <HelpCircle size={12} className="text-oz2-text-faint" />
          </span>
        </li>
        <li className="flex flex-wrap items-center justify-between gap-3 px-[18px] py-3.5 text-[13.5px]">
          <span className="inline-flex items-center gap-2 text-oz2-text-muted">
            {policyCount > 0 ? (
              <>
                <ShieldCheckIcon size={14} className="text-oz2-ok" />
                {policyCount}{" "}
                {policyCount === 1 ? "Active Policy" : "Active Policies"}
              </>
            ) : (
              <>
                <ShieldXIcon size={14} className="text-oz2-err" />
                No Active Policies
              </>
            )}
          </span>
          {policyCount > 0 && (
            <InlineLink href="/access-control">
              Go to Policies
              <ArrowUpRightIcon size={13} />
            </InlineLink>
          )}
        </li>
      </ul>
    </OzCard>
  );
}
