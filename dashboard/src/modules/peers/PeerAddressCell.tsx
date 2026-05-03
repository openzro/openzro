import CopyToClipboardText from "@components/CopyToClipboardText";
import FullTooltip from "@components/FullTooltip";
import { cn } from "@utils/helpers";
import { isEmpty } from "lodash";
import { GlobeIcon } from "lucide-react";
import React from "react";
import RoundedFlag from "@/assets/countries/RoundedFlag";
import { Peer } from "@/interfaces/Peer";
import { PeerAddressTooltipContent } from "@/modules/peers/PeerAddressTooltipContent";

type Props = {
  peer: Peer;
};
export default function PeerAddressCell({ peer }: Props) {
  return (
    <FullTooltip
      side={"top"}
      interactive={true}
      delayDuration={250}
      skipDelayDuration={100}
      contentClassName={"p-0"}
      content={<PeerAddressTooltipContent peer={peer} />}
    >
      <div
        className={
          "flex gap-4 items-center min-w-[320px] max-w-[320px] group/cell transition-all py-2 px-3 rounded-md cursor-default " +
          "hover:bg-neutral-100 dark:hover:bg-nb-gray-800/10"
        }
        onClick={(e) => {
          e.stopPropagation();
          e.preventDefault();
        }}
      >
        <div
          className={cn(
            "flex items-center justify-center rounded-full h-8 w-8 shrink-0 transition-all",
            // Flag halo: neutral-200 on light + the existing
            // nb-gray-920/80 on dark. The dark token resolves to a
            // saturated mid-violet on light pages, which is what
            // produced the "circle around the flag" the user
            // flagged.
            "bg-neutral-200 dark:bg-nb-gray-920/80",
          )}
        >
          {isEmpty(peer.country_code) ? (
            <GlobeIcon
              size={16}
              className={"text-neutral-600 dark:text-nb-gray-300"}
            />
          ) : (
            <RoundedFlag country={peer.country_code} size={20} />
          )}
        </div>
        <div className="flex flex-col gap-0 dark:text-neutral-300 text-neutral-500 font-light truncate">
          <CopyToClipboardText
            message={"DNS label has been copied to your clipboard"}
          >
            <span className={"font-normal truncate"}>{peer.dns_label}</span>
          </CopyToClipboardText>
          <CopyToClipboardText
            message={"IP address has been copied to your clipboard"}
          >
            <span
              className={"dark:text-nb-gray-400 font-mono font-thin text-xs"}
            >
              {peer.ip}
            </span>
          </CopyToClipboardText>
        </div>
      </div>
    </FullTooltip>
  );
}
