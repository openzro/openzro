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

// PeerNetworkRoutesSection — v2 chrome around PeerRoutesTable. The
// table itself still renders legacy DataTable paint; a v2 paint pass
// is tracked separately. AddExitNodeButton + AddRouteDropdownButton
// keep their own internals — those are dropdown / modal triggers
// that already deserve their own port.

export const PeerNetworkRoutesSection = ({ peer }: Props) => {
  const { peerRoutes, isLoading } = usePeerRoutes({ peer });
  const hasExitNodes = useHasExitNodes(peer);
  const { ref: headingRef, portalTarget } =
    usePortalElement<HTMLHeadingElement>();

  return (
    <section className="space-y-4 px-8 pb-10">
      <header className="flex max-w-6xl flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2
            ref={headingRef}
            className="text-[18px] font-semibold tracking-tight text-oz2-text"
          >
            Network Routes
          </h2>
          <p className="mt-1 max-w-2xl text-[13px] leading-[1.55] text-oz2-text-muted">
            Reach LANs and VPCs through this peer — no openZro client needed
            on every resource behind it. Use the exit-node toggle to send all
            internet-bound traffic through this peer instead.
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <AddExitNodeButton peer={peer} firstTime={!hasExitNodes} />
          <AddRouteDropdownButton />
        </div>
      </header>

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
    </section>
  );
};
