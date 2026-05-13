import { cn } from "@utils/helpers";
import {
  CalendarClock,
  Disc3Icon,
  FlagIcon,
  NetworkIcon,
  ServerCogIcon,
} from "lucide-react";
import * as React from "react";
import OpenzroIcon from "@/assets/icons/OpenzroIcon";
import { PostureCheck } from "@/interfaces/PostureCheck";
import { GeoLocationTooltip } from "@/modules/posture-checks/checks/tooltips/GeoLocationTooltip";
import { OpenzroVersionTooltip } from "@/modules/posture-checks/checks/tooltips/OpenzroVersionTooltip";
import { OperatingSystemTooltip } from "@/modules/posture-checks/checks/tooltips/OperatingSystemTooltip";
import { PeerNetworkRangeTooltip } from "@/modules/posture-checks/checks/tooltips/PeerNetworkRangeTooltip";
import { ProcessTooltip } from "@/modules/posture-checks/checks/tooltips/ProcessTooltip";
import { ScheduleTooltip } from "@/modules/posture-checks/checks/tooltips/ScheduleTooltip";

type Props = {
  check: PostureCheck;
  children?: React.ReactNode;
  disableHover?: boolean;
  className?: string;
  onClick?: () => void;
};

// Shared shape for each posture-check pill in the stack: 32x32 circle
// with a 2px white-ish ring so adjacent pills look layered when they
// overlap (-space-x-2 on the parent). Per-pill background + foreground
// is appended at the call site.
const pillBase =
  "relative grid h-8 w-8 place-items-center rounded-full ring-2 ring-oz2-surface transition-colors";

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
        <div className={"flex -space-x-2"}>
          {check.checks.nb_version_check && (
            <OpenzroVersionTooltip
              version={check.checks.nb_version_check.min_version}
            >
              <div className={cn(pillBase, "z-[10] bg-oz2-acc-soft text-oz2-acc-text")}>
                <OpenzroIcon size={14} />
              </div>
            </OpenzroVersionTooltip>
          )}

          {check.checks.geo_location_check && (
            <GeoLocationTooltip check={check.checks.geo_location_check}>
              <div
                className={cn(
                  pillBase,
                  "z-[9] bg-indigo-100 text-indigo-700 dark:bg-indigo-500/15 dark:text-indigo-300",
                )}
              >
                <FlagIcon size={14} />
              </div>
            </GeoLocationTooltip>
          )}

          {check.checks.os_version_check && (
            <OperatingSystemTooltip check={check.checks.os_version_check}>
              <div className={cn(pillBase, "z-[8] bg-oz2-bg-sunken text-oz2-text-2")}>
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
                  pillBase,
                  "z-[7] bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300",
                )}
              >
                <NetworkIcon size={14} />
              </div>
            </PeerNetworkRangeTooltip>
          )}

          {check.checks.process_check && (
            <ProcessTooltip check={check.checks.process_check}>
              <div className={cn(pillBase, "z-[6] bg-oz2-bg-sunken text-oz2-text-2")}>
                <ServerCogIcon size={14} />
              </div>
            </ProcessTooltip>
          )}

          {check.checks.schedule_check && (
            <ScheduleTooltip check={check.checks.schedule_check}>
              <div
                className={cn(
                  pillBase,
                  "z-[5] bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300",
                )}
              >
                <CalendarClock size={14} />
              </div>
            </ScheduleTooltip>
          )}
        </div>
        {children}
      </div>
    </div>
  );
};
