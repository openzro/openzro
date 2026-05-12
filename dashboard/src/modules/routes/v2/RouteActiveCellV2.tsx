"use client";

import React, { useMemo } from "react";
import { useSWRConfig } from "swr";
import OzSwitch from "@/components/v2/OzSwitch";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { useRoutes } from "@/contexts/RoutesProvider";
import { Route } from "@/interfaces/Route";

// V2 paint of RouteActiveCell — swaps legacy ToggleSwitch for OzSwitch
// (sm). Behavior preserved: optimistic update via useRoutes, success
// toast text mirrors the legacy.

type Props = {
  route: Route;
};

export default function RouteActiveCellV2({ route }: Readonly<Props>) {
  const { permission } = usePermissions();
  const { updateRoute } = useRoutes();
  const { mutate } = useSWRConfig();

  const update = async (enabled: boolean) => {
    updateRoute(
      route,
      { enabled },
      () => {
        mutate("/routes");
      },
      enabled
        ? "The network route was successfully enabled"
        : "The network route was successfully disabled",
    );
  };

  const isChecked = useMemo(() => route.enabled, [route]);

  return (
    <div className="flex">
      <OzSwitch
        size="sm"
        checked={isChecked}
        onCheckedChange={(next) => update(next)}
        disabled={!permission.routes.update}
      />
    </div>
  );
}
