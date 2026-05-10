"use client";

import InlineLink from "@components/InlineLink";
import SkeletonTable from "@components/skeletons/SkeletonTable";
import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import { usePortalElement } from "@hooks/usePortalElement";
import useFetchApi from "@utils/api";
import { ExternalLinkIcon } from "lucide-react";
import React, { lazy, Suspense } from "react";
import PeersProvider from "@/contexts/PeersProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import RoutesProvider from "@/contexts/RoutesProvider";
import { Route } from "@/interfaces/Route";
import useGroupedRoutes from "@/modules/route-group/useGroupedRoutes";

// /network-routes — v2 chrome entry. Body still renders the legacy
// NetworkRoutesTable; a deeper v2 paint of the table is deferred
// pending dedicated NetworkRoutesTableV2. The wrapper here only
// strips PageContainer + Breadcrumbs (now handled by
// V2DashboardLayout) and renders the page header/intro in v2 paint.

const NetworkRoutesTable = lazy(
  () => import("@/modules/route-group/NetworkRoutesTable"),
);

export default function NetworkRoutes() {
  const { permission } = usePermissions();
  const { data: routes, isLoading } = useFetchApi<Route[]>("/routes");
  const groupedRoutes = useGroupedRoutes({ routes });

  const { ref: headingRef, portalTarget } =
    usePortalElement<HTMLHeadingElement>();

  return (
    <RoutesProvider>
      <PeersProvider>
        <div className="space-y-6 p-8">
          <header>
            <h1
              ref={headingRef}
              className="text-[24px] font-semibold tracking-tight"
            >
              Network Routes
            </h1>
            <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
              Reach LANs and VPCs through a routing peer — no openZro client
              needed on every resource.{" "}
              <InlineLink
                href="https://docs.openzro.io/how-to/routing-traffic-to-private-networks"
                target="_blank"
              >
                Learn more
                <ExternalLinkIcon size={11} />
              </InlineLink>
            </p>
          </header>

          <RestrictedAccess hasAccess={permission.routes.read}>
            <Suspense fallback={<SkeletonTable />}>
              <NetworkRoutesTable
                isLoading={isLoading}
                groupedRoutes={groupedRoutes}
                routes={routes}
                headingTarget={portalTarget}
              />
            </Suspense>
          </RestrictedAccess>
        </div>
      </PeersProvider>
    </RoutesProvider>
  );
}
