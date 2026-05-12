"use client";

import { cn } from "@utils/helpers";
import { Disc3Icon, FlagIcon, NetworkIcon, ServerCogIcon } from "lucide-react";
import * as React from "react";
import OpenzroIcon from "@/assets/icons/OpenzroIcon";
import { PostureCheck } from "@/interfaces/PostureCheck";
import { GeoLocationTooltip } from "@/modules/posture-checks/checks/tooltips/GeoLocationTooltip";
import { OpenzroVersionTooltip } from "@/modules/posture-checks/checks/tooltips/OpenzroVersionTooltip";
import { OperatingSystemTooltip } from "@/modules/posture-checks/checks/tooltips/OperatingSystemTooltip";
import { PeerNetworkRangeTooltip } from "@/modules/posture-checks/checks/tooltips/PeerNetworkRangeTooltip";
import { ProcessTooltip } from "@/modules/posture-checks/checks/tooltips/ProcessTooltip";

// V2 paint of PostureCheckChecksCell — same overlapping-avatar stack,
// but the surrounding chip uses oz2-bg-sunken / oz2-border-soft and
// the per-check avatar tints stay on the brand violet (Openzro) plus
// the original semantic gradients (indigo geo, blue network, neutral
// OS/process) which read cleanly against the v2 surface.

type Props = {
  check: PostureCheck;
  children?: React.ReactNode;
  disableHover?: boolean;
  className?: string;
  onClick?: () => void;
};

export const PostureCheckChecksCellV2 = ({
  check,
  children,
  disableHover = false,
  className,
  onClick,
}: Props) => {
  return (
    <div className="flex" onClick={onClick}>
      <div
        className={cn(
          "flex items-center gap-3 rounded-full border border-oz2-border-soft bg-oz2-bg-sunken px-1 py-1 transition-colors",
          !disableHover && "hover:bg-oz2-hover hover:border-oz2-border",
          className,
        )}
      >
        <div className="flex -space-x-2">
          {check.checks.nb_version_check && (
            <OpenzroVersionTooltip
              version={check.checks.nb_version_check.min_version}
            >
              <div className="z-[10] flex h-8 w-8 items-center justify-center rounded-full bg-gradient-to-tr from-openzro-200 to-openzro-100 transition-transform hover:scale-[1.1]">
                <OpenzroIcon size={14} />
              </div>
            </OpenzroVersionTooltip>
          )}

          {check.checks.geo_location_check && (
            <GeoLocationTooltip check={check.checks.geo_location_check}>
              <div className="z-[9] flex h-8 w-8 items-center justify-center rounded-full bg-gradient-to-tr from-indigo-500 to-indigo-400 transition-transform hover:scale-[1.1]">
                <FlagIcon size={14} className="text-white" />
              </div>
            </GeoLocationTooltip>
          )}

          {check.checks.os_version_check && (
            <OperatingSystemTooltip check={check.checks.os_version_check}>
              <div className="z-[8] flex h-8 w-8 items-center justify-center rounded-full bg-gradient-to-tr from-neutral-500 to-neutral-400 transition-transform hover:scale-[1.1] dark:from-nb-gray-500 dark:to-nb-gray-300">
                <Disc3Icon size={14} className="text-white" />
              </div>
            </OperatingSystemTooltip>
          )}

          {check.checks.peer_network_range_check && (
            <PeerNetworkRangeTooltip
              check={check.checks.peer_network_range_check}
            >
              <div className="z-[7] flex h-8 w-8 items-center justify-center rounded-full bg-gradient-to-tr from-blue-500 to-blue-400 transition-transform hover:scale-[1.1]">
                <NetworkIcon size={14} className="text-white" />
              </div>
            </PeerNetworkRangeTooltip>
          )}

          {check.checks.process_check && (
            <ProcessTooltip check={check.checks.process_check}>
              <div className="z-[6] flex h-8 w-8 items-center justify-center rounded-full bg-gradient-to-tr from-neutral-500 to-neutral-400 transition-transform hover:scale-[1.1] dark:from-nb-gray-500 dark:to-nb-gray-300">
                <ServerCogIcon size={14} className="text-white" />
              </div>
            </ProcessTooltip>
          )}
        </div>
        {children}
      </div>
    </div>
  );
};
