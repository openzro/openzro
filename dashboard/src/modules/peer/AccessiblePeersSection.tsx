"use client";

import SkeletonTable, {
  SkeletonTableHeader,
} from "@components/skeletons/SkeletonTable";
import { usePortalElement } from "@hooks/usePortalElement";
import useFetchApi from "@utils/api";
import * as React from "react";
import { lazy, Suspense } from "react";
import { useUsers } from "@/contexts/UsersProvider";
import type { Peer } from "@/interfaces/Peer";

const AccessiblePeersTable = lazy(
  () => import("@/modules/peer/AccessiblePeersTable"),
);

type Props = {
  peerID: string;
};

// AccessiblePeersSection — Accessible Peers tab content for /peer.
// The OzTabs trigger label provides the section title; this content
// renders just an intro line + the accessible-peers table. Each peer
// in the response is enriched with its owning user so the table cells
// can render the user inline.

export const AccessiblePeersSection = ({ peerID }: Props) => {
  const { data: peers, isLoading } = useFetchApi<Peer[]>(
    `/peers/${peerID}/accessible-peers`,
  );
  const { users } = useUsers();
  const { portalTarget } = usePortalElement<HTMLHeadingElement>();

  const peersWithUser = peers?.map((peer) => {
    if (!users) return peer;
    return {
      ...peer,
      user: users?.find((user) => user.id === peer.user_id),
    };
  });

  return (
    <div className="flex flex-col gap-4 px-8 py-6">
      <p className="max-w-2xl text-[13px] leading-[1.55] text-oz2-text-muted">
        Every peer this one is allowed to reach across the mesh, resolved
        against the active access-control policies.
      </p>

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
          <AccessiblePeersTable
            peerID={peerID}
            isLoading={isLoading}
            peers={peersWithUser}
            headingTarget={portalTarget}
          />
        </Suspense>
      </div>
    </div>
  );
};
