import { DropdownInfoText } from "@components/DropdownInfoText";
import { DropdownInput } from "@components/DropdownInput";
import { Popover, PopoverContent, PopoverTrigger } from "@components/Popover";
import TextWithTooltip from "@components/ui/TextWithTooltip";
import { VirtualScrollAreaList } from "@components/VirtualScrollAreaList";
import { useSearch } from "@hooks/useSearch";
import useFetchApi from "@utils/api";
import { cn } from "@utils/helpers";
import { sortBy } from "lodash";
import { ChevronsUpDown, MapPin, X } from "lucide-react";
import * as React from "react";
import { useMemo, useState } from "react";
import { useElementSize } from "@/hooks/useElementSize";
import { Peer } from "@/interfaces/Peer";

// PeerMultiSelector — multi-select sibling of PeerSelector. value is a
// list of peer IDs (the wire shape for openZro #5 Q2
// client_update_target_peers); onChange returns the new ID list.
// Mirrors PeerSelector's paint/search; deliberately simpler (no
// routing-support gating — this is a plain membership pick).

interface Props {
  value: string[];
  onChange: (ids: string[]) => void;
  disabled?: boolean;
  dataCy?: string;
}

const searchPredicate = (item: Peer, query: string) => {
  const q = query.toLowerCase();
  if (item.name.toLowerCase().includes(q)) return true;
  if (item.hostname.toLowerCase().includes(q)) return true;
  return item.ip.toLowerCase().startsWith(q);
};

export function PeerMultiSelector({
  value,
  onChange,
  disabled = false,
  dataCy = "peer-multi-selector",
}: Readonly<Props>) {
  const { data: peers } = useFetchApi<Peer[]>("/peers");
  const [inputRef, { width }] = useElementSize<HTMLButtonElement>();
  const [open, setOpen] = useState(false);

  const options = useMemo(
    () => sortBy([...(peers ?? [])], "name") as Peer[],
    [peers],
  );
  const [filteredItems, search, setSearch] = useSearch(
    options,
    searchPredicate,
    { filter: true, debounce: 150 },
  );

  const selectedPeers = useMemo(
    () => (peers ?? []).filter((p) => p.id && value.includes(p.id)),
    [peers, value],
  );

  const toggle = (id?: string) => {
    if (!id) return;
    onChange(
      value.includes(id) ? value.filter((v) => v !== id) : [...value, id],
    );
  };

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
          className={cn(
            "group relative flex min-h-[38px] w-full items-center justify-between gap-2",
            "rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 py-1.5 text-[13px] text-oz2-text-faint transition-colors",
            "hover:border-oz2-border-strong hover:bg-oz2-hover",
            "[outline:none] focus-visible:border-oz2-acc focus-visible:ring-2 focus-visible:ring-oz2-acc/30",
            "disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-60",
          )}
          disabled={disabled}
          data-cy={dataCy}
          ref={inputRef}
        >
          <div className="flex h-full flex-wrap items-center gap-1.5">
            {selectedPeers.length === 0 && <span>Select peer(s)…</span>}
            {selectedPeers.map((p) => (
              <span
                key={p.id}
                className="inline-flex items-center gap-1.5 rounded-[6px] border border-oz2-border-soft bg-oz2-bg-sunken/60 px-1.5 py-[3px] text-[12px] font-medium text-oz2-text-2"
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  if (!disabled) toggle(p.id);
                }}
              >
                <TextWithTooltip text={p.name} maxChars={22} />
                <X size={11} className="shrink-0 text-oz2-text-faint" />
              </span>
            ))}
          </div>
          <ChevronsUpDown size={14} className="shrink-0 text-oz2-text-faint" />
        </button>
      </PopoverTrigger>
      <PopoverContent
        hideWhenDetached={false}
        className="w-full overflow-hidden rounded-oz2-card border border-oz2-border bg-oz2-bg-elev p-0 text-oz2-text shadow-oz2-md"
        style={{ width }}
        align="start"
        side="bottom"
        sideOffset={6}
      >
        <DropdownInput
          value={search}
          onChange={setSearch}
          placeholder="Search peers by name or ip..."
        />
        {options.length === 0 && !search && (
          <DropdownInfoText>No peers available to select.</DropdownInfoText>
        )}
        {filteredItems.length === 0 && search !== "" && (
          <DropdownInfoText>
            There are no peers matching your search.
          </DropdownInfoText>
        )}
        {filteredItems.length > 0 && (
          <VirtualScrollAreaList
            items={filteredItems}
            estimatedItemHeight={37}
            onSelect={(item) => toggle(item.id)}
            renderItem={(option) => {
              const isSelected = !!option.id && value.includes(option.id);
              return (
                <div
                  className={cn(
                    "flex w-full items-center justify-between gap-2 text-sm",
                    isSelected ? "text-oz2-text" : "text-oz2-text-2",
                  )}
                >
                  <TextWithTooltip text={option.name} maxChars={24} />
                  <span className="flex items-center gap-1 font-mono text-[10px] font-medium text-oz2-text-muted">
                    <MapPin size={12} />
                    {option.ip}
                  </span>
                </div>
              );
            }}
          />
        )}
      </PopoverContent>
    </Popover>
  );
}
