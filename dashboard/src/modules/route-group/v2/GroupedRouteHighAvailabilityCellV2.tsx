"use client";

import FullTooltip from "@components/FullTooltip";
import { HelpCircle, PlusCircle } from "lucide-react";
import { useRouter } from "next/navigation";
import * as React from "react";
import { useMemo } from "react";
import PeerIcon from "@/assets/icons/PeerIcon";
import OzPill from "@/components/v2/OzPill";
import OzStatusDot from "@/components/v2/OzStatusDot";
import { GroupedRoute } from "@/interfaces/Route";
import { useAddRoutingPeer } from "@/modules/routes/RouteAddRoutingPeerProvider";

// V2 paint of GroupedRouteHighAvailabilityCell — swaps the legacy
// green/gray Badge for OzPill and the legacy "xs" Button for a
// compact v2 inline button. Logic (status text per state, modal
// opener, route navigation) is preserved verbatim.

type Props = {
  groupedRoute: GroupedRoute;
};

export default function GroupedRouteHighAvailabilityCellV2({
  groupedRoute,
}: Props) {
  const router = useRouter();
  const { openAddRoutingPeerModal } = useAddRoutingPeer();

  const isActive = useMemo(
    () => groupedRoute.high_availability_count > 1,
    [groupedRoute.high_availability_count],
  );

  const disabledText = useMemo(
    () => (
      <>
        High availability is currently{" "}
        <span className="font-medium text-oz2-err">disabled</span> for this
        route.
      </>
    ),
    [],
  );

  const enabledText = useMemo(
    () => (
      <>
        High availability is{" "}
        <span className="font-medium text-oz2-ok">enabled</span> for this route.
      </>
    ),
    [],
  );

  return (
    <FullTooltip
      interactive={false}
      content={
        <div className="max-w-xs text-xs">
          {!isActive && !groupedRoute.is_using_route_groups && (
            <>
              {disabledText}
              <div className="mt-2 inline-flex">
                Go ahead and add more routing peers to enable high availability
                for this network route.
              </div>
            </>
          )}
          {isActive && !groupedRoute.is_using_route_groups && (
            <>
              {enabledText}
              <div className="mt-2 inline-flex">
                You can add more peers to increase the availability of this
                network route.
              </div>
            </>
          )}
          {!isActive && groupedRoute.is_using_route_groups && (
            <>
              {disabledText}
              <div className="mt-2 inline-flex">
                To configure, you must add more peers to a group in this route.
                You can do it in the Peers menu.
              </div>
            </>
          )}
          {isActive && groupedRoute.is_using_route_groups && (
            <>
              {enabledText}
              <div className="mt-2 inline-flex">
                You can add more peers to a group in this route by going to the
                peers page.
              </div>
            </>
          )}
        </div>
      }
    >
      <div className="flex items-center gap-3">
        <OzPill
          variant={isActive ? "ok" : "default"}
          className={
            "min-h-[28px] min-w-[110px] justify-center " +
            (isActive ? "" : "opacity-60")
          }
        >
          {isActive ? (
            <>
              <OzStatusDot status="on" className="!h-[6px] !w-[6px]" />
              {groupedRoute.high_availability_count} Peer(s)
            </>
          ) : (
            <>
              <OzStatusDot status="off" className="!h-[6px] !w-[6px]" />
              Disabled
            </>
          )}
          <HelpCircle size={12} className="opacity-70" />
        </OzPill>
        {groupedRoute.is_using_route_groups ? (
          <InlineActionButton onClick={() => router.push("/peers")}>
            <PeerIcon size={12} />
            Go to Peers
          </InlineActionButton>
        ) : (
          <InlineActionButton
            onClick={() => openAddRoutingPeerModal(groupedRoute)}
          >
            <PlusCircle size={12} />
            Add Peer
          </InlineActionButton>
        )}
      </div>
    </FullTooltip>
  );
}

// Small v2 inline action button used inside row cells. Matches the
// v2 outline-button token (border, surface, hover) but at a compact
// h-7 / text-[12.5px] size suited for row density. Built inline
// because OzButton's base is h-[34px] (handoff primary-toolbar size).
function InlineActionButton({
  onClick,
  children,
}: {
  onClick: (e: React.MouseEvent<HTMLButtonElement>) => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation();
        onClick(e);
      }}
      className="inline-flex h-7 min-w-[120px] items-center justify-center gap-1.5 rounded-oz2-input border border-oz2-border bg-oz2-surface px-2.5 text-[12.5px] font-medium text-oz2-text-2 transition-colors hover:bg-oz2-hover hover:border-oz2-border-strong"
    >
      {children}
    </button>
  );
}
