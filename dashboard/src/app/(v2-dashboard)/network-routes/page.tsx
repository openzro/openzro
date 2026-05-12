"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import useFetchApi from "@utils/api";
import React, { lazy, Suspense } from "react";
import PeersProvider from "@/contexts/PeersProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import RoutesProvider from "@/contexts/RoutesProvider";
import { Route } from "@/interfaces/Route";
import useGroupedRoutes from "@/modules/route-group/useGroupedRoutes";

const NetworkRoutesTableV2 = lazy(
  () => import("@/modules/route-group/v2/NetworkRoutesTableV2"),
);

export default function NetworkRoutes() {
  const { permission } = usePermissions();
  const { data: routes, isLoading } = useFetchApi<Route[]>("/routes");
  const groupedRoutes = useGroupedRoutes({ routes });

  return (
    <RoutesProvider>
      <PeersProvider>
        <div className="space-y-6 p-8">
          <header>
            <h1 className="text-[24px] font-semibold tracking-tight">
              Network Routes
            </h1>
            <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
              Reach LANs and VPCs through a routing peer — no openZro client
              needed on every resource.{" "}
              <a
                href="https://docs.openzro.io/how-to/routing-traffic-to-private-networks"
                target="_blank"
                rel="noopener noreferrer"
                className="text-oz2-acc-text underline-offset-2 hover:underline"
              >
                Learn more
              </a>
              .
            </p>
          </header>

          <RestrictedAccess hasAccess={permission.routes.read}>
            <Suspense fallback={null}>
              <NetworkRoutesTableV2
                isLoading={isLoading}
                groupedRoutes={groupedRoutes}
                routes={routes}
              />
            </Suspense>
          </RestrictedAccess>
        </div>
      </PeersProvider>
    </RoutesProvider>
  );
}
