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

type Props = {
  check: PostureCheck;
  children?: React.ReactNode;
  disableHover?: boolean;
  className?: string;
  onClick?: () => void;
};
export const PostureCheckChecksCell = ({
  check,
  children,
  disableHover = false,
  className,
  onClick,
}: Props) => {
  return (
    <div className={"flex"} onClick={onClick}>
      <div
        className={cn(
          "flex items-center gap-3 py-1 rounded-full px-1 transition-colors border",
          "bg-oz2-surface border-oz2-border-soft",
          !disableHover && "hover:bg-oz2-hover",
          className,
        )}
      >
        <div className={"flex -space-x-2 "}>
          {check.checks.nb_version_check && (
            <OpenzroVersionTooltip
              version={check.checks.nb_version_check.min_version}
            >
              <div
                className={cn(
                  "bg-gradient-to-tr from-openzro-200 to-openzro-100 h-8 w-8 rounded-full flex items-center justify-center relative z-[10] hover:scale-[1.1] transition-all",
                )}
              >
                <OpenzroIcon size={14} />
              </div>
            </OpenzroVersionTooltip>
          )}

          {check.checks.geo_location_check && (
            <GeoLocationTooltip check={check.checks.geo_location_check}>
              <div
                className={cn(
                  "bg-gradient-to-tr from-indigo-500 to-indigo-400 h-8 w-8 rounded-full flex items-center justify-center relative z-[9] hover:scale-[1.1] transition-all",
                )}
              >
                <FlagIcon size={14} />
              </div>
            </GeoLocationTooltip>
          )}

          {check.checks.os_version_check && (
            <OperatingSystemTooltip check={check.checks.os_version_check}>
              <div
                className={cn(
                  "bg-gradient-to-tr from-neutral-500 to-neutral-400 dark:from-nb-gray-500 dark:to-nb-gray-300 h-8 w-8 rounded-full flex items-center justify-center relative z-[8] hover:scale-[1.1] transition-all",
                )}
              >
                <Disc3Icon size={14} />
              </div>
            </OperatingSystemTooltip>
          )}

          {check.checks.peer_network_range_check && (
            <PeerNetworkRangeTooltip
              check={check.checks.peer_network_range_check}
            >
              <div
                className={cn(
                  "bg-gradient-to-tr from-blue-500 to-blue-400 h-8 w-8 rounded-full flex items-center justify-center relative z-[8] hover:scale-[1.1] transition-all",
                )}
              >
                <NetworkIcon size={14} />
              </div>
            </PeerNetworkRangeTooltip>
          )}

          {check.checks.process_check && (
            <ProcessTooltip check={check.checks.process_check}>
              <div
                className={cn(
                  "bg-gradient-to-tr from-neutral-500 to-neutral-400 dark:from-nb-gray-500 dark:to-nb-gray-300 h-8 w-8 rounded-full flex items-center justify-center relative z-[8] hover:scale-[1.1] transition-all",
                )}
              >
                <ServerCogIcon size={14} />
              </div>
            </ProcessTooltip>
          )}
        </div>
        {children}
      </div>
    </div>
  );
};
