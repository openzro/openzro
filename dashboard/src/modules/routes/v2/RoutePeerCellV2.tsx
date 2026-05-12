"use client";

import DescriptionWithTooltip from "@components/ui/DescriptionWithTooltip";
import TextWithTooltip from "@components/ui/TextWithTooltip";
import { ArrowRightIcon } from "lucide-react";
import * as React from "react";
import { useMemo } from "react";
import OzGroupBadge from "@/components/v2/OzGroupBadge";
import OzPeerBadge from "@/components/v2/OzPeerBadge";
import OzStatusDot from "@/components/v2/OzStatusDot";
import { useGroups } from "@/contexts/GroupsProvider";
import { usePeers } from "@/contexts/PeersProvider";
import { Route } from "@/interfaces/Route";

// V2 paint of RoutePeerCell — swaps legacy ActiveInactiveRow +
// GroupBadge + PeerBadge for v2 status dot + name layout +
// OzGroupBadge / OzPeerBadge in the route-groups branch.

type Props = {
  route: Route;
};

export default function RoutePeerCellV2({ route }: Props) {
  const { peers } = usePeers();
  const { groups } = useGroups();

  const peer = useMemo(
    () => peers?.find((p) => p.id === route.peer),
    [peers, route.peer],
  );

  const group = useMemo(
    () =>
      groups?.find((g) => {
        if (route.peer_groups && route.peer_groups.length > 0) {
          return g.id === route.peer_groups[0];
        }
        return false;
      }),
    [groups, route.peer_groups],
  );

  return (
    <div className="flex min-w-[295px] max-w-[295px] items-center gap-2">
      {peer && (
        <div className="flex min-w-0 items-start gap-2.5">
          <OzStatusDot
            status={peer.connected ? "on" : "off"}
            className="mt-[5px] shrink-0"
          />
          <div className="flex min-w-0 flex-col">
            <div className="flex items-center gap-2 font-medium text-oz2-text">
              <TextWithTooltip text={peer.name} maxChars={25} />
            </div>
            <DescriptionWithTooltip className="mt-1" text={route.description} />
          </div>
        </div>
      )}

      {group && (
        <>
          <OzGroupBadge group={group} />
          <ArrowRightIcon size={14} className="shrink-0 text-oz2-text-faint" />
          <OzPeerBadge>{group.peers_count} Peer(s)</OzPeerBadge>
        </>
      )}
    </div>
  );
}
