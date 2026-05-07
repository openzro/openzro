"use client";

import Button from "@components/Button";
import { CommandItem } from "@components/Command";
import { DatePickerWithRange } from "@components/DatePickerWithRange";
import { Input } from "@components/Input";
import { Popover, PopoverContent, PopoverTrigger } from "@components/Popover";
import { ScrollArea } from "@components/ScrollArea";
import SkeletonTable from "@components/skeletons/SkeletonTable";
import DataTableRefreshButton from "@components/table/DataTableRefreshButton";
import TextWithTooltip from "@components/ui/TextWithTooltip";
import {
  IconSortAscending,
  IconSortDescending,
} from "@tabler/icons-react";
import useFetchApi from "@utils/api";
import { cn } from "@utils/helpers";
import { Command, CommandGroup, CommandInput, CommandList } from "cmdk";
import dayjs from "dayjs";
import { trim } from "lodash";
import {
  ActivityIcon,
  ArrowDown,
  ArrowUp,
  Ban,
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronsUpDown,
  ExternalLinkIcon,
  GlobeIcon,
  Play,
  RowsIcon,
  SearchIcon,
  ShieldCheckIcon,
  Square,
  XIcon,
} from "lucide-react";
import Link from "next/link";
import React, { useMemo, useState } from "react";
import { DateRange } from "react-day-picker";
import RoundedFlag from "@/assets/countries/RoundedFlag";
import { usePeers } from "@/contexts/PeersProvider";
import {
  NetworkTrafficEvent,
  NetworkTrafficEventsResponse,
} from "@/interfaces/NetworkTrafficEvent";
import { NetworkResource } from "@/interfaces/Network";
import { Peer } from "@/interfaces/Peer";
import { Policy } from "@/interfaces/Policy";
import { OSLogo } from "@/modules/peers/PeerOSCell";

type FlowGroup = {
  flowID: string;
  events: NetworkTrafficEvent[];
  reportingPeer?: Peer;
  sourcePeer?: Peer;
  destPeer?: Peer;
  // Network Resource on the destination side. Populated when the
  // flow event carries a non-empty dest_resource_id and we can match
  // it against /api/networks/resources. Used to render the resource's
  // address (e.g. ci.example.com) on the destination pill instead of
  // the bare IP labelled "external".
  destResource?: NetworkResource;
  sourceResource?: NetworkResource;
  policy?: Policy;
  totalRx: number;
  totalTx: number;
  latestAt: string;
};

// A "row" in the rendered table is either an actual flow event or
// a synthetic policy line we splice in between the start and end of
// a flow. Discriminating up-front keeps the rendering code branchless.
type Row =
  | { kind: "event"; event: NetworkTrafficEvent; group: FlowGroup; positionInFlow: "first" | "middle" | "last" | "only" }
  | { kind: "policy"; policy: Policy; group: FlowGroup; positionInFlow: "middle" };

const PAGE_SIZES = [10, 25, 50, 100, 1000];
const DEFAULT_PAGE_SIZE = 25;

type SortKey = "time" | "source" | "protocol" | "destination" | "traffic";
type SortDir = "asc" | "desc";
type SortState = { key: SortKey; dir: SortDir };

// Grid template echoes NetBird's table: a wide EVENT column for the
// timeline narrative + four equal-ish columns for SOURCE / PROTOCOL /
// DESTINATION / TRAFFIC. All rows use the same template so columns
// stay aligned across the whole table even though we're not using a
// real <table> element (we want fine-grained control over the
// vertical line connecting timeline dots, which colSpan + tr would
// fight us on).
const GRID_COLS =
  "minmax(0, 4fr) minmax(0, 2fr) minmax(0, 1.4fr) minmax(0, 2fr) minmax(0, 1fr)";

export default function NetworkTrafficTimeline() {
  const { peers } = usePeers();
  const { data: policies } = useFetchApi<Policy[]>("/policies");
  // Network Resources surface as flow-event destinations once the
  // routing peer's forward chain stamps the rule_index — without
  // this fetch, a flow to e.g. ci.example.com would render the bare
  // IP labelled "external", losing the operator-friendly hostname
  // they configured the resource with.
  const { data: resources } = useFetchApi<NetworkResource[]>(
    "/networks/resources",
  );

  const [search, setSearch] = useState("");
  const [range, setRange] = useState<DateRange | undefined>();
  const [peerFilter, setPeerFilter] = useState<string | undefined>();
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE);
  const [pageIndex, setPageIndex] = useState(0);
  const [sort, setSort] = useState<SortState>({ key: "time", dir: "desc" });

  // Push the date range onto the API as RFC3339 since/until — the
  // handler at management/server/http/handlers/network_events parses
  // the same format. URL changes invalidate SWR's cache key, so the
  // hook auto-refetches when the operator picks a different window.
  const queryUrl = useMemo(() => {
    const params = new URLSearchParams();
    if (range?.from) params.set("since", range.from.toISOString());
    if (range?.to) params.set("until", range.to.toISOString());
    const qs = params.toString();
    return qs ? `/network-traffic-events?${qs}` : "/network-traffic-events";
  }, [range]);

  const { data, isLoading, mutate } =
    useFetchApi<NetworkTrafficEventsResponse>(queryUrl);

  const groups = useMemo(
    () =>
      groupFlows(
        data?.events ?? [],
        peers ?? [],
        policies ?? [],
        resources ?? [],
      ),
    [data, peers, policies, resources],
  );

  const filteredGroups = useMemo(() => {
    const q = search.trim().toLowerCase();
    return groups.filter((g) => {
      if (peerFilter) {
        if (
          g.sourcePeer?.id !== peerFilter &&
          g.destPeer?.id !== peerFilter &&
          g.reportingPeer?.id !== peerFilter
        ) {
          return false;
        }
      }
      if (q && !matchesSearch(g, q)) return false;
      return true;
    });
  }, [groups, search, peerFilter]);

  const sortedGroups = useMemo(
    () => sortGroups(filteredGroups, sort),
    [filteredGroups, sort],
  );

  const totalPages = Math.max(1, Math.ceil(sortedGroups.length / pageSize));
  const safePageIndex = Math.min(pageIndex, totalPages - 1);
  const pageStart = safePageIndex * pageSize;
  const pageGroups = sortedGroups.slice(pageStart, pageStart + pageSize);
  const rows = useMemo(() => flattenRows(pageGroups), [pageGroups]);

  const peerOptions = useMemo(
    () =>
      (peers ?? [])
        .filter((p) => p.id)
        .map((p) => ({
          id: p.id as string,
          name: p.name,
          hostname: p.hostname,
          ip: p.ip,
          os: p.os,
          countryCode: p.country_code,
        }))
        .sort((a, b) => a.name.localeCompare(b.name)),
    [peers],
  );

  const resetPage = () => setPageIndex(0);

  if (isLoading && !data) return <SkeletonTable />;

  return (
    <div className={"relative table-fixed-scroll"}>
      {/* Toolbar wrapper mirrors DataTable's `flex gap-x-4 gap-y-6 flex-wrap p-default`
          so the search/filters strip lines up exactly with the audit / users pages. */}
      <div className={"flex gap-x-4 gap-y-6 flex-wrap p-default"}>
        <Toolbar
          search={search}
          onSearch={(v) => {
            setSearch(v);
            resetPage();
          }}
          range={range}
          onRangeChange={(r) => {
            setRange(r);
            resetPage();
          }}
          peerFilter={peerFilter}
          peerOptions={peerOptions}
          onPeerFilterChange={(p) => {
            setPeerFilter(p);
            resetPage();
          }}
          pageSize={pageSize}
          onPageSizeChange={(s) => {
            setPageSize(s);
            resetPage();
          }}
          onRefresh={() => mutate()}
          flowCount={filteredGroups.length}
          isLoading={isLoading}
        />
      </div>

      {rows.length === 0 ? (
        <div className={"p-default"}>
          <EmptyState
            hasFilter={search.length > 0 || !!range || !!peerFilter}
          />
        </div>
      ) : (
        <div
          className={
            "grid border dark:border-zinc-700/40 border-l-0 border-r-0 mt-6"
          }
          style={{ gridTemplateColumns: GRID_COLS }}
          role={"table"}
          aria-label={"Network traffic events"}
        >
          <HeaderRow sort={sort} onSortChange={setSort} />
          {rows.map((r, i) => (
            <RowCells
              key={rowKey(r, i)}
              row={r}
              isLastInTable={i === rows.length - 1}
            />
          ))}
        </div>
      )}

      {filteredGroups.length > 0 && (
        <div className={"p-default py-3"}>
          <Pagination
            pageIndex={safePageIndex}
            pageSize={pageSize}
            total={filteredGroups.length}
            onPrev={() => setPageIndex((p) => Math.max(0, p - 1))}
            onNext={() =>
              setPageIndex((p) => Math.min(totalPages - 1, p + 1))
            }
          />
        </div>
      )}
    </div>
  );
}

type PeerOption = {
  id: string;
  name: string;
  hostname: string;
  ip: string;
  os: string;
  countryCode: string;
};

function Toolbar({
  search,
  onSearch,
  range,
  onRangeChange,
  peerFilter,
  peerOptions,
  onPeerFilterChange,
  pageSize,
  onPageSizeChange,
  onRefresh,
  flowCount,
  isLoading,
}: {
  search: string;
  onSearch: (v: string) => void;
  range: DateRange | undefined;
  onRangeChange: (r: DateRange | undefined) => void;
  peerFilter: string | undefined;
  peerOptions: PeerOption[];
  onPeerFilterChange: (id: string | undefined) => void;
  pageSize: number;
  onPageSizeChange: (n: number) => void;
  onRefresh: () => void;
  flowCount: number;
  isLoading: boolean;
}) {
  return (
    <div className={"flex flex-wrap items-center gap-3"}>
      <div className={"relative flex-1 min-w-[240px] max-w-md"}>
        <SearchIcon
          size={14}
          className={
            "absolute left-3 top-1/2 -translate-y-1/2 text-neutral-500 dark:text-nb-gray-400"
          }
        />
        <Input
          value={search}
          onChange={(e) => onSearch(e.target.value)}
          placeholder={"Search by peer, IP or policy…"}
          className={"pl-9"}
        />
      </div>
      <DatePickerWithRange value={range} onChange={onRangeChange} />
      <PeerFilterDropdown
        value={peerFilter}
        options={peerOptions}
        onChange={onPeerFilterChange}
      />
      <span className={"text-xs text-neutral-600 dark:text-nb-gray-300"}>
        {flowCount} flow{flowCount === 1 ? "" : "s"}
      </span>
      <div className={"ml-auto flex items-center gap-2"}>
        <RowsPerPageSelector value={pageSize} onChange={onPageSizeChange} />
        <DataTableRefreshButton isDisabled={isLoading} onClick={onRefresh} />
      </div>
    </div>
  );
}

// PeerFilterDropdown narrows the timeline to flows that involve a
// specific peer — source, dest, or reporter all count. The visual
// shell (search input + scroll list + per-item avatar/name/sub)
// mirrors UsersDropdownSelector on Audit Events for cohesion across
// activity views.
//
// Typeahead via CommandInput matches name + hostname + IP. cmdk's
// own filter handles the substring match; we only build a search
// haystack string per row that includes everything we want to match.
function PeerFilterDropdown({
  value,
  options,
  onChange,
}: {
  value: string | undefined;
  options: PeerOption[];
  onChange: (id: string | undefined) => void;
}) {
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
        if (!isOpen) {
          setTimeout(() => setSearchInput(""), 100);
        }
        setOpen(isOpen);
      }}
    >
      <PopoverTrigger asChild>
        <Button variant={"secondary"}>
          <div
            className={"w-full flex justify-between items-center gap-2"}
          >
            {!selected ? (
              <>
                <ActivityIcon size={16} />
                All peers
              </>
            ) : (
              <>
                <PeerAvatar peer={selected} size={20} />
                <TextWithTooltip
                  text={selected.name}
                  maxChars={20}
                  className={"leading-none"}
                />
              </>
            )}
            <div className={"pl-2"}>
              <ChevronsUpDown size={18} className={"shrink-0"} />
            </div>
          </div>
        </Button>
      </PopoverTrigger>
      <PopoverContent
        className={
          "w-full p-0 shadow-sm shadow-nb-gray-950 min-w-[300px]"
        }
        align={"start"}
        side={"bottom"}
        sideOffset={10}
      >
        <Command
          className={"w-full flex"}
          loop
          filter={(value, search) => {
            const fv = trim(value.toLowerCase());
            const fs = trim(search.toLowerCase());
            if (fv.includes(fs)) return 1;
            return 0;
          }}
        >
          <CommandList className={"w-full"}>
            <div className={"relative"}>
              <CommandInput
                className={cn(
                  "min-h-[42px] w-full relative",
                  "border-b-0 border-t-0 border-r-0 border-l-0 border-neutral-200 dark:border-nb-gray-700 items-center",
                  "bg-transparent text-sm outline-none focus-visible:outline-none ring-0 focus-visible:ring-0",
                  "dark:placeholder:text-neutral-500 dark:text-nb-gray-400 font-light placeholder:text-neutral-500 pl-10",
                )}
                value={searchInput}
                onValueChange={setSearchInput}
                placeholder={"Search peers..."}
              />
              <div
                className={
                  "absolute left-0 top-0 h-full flex items-center pl-4"
                }
              >
                <SearchIcon size={14} />
              </div>
            </div>

            <ScrollArea
              className={
                "max-h-[380px] overflow-y-hidden flex flex-col gap-1 pl-2 py-2 pr-3"
              }
            >
              <CommandGroup>
                <div className={"grid grid-cols-1 gap-1"}>
                  <CommandItem
                    value={"all peers"}
                    className={"py-1 px-2"}
                    onSelect={() => toggle(undefined)}
                    onClick={(e) => e.preventDefault()}
                  >
                    <div className={"flex items-center gap-2"}>
                      <div
                        className={
                          "w-7 h-7 rounded-full flex items-center justify-center bg-sky-400 text-white"
                        }
                      >
                        <ActivityIcon size={14} />
                      </div>
                      <div className={"flex flex-col text-xs"}>
                        <span className={"text-neutral-700 dark:text-nb-gray-200"}>All peers</span>
                        <span className={"text-neutral-500 dark:text-nb-gray-400 font-light"}>
                          Include every peer in this account
                        </span>
                      </div>
                    </div>
                  </CommandItem>

                  {options.map((opt) => {
                    const haystack = [
                      opt.name,
                      opt.hostname,
                      opt.ip,
                      opt.id,
                    ]
                      .filter(Boolean)
                      .join(" ");

                    return (
                      <CommandItem
                        key={opt.id}
                        value={haystack}
                        className={"py-1 px-2"}
                        onSelect={() => toggle(opt.id)}
                        onClick={(e) => e.preventDefault()}
                      >
                        <div
                          className={"flex items-center gap-2 w-full"}
                        >
                          <PeerAvatar peer={opt} size={28} />
                          <div className={"flex flex-col text-xs w-full"}>
                            <span
                              className={
                                "text-neutral-700 dark:text-nb-gray-200 flex items-center gap-1.5 w-full"
                              }
                            >
                              <TextWithTooltip
                                text={opt.name}
                                maxChars={20}
                              />
                            </span>
                            <span
                              className={
                                "text-neutral-500 dark:text-nb-gray-400 font-light flex items-center gap-1 font-mono"
                              }
                            >
                              {opt.ip}
                            </span>
                          </div>
                          {value === opt.id && (
                            <Check
                              size={14}
                              className={"text-emerald-600 dark:text-emerald-400 ml-auto"}
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

// PeerAvatar renders the OS-icon avatar with an optional country flag
// overlay, sized to caller specification. Mirrors the SmallUserAvatar
// shape used by Audit Events but driven by peer metadata instead of
// user identity.
function PeerAvatar({ peer, size }: { peer: PeerOption; size: number }) {
  const inner = Math.round(size * 0.65);
  const flag = Math.max(10, Math.round(size * 0.55));
  return (
    <div className={"relative shrink-0"}>
      <div
        className={
          "rounded-md bg-neutral-100 dark:bg-nb-gray-900 border border-neutral-300 dark:border-nb-gray-800 flex items-center justify-center"
        }
        style={{ width: size, height: size }}
      >
        <div
          className={"flex items-center justify-center"}
          style={{ width: inner, height: inner }}
        >
          <OSLogo os={peer.os} />
        </div>
      </div>
      {peer.countryCode && (
        <div className={"absolute -bottom-1 -right-1"}>
          <RoundedFlag country={peer.countryCode} size={flag} />
        </div>
      )}
    </div>
  );
}

// RowsPerPageSelector is a standalone version of DataTableRowsPerPage
// — same look, but does not depend on a TanStack Table instance since
// the timeline drives its own pagination state. PAGE_SIZES is shared
// with the Audit page's selector so muscle memory carries over.
function RowsPerPageSelector({
  value,
  onChange,
}: {
  value: number;
  onChange: (n: number) => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant={"secondary"}
          role={"combobox"}
          aria-expanded={open}
          className={"w-[200px] justify-between"}
        >
          <RowsIcon size={15} className={"text-neutral-600 dark:text-nb-gray-300 shrink-0"} />
          <div>
            <span className={"text-neutral-900 dark:text-white"}>{value}</span>
            <span className={"text-neutral-600 dark:text-nb-gray-300"}> rows per page</span>
          </div>
          <ChevronDown className={"h-4 w-4 opacity-50"} />
        </Button>
      </PopoverTrigger>
      <PopoverContent className={"w-[200px] p-0"} sideOffset={7}>
        <Command value={`${value}`}>
          <CommandGroup>
            {PAGE_SIZES.map((n) => (
              <CommandItem
                key={n}
                value={n.toString()}
                onSelect={(currentValue) => {
                  onChange(Number(currentValue));
                  setOpen(false);
                }}
              >
                <div
                  className={cn(
                    "cursor-pointer flex gap-2 px-2 py-1.5 my-1 mx-1 rounded-md items-center hover:bg-neutral-200 dark:hover:bg-nb-gray-800",
                    value === n
                      ? "text-neutral-900 dark:text-white"
                      : "text-neutral-500 dark:text-nb-gray-400",
                  )}
                >
                  <Check
                    size={15}
                    className={cn(
                      "text-white shrink-0",
                      value === n ? "opacity-100" : "opacity-0",
                    )}
                  />
                  {n}
                </div>
              </CommandItem>
            ))}
          </CommandGroup>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

function Pagination({
  pageIndex,
  pageSize,
  total,
  onPrev,
  onNext,
}: {
  pageIndex: number;
  pageSize: number;
  total: number;
  onPrev: () => void;
  onNext: () => void;
}) {
  const start = pageIndex * pageSize + 1;
  const end = Math.min(total, (pageIndex + 1) * pageSize);
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  return (
    <div className={"flex items-center justify-end gap-3 pt-1"}>
      <span className={"text-xs text-neutral-500 dark:text-nb-gray-400"}>
        Showing {start}–{end} of {total}
      </span>
      <div className={"flex items-center gap-1"}>
        <button
          type={"button"}
          onClick={onPrev}
          disabled={pageIndex === 0}
          className={
            "p-1.5 rounded-md text-neutral-600 dark:text-nb-gray-300 hover:text-neutral-900 dark:hover:text-white hover:bg-neutral-200 dark:hover:bg-nb-gray-800 disabled:opacity-40 disabled:hover:bg-transparent"
          }
          aria-label={"Previous page"}
        >
          <ChevronLeft size={16} />
        </button>
        <span className={"font-mono text-xs text-neutral-500 dark:text-nb-gray-400 px-2"}>
          {pageIndex + 1} / {totalPages}
        </span>
        <button
          type={"button"}
          onClick={onNext}
          disabled={pageIndex >= totalPages - 1}
          className={
            "p-1.5 rounded-md text-neutral-600 dark:text-nb-gray-300 hover:text-neutral-900 dark:hover:text-white hover:bg-neutral-200 dark:hover:bg-nb-gray-800 disabled:opacity-40 disabled:hover:bg-transparent"
          }
          aria-label={"Next page"}
        >
          <ChevronRight size={16} />
        </button>
      </div>
    </div>
  );
}

function HeaderRow({
  sort,
  onSortChange,
}: {
  sort: SortState;
  onSortChange: (s: SortState) => void;
}) {
  // Mirror the .h-12 / px-4 / uppercase / dark:bg-nb-gray-900 styling
  // of TableHead in components/table/Table.tsx so this grid feels
  // visually identical to the standard <table>-based pages (Users,
  // Audit Events, Networks). The outer wrapper supplies rounded-lg +
  // overflow-hidden, so the solid bg also gives us the rounded top
  // corners that the user expects from a table.
  const cls =
    "h-12 px-4 flex items-center align-middle uppercase font-medium text-neutral-500 dark:text-nb-gray-400 bg-neutral-100 dark:bg-nb-gray-900 border-b border-neutral-200 dark:border-zinc-700/40";
  return (
    <>
      <div className={cls} role={"columnheader"}>
        <SortableHeader
          label={"Event"}
          sortKey={"time"}
          sort={sort}
          onSortChange={onSortChange}
        />
      </div>
      <div className={cls} role={"columnheader"}>
        <SortableHeader
          label={"Source"}
          sortKey={"source"}
          sort={sort}
          onSortChange={onSortChange}
        />
      </div>
      <div className={cls} role={"columnheader"}>
        <SortableHeader
          label={"Protocol & Port"}
          sortKey={"protocol"}
          sort={sort}
          onSortChange={onSortChange}
        />
      </div>
      <div className={cls} role={"columnheader"}>
        <SortableHeader
          label={"Destination"}
          sortKey={"destination"}
          sort={sort}
          onSortChange={onSortChange}
        />
      </div>
      <div className={cls} role={"columnheader"}>
        <SortableHeader
          label={"Traffic"}
          sortKey={"traffic"}
          sort={sort}
          onSortChange={onSortChange}
          align={"right"}
        />
      </div>
    </>
  );
}

// SortableHeader replicates the look of DataTableHeader (sort icons,
// hover, typography) without depending on TanStack's Column<TData>
// because the timeline doesn't go through DataTable. Clicking toggles
// asc/desc on the active column or activates the column with desc.
function SortableHeader({
  label,
  sortKey,
  sort,
  onSortChange,
  align = "left",
}: {
  label: string;
  sortKey: SortKey;
  sort: SortState;
  onSortChange: (s: SortState) => void;
  align?: "left" | "right";
}) {
  const isActive = sort.key === sortKey;
  const Icon =
    isActive && sort.dir === "asc" ? IconSortDescending : IconSortAscending;
  return (
    <button
      type={"button"}
      onClick={() => {
        if (isActive) {
          onSortChange({
            key: sortKey,
            dir: sort.dir === "asc" ? "desc" : "asc",
          });
        } else {
          onSortChange({ key: sortKey, dir: "desc" });
        }
      }}
      className={cn(
        "flex items-center whitespace-nowrap cursor-pointer gap-2 dark:text-gray-400 dark:hover:text-gray-300 transition-all select-none hover:text-nb-gray text-xs tracking-wide uppercase font-medium",
        align === "right" && "justify-end w-full",
        isActive && "dark:text-gray-200",
      )}
    >
      {label}
      <Icon size={16} />
    </button>
  );
}

// sortGroups reorders flow groups by the chosen column. We do this at
// the FlowGroup level (not at Row level) so the timeline rows of a
// single flow stay together — the policy / end events follow their
// start event regardless of the active sort column.
function sortGroups(groups: FlowGroup[], sort: SortState): FlowGroup[] {
  const out = [...groups];
  out.sort((a, b) => {
    let cmp = 0;
    switch (sort.key) {
      case "time":
        cmp = a.latestAt.localeCompare(b.latestAt);
        break;
      case "source": {
        const ai = a.events[0];
        const bi = b.events[0];
        const av = a.sourcePeer?.name ?? ai?.source_ip ?? "";
        const bv = b.sourcePeer?.name ?? bi?.source_ip ?? "";
        cmp = av.localeCompare(bv);
        break;
      }
      case "protocol": {
        const ap = a.events[0]?.protocol ?? 0;
        const bp = b.events[0]?.protocol ?? 0;
        cmp = ap - bp;
        if (cmp === 0) {
          const adp = a.events[0]?.dest_port ?? 0;
          const bdp = b.events[0]?.dest_port ?? 0;
          cmp = adp - bdp;
        }
        break;
      }
      case "destination": {
        const ai = a.events[0];
        const bi = b.events[0];
        const av = a.destPeer?.name ?? ai?.dest_ip ?? "";
        const bv = b.destPeer?.name ?? bi?.dest_ip ?? "";
        cmp = av.localeCompare(bv);
        break;
      }
      case "traffic":
        cmp = a.totalRx + a.totalTx - (b.totalRx + b.totalTx);
        break;
    }
    return sort.dir === "asc" ? cmp : -cmp;
  });
  return out;
}

function RowCells({
  row,
  isLastInTable,
}: {
  row: Row;
  isLastInTable: boolean;
}) {
  const isFlowFirst =
    row.positionInFlow === "first" || row.positionInFlow === "only";
  const isFlowLast =
    row.positionInFlow === "last" || row.positionInFlow === "only";

  // Border between flows but not within one — the timeline dots'
  // vertical line carries the visual continuity within a flow.
  const flowBorder = isFlowLast && !isLastInTable ? "border-b border-neutral-200 dark:border-nb-gray-900" : "";

  // Padding: top/bottom collapse so events of the same flow read as
  // one block. First/last get the normal padding.
  const paddingY = (() => {
    if (row.positionInFlow === "only") return "py-3";
    if (row.positionInFlow === "first") return "pt-3 pb-1.5";
    if (row.positionInFlow === "last") return "pt-1.5 pb-3";
    return "py-1.5";
  })();

  const cellCls = `px-4 ${paddingY} ${flowBorder}`;

  return (
    <>
      <div className={cellCls} role={"cell"}>
        <EventCell row={row} />
      </div>
      <div className={cellCls} role={"cell"}>
        {isFlowFirst && (
          <PeerCell
            peer={row.group.sourcePeer}
            resource={row.group.sourceResource}
            ip={firstEvent(row).source_ip}
          />
        )}
      </div>
      <div className={cellCls} role={"cell"}>
        {isFlowFirst && <ProtoCell event={firstEvent(row)} />}
      </div>
      <div className={cellCls} role={"cell"}>
        {isFlowFirst && (
          <PeerCell
            peer={row.group.destPeer}
            resource={row.group.destResource}
            ip={firstEvent(row).dest_ip}
          />
        )}
      </div>
      <div className={cellCls + " text-right"} role={"cell"}>
        {isFlowFirst && <TrafficCell rx={row.group.totalRx} tx={row.group.totalTx} />}
      </div>
    </>
  );
}

function firstEvent(row: Row): NetworkTrafficEvent {
  if (row.kind === "event") return row.event;
  return row.group.events[0];
}

function EventCell({ row }: { row: Row }) {
  const lineUp = row.positionInFlow !== "first" && row.positionInFlow !== "only";
  const lineDown = row.positionInFlow !== "last" && row.positionInFlow !== "only";
  const dot = row.kind === "event" ? eventDot(row.event) : policyDot();

  // items-center vertically aligns the dot with the content's center,
  // so 1-line policy rows and 2-line event rows both place the dot
  // next to the (visual) middle of their text. The connecting lines
  // grow from dot to cell edge on both sides via flex-1.
  return (
    <div className={"flex items-center gap-3 min-w-0"}>
      <div className={"flex flex-col items-center self-stretch shrink-0"}>
        <span
          className={`w-px flex-1 ${lineUp ? "bg-neutral-300 dark:bg-nb-gray-800" : "bg-transparent"}`}
          style={{ minHeight: "4px" }}
        />
        <span className={dot.cls}>{dot.icon}</span>
        <span
          className={`w-px flex-1 ${lineDown ? "bg-neutral-300 dark:bg-nb-gray-800" : "bg-transparent"}`}
          style={{ minHeight: "4px" }}
        />
      </div>
      <div className={"flex flex-col min-w-0 gap-0.5"}>
        {row.kind === "event" ? (
          <EventContent event={row.event} group={row.group} />
        ) : (
          <PolicyContent policy={row.policy} />
        )}
      </div>
    </div>
  );
}

function EventContent({
  event,
  group,
}: {
  event: NetworkTrafficEvent;
  group: FlowGroup;
}) {
  return (
    <>
      <span className={"font-mono text-[11px] text-neutral-500 dark:text-nb-gray-400"}>
        {dayjs(event.occurred_at).format("MMM DD, YYYY · HH:mm:ss")}
      </span>
      <span className={"text-sm text-neutral-900 dark:text-white"}>{narrativeFor(event, group)}</span>
    </>
  );
}

function PolicyContent({ policy }: { policy: Policy }) {
  // The verb mirrors the rule's action so a "drop" policy doesn't read
  // as "allowed". rules[0] is the canonical entry — the dashboard
  // editor enforces a single rule per policy today, but if multiple
  // appear in the wire format we still index 0 for display since the
  // rendered policy line summarizes the parent policy, not each rule.
  const action = policy.rules?.[0]?.action;
  const verb =
    action === "drop" || action === "deny"
      ? "blocked the connection"
      : "allowed the connection";
  const tone =
    action === "drop" || action === "deny"
      ? "text-red-600 dark:text-red-400"
      : "text-emerald-600 dark:text-emerald-400";

  return (
    <span className={"text-sm text-neutral-900 dark:text-white"} title={policy.description}>
      Policy{" "}
      <Link
        href={"/access-control"}
        className={`font-medium ${tone} inline-flex items-center gap-0.5 hover:underline`}
      >
        {policy.name}
        <ExternalLinkIcon size={11} className={"opacity-70"} />
      </Link>{" "}
      {verb}
    </span>
  );
}

// PeerCell shows the peer as an OS-icon avatar with a country flag
// overlay, name on top, IP underneath in mono. When the peer didn't
// resolve, we walk a fallback chain before giving up: a Network
// Resource match (rendering the operator-configured address such as
// ci.example.com) takes precedence over the bare-IP "external" label.
// Knowing which IP fired matters even when the friendly name is
// missing — we always print the IP underneath. The Globe icon stands
// in for the avatar when neither a peer nor a resource matches.
function PeerCell({
  peer,
  resource,
  ip,
}: {
  peer?: Peer;
  resource?: NetworkResource;
  ip: string;
}) {
  const fallbackTitle = resource?.address ?? "external";
  const fallbackKind = resource?.type ?? null; // "domain" | "host" | "subnet"
  return (
    <div className={"flex items-center gap-2 min-w-0"}>
      <div className={"relative shrink-0"}>
        <div
          className={
            "h-8 w-8 rounded-md bg-neutral-100 dark:bg-nb-gray-930 border border-neutral-200 dark:border-nb-gray-900 flex items-center justify-center"
          }
        >
          {peer ? (
            <div className={"h-5 w-5 flex items-center justify-center"}>
              <OSLogo os={peer.os} />
            </div>
          ) : (
            <GlobeIcon size={14} className={"text-neutral-400 dark:text-nb-gray-500"} />
          )}
        </div>
        {peer?.country_code && (
          <div className={"absolute -bottom-1 -right-1"}>
            <RoundedFlag country={peer.country_code} size={14} />
          </div>
        )}
      </div>
      <div className={"flex flex-col min-w-0"}>
        {peer ? (
          <span className={"text-sm text-neutral-900 dark:text-white truncate"}>{peer.name}</span>
        ) : resource ? (
          <span
            className={"text-sm text-neutral-900 dark:text-white truncate"}
            title={resource.name ? `${resource.name} (${resource.address})` : resource.address}
          >
            {fallbackTitle}
          </span>
        ) : (
          <span className={"text-xs text-neutral-500 dark:text-nb-gray-400 truncate"}>external</span>
        )}
        <span className={"font-mono text-[11px] text-neutral-500 dark:text-nb-gray-400 truncate"}>
          {fallbackKind && !peer ? `${fallbackKind} · ${ip}` : ip}
        </span>
      </div>
    </div>
  );
}

function ProtoCell({ event }: { event: NetworkTrafficEvent }) {
  const sub = portChipFor(event);
  return (
    <div className={"flex items-center gap-1.5 flex-wrap"}>
      <Chip label={protocolName(event.protocol)} />
      {sub && <Chip label={sub.label} muted />}
    </div>
  );
}

function Chip({ label, muted }: { label: string; muted?: boolean }) {
  return (
    <span
      className={[
        "inline-flex items-center px-2 py-0.5 rounded-md font-mono text-[11px] uppercase tracking-wide",
        muted
          ? "bg-neutral-100 dark:bg-nb-gray-930 text-neutral-600 dark:text-nb-gray-300 border border-neutral-200 dark:border-nb-gray-900"
          : "bg-neutral-200 text-neutral-900 dark:bg-nb-gray-900 dark:text-white border border-neutral-300 dark:border-nb-gray-800",
      ].join(" ")}
    >
      {label}
    </span>
  );
}

function TrafficCell({ rx, tx }: { rx: number; tx: number }) {
  return (
    <div className={"flex flex-col items-end gap-0.5 font-mono text-xs"}>
      <span className={"flex items-center gap-1 text-sky-600 dark:text-sky-400"}>
        <ArrowDown size={11} />
        {formatBytes(rx)}
      </span>
      <span className={"flex items-center gap-1 text-violet-600 dark:text-violet-400"}>
        <ArrowUp size={11} />
        {formatBytes(tx)}
      </span>
    </div>
  );
}

function EmptyState({ hasFilter }: { hasFilter: boolean }) {
  return (
    <div
      className={
        "rounded-lg border border-dashed border-neutral-200 dark:border-nb-gray-900 px-6 py-12 text-center"
      }
    >
      <ActivityIcon size={28} className={"mx-auto mb-3 text-neutral-400 dark:text-nb-gray-600"} />
      <p className={"text-sm text-neutral-600 dark:text-nb-gray-300"}>
        {hasFilter
          ? "No flows match your filters."
          : "No network traffic events yet."}
      </p>
      {!hasFilter && (
        <p className={"text-xs text-neutral-400 dark:text-nb-gray-500 mt-1"}>
          Once peers start reporting, every TCP / UDP / ICMP connection
          shows up here.
        </p>
      )}
    </div>
  );
}

// flattenRows expands each FlowGroup into its constituent rows in
// chronological order. A flow with a known policy gets a synthetic
// "policy" row inserted between the start event and any subsequent
// events — that mirrors NetBird's UX where the access-control rule
// reads as a step in the flow's lifecycle.
function flattenRows(groups: FlowGroup[]): Row[] {
  const out: Row[] = [];
  for (const g of groups) {
    const events = [...g.events].sort((a, b) =>
      a.occurred_at.localeCompare(b.occurred_at),
    );
    const totalRows = events.length + (g.policy ? 1 : 0);
    let i = 0;
    for (let idx = 0; idx < events.length; idx++) {
      const e = events[idx];
      out.push({
        kind: "event",
        event: e,
        group: g,
        positionInFlow: rowPosition(i, totalRows),
      });
      i++;
      // Splice the policy line in right after the start event so the
      // chronological narrative reads: "X received connection ..." →
      // "Policy Y allowed the connection" → "X stopped connection ...".
      if (idx === 0 && g.policy && events.length > 1) {
        out.push({
          kind: "policy",
          policy: g.policy,
          group: g,
          positionInFlow: "middle",
        });
        i++;
      }
    }
    if (g.policy && events.length === 1) {
      // Drop the "only" tag on the previous row since we're appending
      // a policy line — it's now the last instead.
      const prev = out[out.length - 1];
      if (prev && prev.kind === "event" && prev.positionInFlow === "only") {
        prev.positionInFlow = "first";
      }
      out.push({
        kind: "policy",
        policy: g.policy,
        group: g,
        positionInFlow: "last" as unknown as "middle",
      });
    }
  }
  return out;
}

function rowPosition(
  i: number,
  total: number,
): "first" | "middle" | "last" | "only" {
  if (total === 1) return "only";
  if (i === 0) return "first";
  if (i === total - 1) return "last";
  return "middle";
}

function rowKey(r: Row, idx: number): string {
  if (r.kind === "event") return `${r.group.flowID}:e:${r.event.event_id}`;
  return `${r.group.flowID}:p:${idx}`;
}

// groupFlows folds the event stream into per-flow_id groups and
// resolves each group's peers / policy once. Resolution priority for
// the source/dest peers:
//
//   1. byIP[event.source_ip|dest_ip]   — the obvious match
//   2. byID[event.peer_id] when the event's direction tells us which
//      side reported (egress => reporter is source; ingress => reporter
//      is destination). This catches the case where peers cycle their
//      mesh IPs (e.g. across HA upgrades) and the IP recorded in the
//      flow event no longer matches the peer's current /api/peers row.
function groupFlows(
  events: NetworkTrafficEvent[],
  peers: Peer[],
  policies: Policy[],
  resources: NetworkResource[],
): FlowGroup[] {
  const byID = new Map<string, Peer>();
  const byIP = new Map<string, Peer>();
  for (const p of peers) {
    if (p.id) byID.set(p.id, p);
    if (p.ip) byIP.set(p.ip, p);
  }
  const policyByID = new Map<string, Policy>();
  for (const p of policies) {
    if (p.id) policyByID.set(p.id, p);
  }
  const resourceByID = new Map<string, NetworkResource>();
  for (const r of resources) {
    if (r.id) resourceByID.set(r.id, r);
  }

  const grouped = new Map<string, FlowGroup>();
  for (const e of events) {
    const reporting = byID.get(e.peer_id);
    let source = byIP.get(e.source_ip);
    let dest = byIP.get(e.dest_ip);
    if (!source && reporting && e.direction === "egress") source = reporting;
    if (!dest && reporting && e.direction === "ingress") dest = reporting;
    const sourceResource = e.source_resource_id
      ? resourceByID.get(e.source_resource_id)
      : undefined;
    const destResource = e.dest_resource_id
      ? resourceByID.get(e.dest_resource_id)
      : undefined;

    const key = e.flow_id || e.event_id;
    let g = grouped.get(key);
    if (!g) {
      g = {
        flowID: key,
        events: [],
        reportingPeer: reporting,
        sourcePeer: source,
        destPeer: dest,
        sourceResource,
        destResource,
        policy: e.rule_id ? policyByID.get(e.rule_id) : undefined,
        totalRx: 0,
        totalTx: 0,
        latestAt: e.occurred_at,
      };
      grouped.set(key, g);
    } else {
      g.sourcePeer = g.sourcePeer ?? source;
      g.destPeer = g.destPeer ?? dest;
      g.reportingPeer = g.reportingPeer ?? reporting;
      g.sourceResource = g.sourceResource ?? sourceResource;
      g.destResource = g.destResource ?? destResource;
    }
    g.events.push(e);
    g.totalRx += e.rx_bytes;
    g.totalTx += e.tx_bytes;
    if (e.occurred_at > g.latestAt) g.latestAt = e.occurred_at;
    if (!g.policy && e.rule_id) g.policy = policyByID.get(e.rule_id);
  }

  return Array.from(grouped.values()).sort((a, b) =>
    b.latestAt.localeCompare(a.latestAt),
  );
}

function matchesSearch(g: FlowGroup, q: string): boolean {
  const haystack = [
    g.sourcePeer?.name,
    g.sourcePeer?.hostname,
    g.destPeer?.name,
    g.destPeer?.hostname,
    g.sourceResource?.name,
    g.sourceResource?.address,
    g.destResource?.name,
    g.destResource?.address,
    g.reportingPeer?.name,
    g.policy?.name,
    g.events[0]?.source_ip,
    g.events[0]?.dest_ip,
  ];
  return haystack.some((v) => v && v.toLowerCase().includes(q));
}

function narrativeFor(e: NetworkTrafficEvent, g: FlowGroup): string {
  const dest = g.destPeer?.name ?? g.destResource?.address ?? e.dest_ip;
  const src = g.sourcePeer?.name ?? g.sourceResource?.address ?? e.source_ip;
  switch (e.type) {
    case "start":
      // Mirror NetBird's wording. "received P2P connection from" reads
      // from the destination's perspective, which matches how the
      // event was reported (peer_id is the reporter, direction tells
      // us which side they were on).
      if (e.direction === "egress") {
        return `Peer ${src} requested P2P connection to Peer ${dest}`;
      }
      return `Peer ${dest} received P2P connection from Peer ${src}`;
    case "end":
      if (e.direction === "egress") {
        return `Peer ${src} stopped P2P connection to Peer ${dest}`;
      }
      return `Peer ${dest} stopped P2P connection from Peer ${src}`;
    case "drop":
      return `Peer ${dest} blocked connection from Peer ${src}`;
    default:
      return `Event between ${dest} and ${src}`;
  }
}

function eventDot(e: NetworkTrafficEvent): {
  icon: React.ReactNode;
  cls: string;
} {
  switch (e.type) {
    case "start":
      return {
        icon: <Play size={10} />,
        cls:
          "inline-flex items-center justify-center w-5 h-5 rounded-full bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 shrink-0",
      };
    case "end":
      return {
        icon: <Square size={10} />,
        cls:
          "inline-flex items-center justify-center w-5 h-5 rounded-full bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 shrink-0",
      };
    case "drop":
      return {
        icon: <Ban size={10} />,
        cls:
          "inline-flex items-center justify-center w-5 h-5 rounded-full bg-red-500/15 text-red-600 dark:text-red-400 shrink-0",
      };
    default:
      return {
        icon: null,
        cls:
          "inline-flex items-center justify-center w-5 h-5 rounded-full bg-neutral-100 dark:bg-nb-gray-900 text-neutral-400 dark:text-nb-gray-500 shrink-0",
      };
  }
}

function policyDot(): { icon: React.ReactNode; cls: string } {
  return {
    icon: <ShieldCheckIcon size={10} />,
    cls:
      "inline-flex items-center justify-center w-5 h-5 rounded-full bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 shrink-0",
  };
}

// portChipFor renders the L4 detail beside the protocol chip:
// TCP/UDP get the destination port, ICMP/ICMPv6 get the well-known
// type label (Echo for type 8/128, Reply for 0/129) so operators
// don't have to remember IANA numbers.
function portChipFor(e: NetworkTrafficEvent): { label: string } | null {
  if (e.protocol === 6 || e.protocol === 17) {
    if (e.dest_port) return { label: String(e.dest_port) };
    return null;
  }
  if (e.protocol === 1 || e.protocol === 58) {
    const t = e.icmp_type;
    if (t === undefined) return null;
    if (t === 8 || t === 128) return { label: "Echo" };
    if (t === 0 || t === 129) return { label: "Reply" };
    return { label: `type ${t}` };
  }
  return null;
}

function protocolName(protocol: number): string {
  switch (protocol) {
    case 1:
      return "icmp";
    case 6:
      return "tcp";
    case 17:
      return "udp";
    case 58:
      return "icmpv6";
    case 132:
      return "sctp";
    default:
      return `p${protocol}`;
  }
}

function formatBytes(b: number): string {
  if (b < 1024) return `${b} B`;
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`;
  if (b < 1024 * 1024 * 1024) return `${(b / 1024 / 1024).toFixed(1)} MB`;
  return `${(b / 1024 / 1024 / 1024).toFixed(2)} GB`;
}
