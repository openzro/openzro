import { DropdownInfoText } from "@components/DropdownInfoText";
import { DropdownInput } from "@components/DropdownInput";
import FullTooltip from "@components/FullTooltip";
import { Popover, PopoverContent, PopoverTrigger } from "@components/Popover";
import TextWithTooltip from "@components/ui/TextWithTooltip";
import { VirtualScrollAreaList } from "@components/VirtualScrollAreaList";
import { getOperatingSystem } from "@hooks/useOperatingSystem";
import { useSearch } from "@hooks/useSearch";
import useFetchApi from "@utils/api";
import { cn } from "@utils/helpers";
import { isRoutingPeerSupported } from "@utils/version";
import { sortBy, unionBy } from "lodash";
import { ArrowUpCircleIcon, ChevronsUpDown, MapPin } from "lucide-react";
import * as React from "react";
import { memo, useEffect, useState } from "react";
import { useElementSize } from "@/hooks/useElementSize";
import { OperatingSystem } from "@/interfaces/OperatingSystem";
import { Peer } from "@/interfaces/Peer";
import { OSLogo } from "@/modules/peers/PeerOSCell";

const MapPinIcon = memo(() => <MapPin size={12} />);
MapPinIcon.displayName = "MapPinIcon";

interface MultiSelectProps {
  value?: Peer;
  onChange: React.Dispatch<React.SetStateAction<Peer | undefined>>;
  excludedPeers?: string[];
  disabled?: boolean;
}

const searchPredicate = (item: Peer, query: string) => {
  const lowerCaseQuery = query.toLowerCase();
  if (item.name.toLowerCase().includes(lowerCaseQuery)) return true;
  if (item.hostname.toLowerCase().includes(lowerCaseQuery)) return true;
  return item.ip.toLowerCase().startsWith(lowerCaseQuery);
};

export function PeerSelector({
  onChange,
  value,
  excludedPeers,
  disabled = false,
}: MultiSelectProps) {
  const { data: peers } = useFetchApi<Peer[]>("/peers");
  const [inputRef, { width }] = useElementSize<HTMLButtonElement>();

  const [unfilteredItems, setUnfilteredItems] = useState<Peer[]>([]);
  const [filteredItems, search, setSearch] = useSearch(
    unfilteredItems,
    searchPredicate,
    { filter: true, debounce: 150 },
  );

  // Update unfiltered items when peers change
  useEffect(() => {
    if (!peers) return;

    // Sort
    let options = sortBy([...peers], "name") as Peer[];

    // Filter out excluded peers
    if (excludedPeers) {
      options = options.filter((peer) => {
        if (!peer.id) return false;
        return !excludedPeers.includes(peer.id);
      });
    }

    setUnfilteredItems(unionBy(options, unfilteredItems, "id"));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [peers]);

  const togglePeer = (peer: Peer) => {
    const isSelected = value && value.id == peer.id;
    if (isSelected) {
      onChange(undefined);
    } else {
      onChange(peer);
      setSearch("");
    }
    setOpen(false);
  };

  const [open, setOpen] = useState(false);

  return (
    <Popover
      open={open}
      onOpenChange={(isOpen) => {
        if (!isOpen) {
          setTimeout(() => {
            setSearch("");
          }, 100);
        }
        setOpen(isOpen);
      }}
    >
      <PopoverTrigger asChild>
        <button
          className={cn(
            "group min-h-[38px] w-full relative flex items-center justify-between gap-2",
            "rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 py-1.5 text-[13px] text-oz2-text-faint transition-colors",
            "hover:border-oz2-border-strong hover:bg-oz2-hover",
            "[outline:none] focus-visible:border-oz2-acc focus-visible:ring-2 focus-visible:ring-oz2-acc/30",
            "disabled:cursor-not-allowed disabled:pointer-events-none disabled:opacity-60",
          )}
          disabled={disabled}
          ref={inputRef}
        >
          <div className="flex w-full items-center gap-2 flex-wrap h-full">
            {value ? (
              <div className="flex w-full items-center justify-between gap-2 text-[13px] text-oz2-text">
                <div className="flex items-center gap-2.5">
                  <TextWithTooltip text={value.name} maxChars={22} />
                </div>
                <div className="flex items-center gap-1 font-mono text-[10px] font-medium text-oz2-text-muted">
                  <MapPinIcon />
                  {value.ip}
                </div>
              </div>
            ) : (
              <span>Select a peer...</span>
            )}
          </div>

          <ChevronsUpDown size={14} className="shrink-0 text-oz2-text-faint" />
        </button>
      </PopoverTrigger>
      <PopoverContent
        hideWhenDetached={false}
        className="w-full overflow-hidden rounded-oz2-card border border-oz2-border bg-oz2-bg-elev p-0 text-oz2-text shadow-oz2-md"
        style={{
          width: width,
        }}
        align="start"
        side={"top"}
        sideOffset={6}
      >
        <div className={"w-full"}>
          <DropdownInput
            value={search}
            onChange={setSearch}
            placeholder={"Search for peers by name or ip..."}
          />

          {unfilteredItems.length == 0 && !search && (
            <div className={"max-w-xs mx-auto"}>
              <DropdownInfoText>
                {"No peers available to select."}
              </DropdownInfoText>
            </div>
          )}

          {filteredItems.length == 0 && search != "" && (
            <DropdownInfoText>
              There are no peers matching your search.
            </DropdownInfoText>
          )}

          {filteredItems.length > 0 && (
            <VirtualScrollAreaList
              items={filteredItems}
              estimatedItemHeight={37}
              onSelect={(item) => {
                const isSupported = isRoutingPeerSupported(
                  item.version,
                  item.os,
                );
                if (!isSupported) return;
                togglePeer(item);
              }}
              renderItem={(option) => {
                const os = getOperatingSystem(option.os);
                const isSupported = isRoutingPeerSupported(
                  option.version,
                  option.os,
                );
                return (
                  <FullTooltip
                    disabled={isSupported}
                    interactive={false}
                    delayDuration={200}
                    skipDelayDuration={350}
                    className={"w-full flex items-center justify-between"}
                    content={
                      <div className={"max-w-[240px] text-xs"}>
                        Please update Openzro to at least{" "}
                        <span className={"text-openzro"}>v0.36.6</span> or later
                        to use this peer as a routing peer.
                      </div>
                    }
                  >
                    <div
                      className={cn(
                        "flex items-center gap-2.5 text-sm",
                        value && value.id == option.id
                          ? "text-white"
                          : "text-nb-gray-300",
                      )}
                    >
                      <div
                        className={cn(
                          "flex items-center justify-center grayscale brightness-[100%] contrast-[40%]",
                          "w-4 h-4 shrink-0",
                          os === OperatingSystem.WINDOWS && "p-[2.5px]",
                          os === OperatingSystem.APPLE && "p-[2.7px]",
                          os === OperatingSystem.FREEBSD && "p-[1.5px]",
                          !isSupported && "opacity-50",
                        )}
                      >
                        <OSLogo os={option.os} />
                      </div>

                      <div className={cn(!isSupported && "opacity-50")}>
                        <TextWithTooltip
                          text={option.name}
                          maxChars={22}
                          hideTooltip={!isSupported}
                        />
                      </div>
                      {!isSupported && (
                        <div className={"relative"}>
                          <span className="animate-ping absolute left-0 inline-flex h-[14px] w-[14px] rounded-full bg-openzro opacity-20"></span>
                          <ArrowUpCircleIcon
                            size={14}
                            className={"text-openzro"}
                          />
                        </div>
                      )}
                    </div>

                    <div
                      className={cn(
                        "font-medium flex items-center gap-1 font-mono text-[10px]",
                        value && value.id == option.id
                          ? "text-white"
                          : "text-nb-gray-300",
                        !isSupported && "opacity-50",
                      )}
                    >
                      <MapPinIcon />
                      {option.ip}
                    </div>
                  </FullTooltip>
                );
              }}
            />
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
