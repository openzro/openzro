"use client";

import SkeletonTable, {
  SkeletonTableHeader,
} from "@components/skeletons/SkeletonTable";
import { usePortalElement } from "@hooks/usePortalElement";
import useFetchApi from "@utils/api";
import { PlusCircle } from "lucide-react";
import * as React from "react";
import { Suspense } from "react";
import OzButton from "@/components/v2/OzButton";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Network, NetworkRouter } from "@/interfaces/Network";
import { useNetworksContext } from "@/modules/networks/NetworkProvider";
import NetworkRoutingPeersTable from "@/modules/networks/routing-peers/NetworkRoutingPeersTable";

// NetworkRoutingPeersSection — wraps NetworkRoutingPeersTable with
// v2 section chrome (h2 + intro paragraph + Add Routing Peer CTA).
// The #routing-peers id stays so deep-links from PeersTable cells
// keep resolving.

export const NetworkRoutingPeersSection = ({
  network,
}: {
  network: Network;
}) => {
  const { permission } = usePermissions();
  const { data: routers, isLoading } = useFetchApi<NetworkRouter[]>(
    `/networks/${network.id}/routers`,
  );
  const { ref: headingRef, portalTarget } =
    usePortalElement<HTMLHeadingElement>();

  const { openAddRoutingPeerModal } = useNetworksContext();

  return (
    <section className="space-y-4 px-8 py-7" id="routing-peers">
      <header className="flex max-w-6xl flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2
            ref={headingRef}
            className="text-[18px] font-semibold tracking-tight text-oz2-text"
          >
            Routing Peers
          </h2>
          <p className="mt-1 max-w-2xl text-[13px] leading-[1.55] text-oz2-text-muted">
            Peers that proxy traffic from the mesh into the resources of this
            network. Run 2+ in different fault domains for high availability.
          </p>
        </div>
        <OzButton
          variant="primary"
          type="button"
          onClick={() => openAddRoutingPeerModal(network)}
          disabled={!permission.networks.update}
        >
          <PlusCircle size={14} />
          Add Routing Peer
        </OzButton>
      </header>

      <div className="max-w-6xl">
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
          <NetworkRoutingPeersTable
            isLoading={isLoading}
            routers={routers}
            headingTarget={portalTarget}
          />
        </Suspense>
      </div>
    </section>
  );
};
