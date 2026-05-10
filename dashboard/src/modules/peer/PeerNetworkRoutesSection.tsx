"use client";

import SkeletonTable from "@components/skeletons/SkeletonTable";
import { usePortalElement } from "@hooks/usePortalElement";
import * as React from "react";
import { lazy, Suspense } from "react";
import type { Peer } from "@/interfaces/Peer";
import { AddExitNodeButton } from "@/modules/exit-node/AddExitNodeButton";
import { useHasExitNodes } from "@/modules/exit-node/useHasExitNodes";
import AddRouteDropdownButton from "@/modules/peer/AddRouteDropdownButton";
import usePeerRoutes from "@/modules/peer/usePeerRoutes";

const PeerRoutesTable = lazy(() => import("@/modules/peer/PeerRoutesTable"));

type Props = {
  peer: Peer;
};

// PeerNetworkRoutesSection — Network Routes tab content for /peer.
// The OzTabs trigger label provides the section title; this content
// renders just the intro/actions row + the routes table.

export const PeerNetworkRoutesSection = ({ peer }: Props) => {
  const { peerRoutes, isLoading } = usePeerRoutes({ peer });
  const hasExitNodes = useHasExitNodes(peer);
  const { portalTarget } = usePortalElement<HTMLHeadingElement>();

  return (
    <div className="flex flex-col gap-4 px-8 py-6">
      <div className="flex max-w-6xl flex-wrap items-start justify-between gap-3">
        <p className="max-w-2xl text-[13px] leading-[1.55] text-oz2-text-muted">
          Reach LANs and VPCs through this peer — no openZro client needed on
          every resource behind it. Use the exit-node toggle to send all
          internet-bound traffic through this peer instead.
        </p>
        <div className="flex shrink-0 items-center gap-2">
          <AddExitNodeButton peer={peer} firstTime={!hasExitNodes} />
          <AddRouteDropdownButton />
        </div>
      </div>

      <div className="max-w-6xl">
        <Suspense
          fallback={
            <div className="w-full">
              <SkeletonTable withHeader={false} />
            </div>
          }
        >
          <PeerRoutesTable
            peer={peer}
            isLoading={isLoading}
            peerRoutes={peerRoutes}
            headingTarget={portalTarget}
          />
        </Suspense>
      </div>
    </div>
  );
};
