"use client";

import { CommandItem } from "@components/Command";
import { Popover, PopoverContent, PopoverTrigger } from "@components/Popover";
import { ScrollArea } from "@components/ScrollArea";
import TextWithTooltip from "@components/ui/TextWithTooltip";
import { cn, generateColorFromString } from "@utils/helpers";
import { Command, CommandGroup, CommandInput, CommandList } from "cmdk";
import { sortBy, trim, uniqBy } from "lodash";
import { ChevronDown, Cog, SearchIcon, UserCircle2 } from "lucide-react";
import * as React from "react";
import { useMemo, useState } from "react";
import { useDebounce } from "@/hooks/useDebounce";
import { type UserSelectOption } from "@/modules/activity/UsersDropdownSelector";

// InitiatorSelectorV2 — handoff-flavored repaint of the legacy
// UsersDropdownSelector. Same single-select cmdk popover content
// (search + scrollable list, "All Users" reset row, External pill
// per option) — only the trigger flips to the v2 outline-button
// paint so the AuditTimelineV2 toolbar reads consistently.

interface Props {
  value?: string;
  onChange: (item: string | undefined) => void;
  options: UserSelectOption[];
  disabled?: boolean;
}

export default function InitiatorSelectorV2({
  value,
  onChange,
  options,
  disabled = false,
}: Props) {
  const searchRef = React.useRef<HTMLInputElement>(null);
  const [searchInput, setSearchInput] = useState("");
  const search = useDebounce(searchInput, 500);
  const [open, setOpen] = useState(false);

  const toggle = (item: string | undefined) => {
    const isSelected = value === item;
    if (isSelected) {
      onChange(undefined);
    } else {
      onChange(item);
      setSearchInput("");
    }
    setOpen(false);
  };

  const sortedOptions = useMemo(() => {
    return sortBy(
      uniqBy(options, (o) => o.email),
      ["external", "name"],
    );
  }, [options]);

  const selectedUser = useMemo(
    () => options.find((u) => u.email === value),
    [value, options],
  );

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
          {!selectedUser ? (
            <>
              <UserCircle2 size={13} className="shrink-0 text-oz2-text-faint" />
              <span>All users</span>
            </>
          ) : (
            <>
              <span
                className="grid h-5 w-5 flex-shrink-0 place-items-center rounded-full bg-oz2-bg-sunken text-[10px] font-semibold uppercase"
                style={{
                  color:
                    selectedUser.email === "Openzro"
                      ? "var(--ozv2-text-muted)"
                      : generateColorFromString(
                          selectedUser.name ||
                            selectedUser.id ||
                            selectedUser.email,
                        ),
                }}
              >
                {selectedUser.email === "Openzro" ? (
                  <Cog size={11} />
                ) : (
                  selectedUser.name?.charAt(0) ||
                  selectedUser.id?.charAt(0) ||
                  "?"
                )}
              </span>
              <TextWithTooltip
                text={
                  selectedUser.email === "Openzro"
                    ? "System"
                    : selectedUser.name || selectedUser.email
                }
                maxChars={18}
                className="leading-none"
              />
            </>
          )}
          <ChevronDown size={13} className="shrink-0 text-oz2-text-faint" />
        </button>
      </PopoverTrigger>
      <PopoverContent
        className="w-[300px] p-0"
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
                ref={searchRef}
                value={searchInput}
                onValueChange={setSearchInput}
                placeholder="Search user…"
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
                    value=""
                    className="px-2 py-1.5"
                    onSelect={() => toggle(undefined)}
                    onClick={(e) => e.preventDefault()}
                  >
                    <div className="flex items-center gap-2.5">
                      <span className="grid h-7 w-7 place-items-center rounded-full bg-oz2-acc text-oz2-text-on-acc">
                        <UserCircle2 size={15} />
                      </span>
                      <div className="flex flex-col text-[12.5px]">
                        <span className="text-oz2-text">All users</span>
                        <span className="text-oz2-text-muted">
                          Include every initiator
                        </span>
                      </div>
                    </div>
                  </CommandItem>

                  {sortedOptions.map((user) => {
                    const isSystem = user.email === "Openzro";
                    const searchValue = isSystem
                      ? "Openzro System"
                      : `${user.name || ""} ${user.id || ""} ${user.email || ""}`;
                    return (
                      <CommandItem
                        key={user.id || user.email}
                        value={searchValue}
                        className="px-2 py-1.5"
                        onSelect={() => toggle(user.email)}
                        onClick={(e) => e.preventDefault()}
                      >
                        <div className="flex w-full items-center gap-2.5">
                          <span
                            className="grid h-7 w-7 flex-shrink-0 place-items-center rounded-full bg-oz2-bg-sunken text-[11px] font-semibold uppercase"
                            style={{
                              color: isSystem
                                ? "var(--ozv2-text-muted)"
                                : generateColorFromString(
                                    user.name || user.id || user.email,
                                  ),
                            }}
                          >
                            {isSystem ? (
                              <Cog size={13} />
                            ) : (
                              user.name?.charAt(0) ||
                              user.id?.charAt(0) ||
                              user.email?.charAt(0) ||
                              "?"
                            )}
                          </span>
                          <div className="flex w-full min-w-0 flex-col text-[12.5px]">
                            <TextWithTooltip
                              text={
                                isSystem
                                  ? "System"
                                  : user.name || user.id || user.email
                              }
                              maxChars={18}
                              className="text-oz2-text"
                            />
                            <TextWithTooltip
                              text={user.email || "Openzro"}
                              maxChars={22}
                              className="text-oz2-text-muted"
                            />
                          </div>
                          {user.external && (
                            <span className="ml-auto inline-flex items-center rounded-full border border-oz2-border bg-oz2-bg-sunken px-2 py-0.5 font-mono text-[10px] uppercase tracking-wider text-oz2-text-muted">
                              External
                            </span>
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
