"use client";

import { CommandItem } from "@components/Command";
import { Popover, PopoverContent, PopoverTrigger } from "@components/Popover";
import { ScrollArea } from "@components/ScrollArea";
import TextWithTooltip from "@components/ui/TextWithTooltip";
import { cn } from "@utils/helpers";
import { Command, CommandGroup, CommandInput, CommandList } from "cmdk";
import { trim } from "lodash";
import {
  ActivityIcon,
  Check,
  ChevronDown,
  SearchIcon,
} from "lucide-react";
import * as React from "react";
import { useState } from "react";
import RoundedFlag from "@/assets/countries/RoundedFlag";
import { OSLogo } from "@/modules/peers/PeerOSCell";

// PeerFilterV2 — handoff-flavored peer filter for /events/network-
// traffic. Same single-select cmdk dropdown semantics as the legacy
// PeerFilterDropdown (search by name + hostname + IP, "All peers"
// reset row), only the trigger flips to v2 paint matching the rest
// of the AuditTimelineV2-style toolbar.

export type PeerFilterOption = {
  id: string;
  name: string;
  hostname: string;
  ip: string;
  os: string;
  countryCode: string;
};

interface Props {
  value?: string;
  options: PeerFilterOption[];
  onChange: (id: string | undefined) => void;
  disabled?: boolean;
}

export default function PeerFilterV2({
  value,
  options,
  onChange,
  disabled = false,
}: Props) {
  const [open, setOpen] = useState(false);
  const [searchInput, setSearchInput] = useState("");
  const selected = options.find((o) => o.id === value);

  const toggle = (id: string | undefined) => {
    const isSelected = value === id;
    onChange(isSelected ? undefined : id);
    setSearchInput("");
    setOpen(false);
  };

  return (
    <Popover
      open={open}
      onOpenChange={(isOpen) => {
        if (!isOpen) setTimeout(() => setSearchInput(""), 100);
        setOpen(isOpen);
      }}
    >
      <PopoverTrigger asChild>
        <button
          type="button"
          disabled={disabled}
          className={cn(
            "inline-flex h-8 items-center gap-2 whitespace-nowrap rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 text-[13px] font-medium text-oz2-text-2 transition-colors",
            "hover:bg-oz2-hover hover:border-oz2-border-strong",
            "disabled:cursor-not-allowed disabled:opacity-50",
          )}
        >
          {!selected ? (
            <>
              <ActivityIcon
                size={13}
                className="shrink-0 text-oz2-text-faint"
              />
              <span>All peers</span>
            </>
          ) : (
            <>
              <PeerMiniAvatar peer={selected} />
              <TextWithTooltip
                text={selected.name}
                maxChars={18}
                className="leading-none"
              />
            </>
          )}
          <ChevronDown size={13} className="shrink-0 text-oz2-text-faint" />
        </button>
      </PopoverTrigger>
      <PopoverContent
        className="w-[320px] p-0"
        align="start"
        side="bottom"
        sideOffset={6}
      >
        <Command
          className="flex w-full"
          loop
          filter={(v, q) => {
            const fv = trim(v.toLowerCase());
            const fq = trim(q.toLowerCase());
            return fv.includes(fq) ? 1 : 0;
          }}
        >
          <CommandList className="w-full">
            <div className="relative border-b border-oz2-border-soft">
              <CommandInput
                value={searchInput}
                onValueChange={setSearchInput}
                placeholder="Search peers…"
                className="h-10 w-full bg-transparent pl-10 pr-3 text-[13px] outline-none placeholder:text-oz2-text-faint focus-visible:outline-none"
              />
              <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-oz2-text-faint">
                <SearchIcon size={14} />
              </span>
            </div>
            <ScrollArea className="flex max-h-[380px] flex-col gap-1 overflow-y-hidden py-2 pl-2 pr-3">
              <CommandGroup>
                <div className="grid grid-cols-1 gap-1">
                  <CommandItem
                    value="all peers"
                    className="px-2 py-1.5"
                    onSelect={() => toggle(undefined)}
                    onClick={(e) => e.preventDefault()}
                  >
                    <div className="flex items-center gap-2.5">
                      <span className="grid h-7 w-7 place-items-center rounded-full bg-oz2-acc text-oz2-text-on-acc">
                        <ActivityIcon size={14} />
                      </span>
                      <div className="flex flex-col text-[12.5px]">
                        <span className="text-oz2-text">All peers</span>
                        <span className="text-oz2-text-muted">
                          Include every reporting peer
                        </span>
                      </div>
                    </div>
                  </CommandItem>

                  {options.map((opt) => {
                    const haystack = [opt.name, opt.hostname, opt.ip, opt.id]
                      .filter(Boolean)
                      .join(" ");
                    const isSelected = value === opt.id;
                    return (
                      <CommandItem
                        key={opt.id}
                        value={haystack}
                        className="px-2 py-1.5"
                        onSelect={() => toggle(opt.id)}
                        onClick={(e) => e.preventDefault()}
                      >
                        <div className="flex w-full items-center gap-2.5">
                          <PeerOptionAvatar peer={opt} />
                          <div className="flex w-full min-w-0 flex-col text-[12.5px]">
                            <TextWithTooltip
                              text={opt.name}
                              maxChars={20}
                              className="text-oz2-text"
                            />
                            <span className="font-mono text-[11px] text-oz2-text-muted">
                              {opt.ip}
                            </span>
                          </div>
                          {isSelected && (
                            <Check
                              size={14}
                              className="ml-auto text-oz2-acc"
                            />
                          )}
                        </div>
                      </CommandItem>
                    );
                  })}
                </div>
              </CommandGroup>
            </ScrollArea>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

// 18px avatar for the trigger pill (compact). Keeps the OS logo +
// country flag overlay the legacy PeerAvatar uses but smaller.
function PeerMiniAvatar({ peer }: { peer: PeerFilterOption }) {
  return (
    <span className="relative inline-flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-md border border-oz2-border bg-oz2-bg-sunken">
      <span className="grid h-3 w-3 place-items-center">
        <OSLogo os={peer.os} />
      </span>
      {peer.countryCode && (
        <span className="absolute -bottom-1 -right-1">
          <RoundedFlag country={peer.countryCode} size={9} />
        </span>
      )}
    </span>
  );
}

// 28px avatar for the dropdown options (handoff list density).
function PeerOptionAvatar({ peer }: { peer: PeerFilterOption }) {
  return (
    <span className="relative inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md border border-oz2-border bg-oz2-bg-sunken">
      <span className="grid h-4 w-4 place-items-center">
        <OSLogo os={peer.os} />
      </span>
      {peer.countryCode && (
        <span className="absolute -bottom-1 -right-1">
          <RoundedFlag country={peer.countryCode} size={11} />
        </span>
      )}
    </span>
  );
}
