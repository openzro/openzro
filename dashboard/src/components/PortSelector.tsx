import { Callout } from "@components/Callout";
import { Checkbox } from "@components/Checkbox";
import { CommandItem } from "@components/Command";
import { DropdownInfoText } from "@components/DropdownInfoText";
import { Popover, PopoverContent, PopoverTrigger } from "@components/Popover";
import { IconArrowBack } from "@tabler/icons-react";
import { cn } from "@utils/helpers";
import { Command, CommandGroup, CommandInput, CommandList } from "cmdk";
import { orderBy, trim } from "lodash";
import { ChevronsUpDown, SearchIcon, XIcon } from "lucide-react";
import * as React from "react";
import { useEffect, useMemo, useState } from "react";
import { useElementSize } from "@/hooks/useElementSize";
import { PortRange } from "@/interfaces/Policy";

interface MultiSelectProps {
  ports: number[];
  onPortsChange: React.Dispatch<React.SetStateAction<number[]>>;
  portRanges?: PortRange[];
  onPortRangesChange?: React.Dispatch<React.SetStateAction<PortRange[]>>;
  max?: number;
  disabled?: boolean;
  popoverWidth?: "auto" | number;
  showAll?: boolean;
}

const isValidPort = (p: number) => p >= 1 && p <= 65535;

const parseRange = (value: string): PortRange | undefined => {
  const parts = value.split("-").map((x) => Number(trim(x)));
  if (parts.length !== 2) return undefined;
  const [start, end] = parts;
  if (!isValidPort(start) || !isValidPort(end) || start >= end)
    return undefined;
  return { start, end };
};

const parsePortInput = (value: string): number | PortRange | undefined => {
  const trimmed = trim(value);
  if (/^\d{1,5}-\d{1,5}$/.test(trimmed)) return parseRange(trimmed);
  const port = Number(trimmed);
  return isValidPort(port) ? port : undefined;
};

export function PortSelector({
  onPortsChange,
  ports,
  portRanges = [],
  onPortRangesChange,
  disabled = false,
  popoverWidth = "auto",
  showAll = false,
}: Readonly<MultiSelectProps>) {
  const searchRef = React.useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [inputRef, { width }] = useElementSize<HTMLButtonElement>();
  const [search, setSearch] = useState("");

  const [portsInput, setPortsInput] = useState<string[]>(() => {
    const p = ports.map(String);
    const pr = portRanges.map((r) => {
      if (r.start === r.end) return String(r.start);
      return `${r.start}-${r.end}`;
    });
    return orderBy([...p, ...pr], [(x) => Number(x.split("-")[0])], ["asc"]);
  });

  useEffect(() => {
    const parsed = portsInput.map(parsePortInput).filter(Boolean);
    const newPorts: number[] = [];
    const newRanges: PortRange[] = [];
    parsed.forEach((entry) => {
      if (typeof entry === "number") newPorts.push(entry);
      else if (entry !== undefined) newRanges.push(entry);
    });
    onPortsChange(newPorts);
    onPortRangesChange?.(newRanges);
  }, [portsInput]);

  const toggle = (value: string) => {
    if (disabled) return;
    setPortsInput((prev) =>
      prev.includes(value) ? prev.filter((e) => e !== value) : [...prev, value],
    );
    setSearch("");
  };

  const notFound = useMemo(() => {
    const isSearching = search.length > 0;
    const trimmed = trim(search);
    return (
      trimmed &&
      !portsInput.includes(trimmed) &&
      parsePortInput(trimmed) &&
      isSearching
    );
  }, [search, portsInput]);

  return (
    <>
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
              "flex w-full min-h-[40px] items-center justify-between gap-2 py-1.5 px-3",
              "rounded-oz2-input border border-oz2-border bg-oz2-surface text-[13px] text-oz2-text-muted",
              "transition-colors hover:border-oz2-border-strong",
              "disabled:cursor-not-allowed disabled:opacity-50",
            )}
            data-cy={"port-selector"}
            disabled={disabled}
            ref={inputRef}
          >
            <div className="flex flex-wrap items-center gap-1.5">
              {portsInput.length === 0 && showAll && (
                <span className="inline-flex items-center rounded-[6px] border border-oz2-border-soft bg-oz2-bg-sunken px-1.5 py-0.5 font-mono text-[10.5px] font-medium uppercase tracking-[0.06em] text-oz2-text-muted">
                  All
                </span>
              )}

              {portsInput.map((x) => (
                <span
                  key={x}
                  onClick={(e) => {
                    e.stopPropagation();
                    e.preventDefault();
                    toggle(x);
                  }}
                  className="group inline-flex cursor-pointer items-center gap-1 rounded-[6px] bg-oz2-acc-soft px-1.5 py-0.5 font-mono text-[11px] font-medium text-oz2-acc-text transition-colors hover:bg-oz2-acc-soft-2"
                >
                  {x}
                  <XIcon
                    size={11}
                    className="text-oz2-acc-text/70 transition-colors group-hover:text-oz2-acc-text"
                  />
                </span>
              ))}
              {ports.length == 0 && <span>Select ports...</span>}
            </div>

            <ChevronsUpDown size={14} className="shrink-0 text-oz2-text-faint" />
          </button>
        </PopoverTrigger>
        <PopoverContent
          className="overflow-hidden rounded-oz2-card border border-oz2-border bg-oz2-bg-elev p-0 text-oz2-text shadow-oz2-md"
          style={{
            width: popoverWidth === "auto" ? width : popoverWidth,
          }}
          align="start"
          side={"top"}
          sideOffset={10}
        >
          <Command
            className={"w-full flex"}
            loop
            filter={(value, search) => {
              const formatValue = trim(value.toLowerCase());
              const formatSearch = trim(search.toLowerCase());
              if (formatValue.includes(formatSearch)) return 1;
              return 0;
            }}
          >
            <CommandList className={"w-full"}>
              <div className="relative border-b border-oz2-border-soft">
                <CommandInput
                  className={cn(
                    "h-10 w-full bg-transparent text-[13px] text-oz2-text outline-none",
                    "placeholder:text-oz2-text-faint",
                    "pl-10 pr-12",
                  )}
                  data-cy={"port-input"}
                  ref={searchRef}
                  value={search}
                  onValueChange={setSearch}
                  placeholder={
                    'Add a port or a range e.g. 80 or 1-1023 and press "Enter" to add...'
                  }
                />
                <div className="pointer-events-none absolute left-0 top-0 flex h-full items-center pl-4 text-oz2-text-faint">
                  <SearchIcon size={14} />
                </div>
                <div className="absolute right-0 top-0 flex h-full items-center pr-3">
                  <kbd
                    className="inline-flex items-center gap-1 rounded-[5px] border border-oz2-border-soft bg-oz2-bg-sunken px-1.5 py-[3px] font-mono text-[10.5px] text-oz2-text-faint"
                    aria-label="Press Enter to add"
                  >
                    <IconArrowBack size={10} />
                  </kbd>
                </div>
              </div>

              <div
                className={cn(
                  "flex flex-col gap-2",
                  portsInput.length != 0 && "p-2",
                  portsInput.length != 0 && search && "p-2",
                  notFound && "p-2",
                )}
              >
                {!notFound && search && !portsInput.includes(search) && (
                  <div className={"text-sm"}>
                    <DropdownInfoText className={"mb-[18px] pt-[4px]"}>
                      {
                        "Please add a valid port or port range (e.g. 80, 443, 1-1023)"
                      }
                    </DropdownInfoText>
                  </div>
                )}

                {notFound && (
                  <CommandGroup>
                    <div className="max-h-[180px] overflow-y-auto px-2 py-2">
                      <CommandItem
                        key={search}
                        onSelect={() => {
                          toggle(search);
                          searchRef.current?.focus();
                        }}
                        value={search}
                        onClick={(e) => e.preventDefault()}
                      >
                        <span className="inline-flex items-center rounded-[6px] bg-oz2-acc-soft px-1.5 py-0.5 font-mono text-[11px] font-medium text-oz2-acc-text">
                          {search}
                        </span>
                        <div className="text-[12px] text-oz2-text-muted">
                          Add this port or range by pressing{" "}
                          <span className="font-semibold text-oz2-acc-text">
                            {"'Enter'"}
                          </span>
                        </div>
                      </CommandItem>
                    </div>
                  </CommandGroup>
                )}

                <CommandGroup>
                  <div className="flex max-h-[180px] flex-col gap-1 overflow-y-auto px-2 py-2">
                    {portsInput.map((option) => {
                      const isSelected = portsInput.includes(option);
                      return (
                        <CommandItem
                          key={option}
                          value={option.toString()}
                          onSelect={() => {
                            toggle(option);
                            searchRef.current?.focus();
                          }}
                          onClick={(e) => e.preventDefault()}
                        >
                          <span className="inline-flex items-center rounded-[6px] bg-oz2-acc-soft px-1.5 py-0.5 font-mono text-[11px] font-medium text-oz2-acc-text">
                            {option}
                          </span>
                          <Checkbox checked={isSelected} />
                        </CommandItem>
                      );
                    })}
                  </div>
                </CommandGroup>
              </div>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
      {portRanges?.length > 0 && (
        <Callout variant={"info"} className={"mt-4"}>
          Port ranges requires Openzro client{" "}
          <span className="font-medium text-oz2-text">v0.48</span> or higher.
        </Callout>
      )}
    </>
  );
}
