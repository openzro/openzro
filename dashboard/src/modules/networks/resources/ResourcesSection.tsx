"use client";

import SkeletonTable, {
  SkeletonTableHeader,
} from "@components/skeletons/SkeletonTable";
import { usePortalElement } from "@hooks/usePortalElement";
import useFetchApi from "@utils/api";
import * as React from "react";
import { Suspense } from "react";
import { Network, NetworkResource } from "@/interfaces/Network";
import ResourcesTable from "@/modules/networks/resources/ResourcesTable";

type ResourcesSectionProps = {
  network: Network;
};

// ResourcesSection — wraps ResourcesTable with the v2 section chrome
// (h2 + intro paragraph). Top padding pulls the section off the
// header card above so the spacing reads as a distinct row of the
// /network detail page.

export const ResourcesSection = ({ network }: ResourcesSectionProps) => {
  const { data: resources, isLoading } = useFetchApi<NetworkResource[]>(
    `/networks/${network.id}/resources`,
  );
  const { ref: headingRef, portalTarget } =
    usePortalElement<HTMLHeadingElement>();

  return (
    <section className="space-y-4 px-8 py-7">
      <header className="max-w-6xl">
        <h2
          ref={headingRef}
          className="text-[18px] font-semibold tracking-tight text-oz2-text"
        >
          Resources
        </h2>
        <p className="mt-1 max-w-2xl text-[13px] leading-[1.55] text-oz2-text-muted">
          Add and manage the addresses (single IP, subnet, or domain) that
          peers in this network are allowed to reach.
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
          <ResourcesTable
            isLoading={isLoading}
            headingTarget={portalTarget}
            resources={resources}
          />
        </Suspense>
      </div>
    </section>
  );
};
