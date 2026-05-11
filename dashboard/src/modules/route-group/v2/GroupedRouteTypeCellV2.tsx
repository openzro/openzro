"use client";

import GroupBadge from "@components/ui/GroupBadge";
import { MonitorSmartphoneIcon } from "lucide-react";
import * as React from "react";
import { useMemo } from "react";
import OzPill from "@/components/v2/OzPill";
import { useGroups } from "@/contexts/GroupsProvider";
import { GroupedRoute } from "@/interfaces/Route";

// V2 paint of GroupedRouteTypeCell — swaps the legacy gray Badge for
// OzPill default. The GroupBadge branch is preserved as-is (it carries
// its own color-dot + name visual identity used across the dashboard).

type Props = {
  groupedRoute: GroupedRoute;
};

export default function GroupedRouteTypeCellV2({ groupedRoute }: Props) {
  const { groups } = useGroups();

  const group = useMemo(() => {
    const firstRoute = groupedRoute.routes && groupedRoute.routes[0];
    if (!firstRoute) return undefined;
    const peerGroups = firstRoute.peer_groups;
    if (!peerGroups) return undefined;
    return groups?.find((g) => g.id === peerGroups[0]);
  }, [groupedRoute.routes, groups]);

  return (
    <div className="inline-flex">
      {group ? (
        <GroupBadge group={group} />
      ) : (
        <OzPill variant="default" className="min-w-[130px] justify-center">
          <MonitorSmartphoneIcon size={12} />
          Routing Peers
        </OzPill>
      )}
    </div>
  );
}
