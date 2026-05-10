"use client";

import InlineLink from "@components/InlineLink";
import FullScreenLoading from "@components/ui/FullScreenLoading";
import useRedirect from "@hooks/useRedirect";
import useFetchApi from "@utils/api";
import { cn } from "@utils/helpers";
import {
  ArrowUpRightIcon,
  HelpCircle,
  PencilLineIcon,
  ServerIcon,
  ShieldCheckIcon,
  ShieldXIcon,
} from "lucide-react";
import { useSearchParams } from "next/navigation";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import OzCard from "@/components/v2/OzCard";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Network } from "@/interfaces/Network";
import { NetworkInformationSquare } from "@/modules/networks/misc/NetworkInformationSquare";
import NetworkModal from "@/modules/networks/NetworkModal";
import { NetworkProvider } from "@/modules/networks/NetworkProvider";
import { ResourcesSection } from "@/modules/networks/resources/ResourcesSection";
import { NetworkRoutingPeersSection } from "@/modules/networks/routing-peers/NetworkRoutingPeersSection";

// /network — v2 chrome entry. Body keeps the existing
// ResourcesSection + NetworkRoutingPeersSection composition (those
// are sub-modules with their own state); only the wrapping page
// chrome flips from PageContainer + Breadcrumbs + Card.List to v2
// header + OzCard for the info row. Modal mount unchanged.

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
      <div className="space-y-6 p-8">
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

      <ResourcesSection network={network} />
      <NetworkRoutingPeersSection network={network} />
    </NetworkProvider>
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
                : (isHighlyAvailable
                    ? "High availability is active. You can add more routing peers."
                    : "High availability is inactive. Add more routing peers.")
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
