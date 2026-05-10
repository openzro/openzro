"use client";

import { Checkbox } from "@components/Checkbox";
import { CommandItem } from "@components/Command";
import { Popover, PopoverContent, PopoverTrigger } from "@components/Popover";
import { ScrollArea } from "@components/ScrollArea";
import { cn } from "@utils/helpers";
import { Command, CommandGroup, CommandInput, CommandList } from "cmdk";
import { trim, uniqBy } from "lodash";
import { ChevronDown, Layers, SearchIcon } from "lucide-react";
import * as React from "react";
import { useMemo, useState } from "react";
import { ActivityEvent } from "@/interfaces/ActivityEvent";
import ActivityTypeIcon from "@/modules/activity/ActivityTypeIcon";

// EventCodeSelectorV2 — handoff-flavored repaint of
// ActivityEventCodeSelector. Same multi-select grouped command-menu
// content (cmdk + Checkbox per code, grouped by code prefix) — only
// the Popover trigger flips to the v2 outline-button paint so the
// AuditTimelineV2 toolbar reads consistently.

interface Props {
  values: string[];
  onChange: (items: string[]) => void;
  events: ActivityEvent[];
  disabled?: boolean;
}

export default function EventCodeSelectorV2({
  values,
  onChange,
  events,
  disabled = false,
}: Props) {
  const searchRef = React.useRef<HTMLInputElement>(null);
  const [search, setSearch] = useState("");
  const [open, setOpen] = useState(false);

  const toggle = (code: string) => {
    const isSelected = values.includes(code);
    if (isSelected) {
      onChange(values.filter((c) => c !== code));
    } else {
      onChange([...values, code]);
      setSearch("");
    }
  };

  const groupedEventNames = useMemo(() => {
    const uniqueCodes = uniqBy(events, (event) => event.activity_code);
    const items = uniqueCodes.map((event) => ({
      activity_code: event.activity_code,
      activity: event.activity,
      group: event.activity_code.split(".")[0],
    }));
    return items.reduce<Record<string, typeof items>>((acc, item) => {
      const { group } = item;
      if (!acc[group]) acc[group] = [];
      acc[group].push(item);
      return acc;
    }, {});
  }, [events]);

  return (
    <Popover
      open={open}
      onOpenChange={(isOpen) => {
        if (!isOpen) setTimeout(() => setSearch(""), 100);
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
          <Layers size={13} className="shrink-0 text-oz2-text-faint" />
          <span>
            {values.length > 0
              ? `${values.length} event${values.length === 1 ? "" : "s"}`
              : "All event types"}
          </span>
          <ChevronDown size={13} className="shrink-0 text-oz2-text-faint" />
        </button>
      </PopoverTrigger>
      <PopoverContent
        className="w-[400px] p-0"
        align="start"
        side="bottom"
        sideOffset={6}
      >
        <Command
          className="flex w-full"
          loop
          filter={(value, q) => {
            const v = trim(value.toLowerCase());
            const s = trim(q.toLowerCase());
            return v.includes(s) ? 1 : 0;
          }}
        >
          <CommandList className="w-full">
            <div className="relative border-b border-oz2-border-soft">
              <CommandInput
                ref={searchRef}
                value={search}
                onValueChange={setSearch}
                placeholder="Search event…"
                className="h-10 w-full bg-transparent pl-10 pr-3 text-[13px] outline-none placeholder:text-oz2-text-faint focus-visible:outline-none"
              />
              <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-oz2-text-faint">
                <SearchIcon size={14} />
              </span>
            </div>
            <ScrollArea className="flex max-h-[380px] flex-col gap-1 overflow-y-hidden py-2 pl-2 pr-3">
              {Object.keys(groupedEventNames).map((group) => {
                const groupItems = groupedEventNames[group];
                return (
                  <CommandGroup key={group}>
                    <div className="mb-3">
                      <p className="mb-0.5 pb-1 pl-2 font-mono text-[10.5px] font-medium uppercase tracking-widest text-oz2-text-faint">
                        {group}
                      </p>
                      <div className="grid grid-cols-1 gap-1 pl-1">
                        {groupItems.map((event) => {
                          const code = event.activity_code;
                          const label = event.activity;
                          const isSelected = values.includes(code);
                          return (
                            <CommandItem
                              key={code}
                              value={code}
                              className="p-1.5"
                              onSelect={() => {
                                toggle(code);
                                searchRef.current?.focus();
                              }}
                              onClick={(e) => e.preventDefault()}
                            >
                              <div className="flex items-center gap-2 text-oz2-text-2">
                                <Checkbox checked={isSelected} />
                                <span className="inline-flex items-center gap-2 whitespace-nowrap text-[12.5px]">
                                  <ActivityTypeIcon code={code} size={13} />
                                  {label}
                                </span>
                              </div>
                            </CommandItem>
                          );
                        })}
                      </div>
                    </div>
                  </CommandGroup>
                );
              })}
            </ScrollArea>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
