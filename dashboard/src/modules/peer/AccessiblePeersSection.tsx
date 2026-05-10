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

// AccessiblePeersSection — v2 chrome around AccessiblePeersTable.
// The table itself still renders legacy DataTable paint; a v2 paint
// pass is tracked separately. Section just enriches peers with their
// owning user (so the table cells can render the user info inline)
// and forwards the data + portal headingTarget.

export const AccessiblePeersSection = ({ peerID }: Props) => {
  const { data: peers, isLoading } = useFetchApi<Peer[]>(
    `/peers/${peerID}/accessible-peers`,
  );
  const { users } = useUsers();
  const { ref: headingRef, portalTarget } =
    usePortalElement<HTMLHeadingElement>();

  const peersWithUser = peers?.map((peer) => {
    if (!users) return peer;
    return {
      ...peer,
      user: users?.find((user) => user.id === peer.user_id),
    };
  });

  return (
    <section className="space-y-4 px-8 pb-10">
      <header className="max-w-6xl">
        <h2
          ref={headingRef}
          className="text-[18px] font-semibold tracking-tight text-oz2-text"
        >
          Accessible Peers
        </h2>
        <p className="mt-1 max-w-2xl text-[13px] leading-[1.55] text-oz2-text-muted">
          Every peer this one is allowed to reach across the mesh, resolved
          against the active access-control policies.
        </p>
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
          <AccessiblePeersTable
            peerID={peerID}
            isLoading={isLoading}
            peers={peersWithUser}
            headingTarget={portalTarget}
          />
        </Suspense>
      </div>
    </section>
  );
};
