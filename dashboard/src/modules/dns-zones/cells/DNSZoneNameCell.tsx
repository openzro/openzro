import React from "react";
import { DNSZone } from "@/interfaces/DNSZone";
import ActiveInactiveRow from "@/modules/common-table-rows/ActiveInactiveRow";

type Props = {
  zone: DNSZone;
};

export default function DNSZoneNameCell({ zone }: Props) {
  const enabled = zone.enabled ?? true;
  return (
    <div className={"flex min-w-[270px] max-w-[270px]"}>
      <div
        className={
          "flex items-center gap-2 dark:text-neutral-300 text-neutral-500 hover:text-neutral-900 dark:hover:text-neutral-100 transition-all hover:bg-neutral-100 dark:hover:bg-nb-gray-800/60 py-2 px-3 rounded-md cursor-pointer"
        }
      >
        <ActiveInactiveRow
          active={enabled}
          inactiveDot={"gray"}
          text={zone.name}
        >
          <span className="mt-1 font-mono text-[11.5px] text-oz2-text-faint">
            {zone.domain}
          </span>
        </ActiveInactiveRow>
      </div>
    </div>
  );
}
