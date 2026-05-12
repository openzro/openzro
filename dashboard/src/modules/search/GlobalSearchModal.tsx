import { DropdownInput } from "@components/DropdownInput";
import { Modal, ModalContent } from "@components/modal/Modal";
import { VirtualScrollAreaList } from "@components/VirtualScrollAreaList";
import { useSearch } from "@hooks/useSearch";
import useFetchApi from "@utils/api";
import { removeAllSpaces } from "@utils/helpers";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  CornerDownLeft,
  GlobeIcon,
  LayersIcon,
  NetworkIcon,
  TextSearchIcon,
  WorkflowIcon,
} from "lucide-react";
import { useRouter } from "next/navigation";
import * as React from "react";
import { useMemo } from "react";
import Skeleton from "react-loading-skeleton";
import { Network, NetworkResource } from "@/interfaces/Network";

type Props = {
  open: boolean;
  setOpen: (open: boolean) => void;
};

enum SearchType {
  Network = "network",
  NetworkResource = "network-resource",
}

type SearchResult<T, U extends SearchType> = {
  type: U;
  id: string;
  data: T;
  onAction?: (item: T) => void;
};

type NetworkSearchResult = SearchResult<Network, SearchType.Network>;
type ResourceSearchResult = SearchResult<
  NetworkResource,
  SearchType.NetworkResource
>;
type AnySearchResult = NetworkSearchResult | ResourceSearchResult;

const searchPredicate = (item: AnySearchResult, query: string) => {
  if (!query) return false;
  const lower = removeAllSpaces(query.toLowerCase());
  const { name, description, id } = item.data;
  const find = (s: string | undefined) =>
    removeAllSpaces(s?.toLowerCase()).includes(lower);

  if (item.type === SearchType.Network) {
    if (find(name)) return true;
    if (find(description)) return true;
    if (find(id)) return true;
  }

  if (item.type === SearchType.NetworkResource) {
    if (find(name)) return true;
    if (find(description)) return true;
    if (find(item.data?.address)) return true;
    if (find(id)) return true;
  }

  return false;
};

export const GlobalSearchModal = ({ open, setOpen }: Props) => {
  return open && <GlobalSearchModalContent open={open} setOpen={setOpen} />;
};

const GlobalSearchModalContent = ({ open, setOpen }: Props) => {
  const router = useRouter();

  const { data: networks, isLoading: isNetworksLoading } = useFetchApi<
    Network[]
  >("/networks", true, false, open, {
    key: "global-search-networks",
  });
  const { data: resources, isLoading: isResourcesLoading } = useFetchApi<
    NetworkResource[]
  >("/networks/resources", true, false, open, {
    key: "global-search-resources",
  });

  const findNetworkByResourceId = (resourceId: string) => {
    return networks?.find(
      (network) => network.resources?.some((res) => res === resourceId),
    );
  };

  const items: AnySearchResult[] = useMemo(() => {
    if (isNetworksLoading || isResourcesLoading) return [];
    const networkResults: NetworkSearchResult[] = (networks ?? []).map(
      (network) => ({
        type: SearchType.Network,
        id: network.id,
        data: network,
        onAction: () => router.push(`/network?id=${network.id}`),
      }),
    );

    const resourceResults: ResourceSearchResult[] = (resources ?? []).map(
      (resource) => ({
        type: SearchType.NetworkResource,
        id: resource.id,
        data: resource,
        onAction: () => {
          const network = findNetworkByResourceId(resource.id);
          if (network)
            router.push(`/network?id=${network.id}&resource=${resource.id}`);
        },
      }),
    );

    return [...networkResults, ...resourceResults];
  }, [isNetworksLoading, isResourcesLoading, networks, resources]);

  const [filteredItems, search, setSearch, setQuery, isSearching] = useSearch(
    items,
    searchPredicate,
    {
      filter: false,
      debounce: 350,
    },
  );

  const isLoading = isNetworksLoading || isResourcesLoading || isSearching;

  const networksCount = useMemo(() => {
    return filteredItems.filter((i) => i.type === SearchType.Network).length;
  }, [filteredItems]);

  const resourcesCount = useMemo(() => {
    return filteredItems.filter((i) => i.type === SearchType.NetworkResource)
      .length;
  }, [filteredItems]);

  return (
    <div>
      <Modal
        open={open}
        onOpenChange={(isOpen) => {
          if (!isOpen) setSearch("");
          setOpen(isOpen);
        }}
      >
        <ModalContent
          showClose={false}
          className={"py-0 overflow-hidden"}
          maxWidthClass={"max-w-xl"}
        >
          <DropdownInput
            hideEnterIcon={true}
            value={search}
            onChange={setSearch}
            autoFocus={true}
          />

          {search === "" && <BlankState />}

          {isLoading && search !== "" && <LoadingState />}

          {!isSearching && search !== "" && filteredItems.length === 0 && (
            <NotFoundState />
          )}

          {!isSearching && search != "" && filteredItems.length !== 0 && (
            <VirtualScrollAreaList
              items={filteredItems}
              maxHeight={400}
              scrollAreaClassName={"pt-0"}
              groupKey={(i) => i.type}
              estimatedItemHeight={48}
              estimatedHeadingHeight={32}
              heightAdjustment={5}
              onSelect={(item) => {
                const { onAction, data, type } = item;
                if (type === SearchType.Network) onAction?.(data);
                if (type === SearchType.NetworkResource) onAction?.(data);
              }}
              renderHeading={(item) => {
                return (
                  <div className="px-4 py-2 font-mono text-[10.5px] uppercase tracking-[0.08em] text-oz2-text-faint">
                    {item.type === SearchType.Network &&
                      `Networks (${networksCount})`}
                    {item.type === SearchType.NetworkResource &&
                      `Resources (${resourcesCount})`}
                  </div>
                );
              }}
              renderItem={(item) => {
                const network = findNetworkByResourceId(item.id);

                return (
                  <div className="flex w-full items-center justify-between gap-3">
                    <div className="flex min-w-0 items-center gap-3">
                      <div className="grid h-8 w-8 shrink-0 place-items-center rounded-oz2-input bg-oz2-bg-sunken text-oz2-text-2 group-aria-selected/list-item:bg-oz2-acc-soft group-aria-selected/list-item:text-oz2-acc-text transition-colors">
                        {item.type === SearchType.Network && (
                          <span className="font-mono text-[11px] font-medium uppercase tracking-[0.04em]">
                            {item.data.name.substring(0, 2)}
                          </span>
                        )}
                        {item.type === SearchType.NetworkResource && (
                          <ResourceIcon type={item.data.type} />
                        )}
                      </div>
                      <div className="min-w-0">
                        <div className="truncate text-[13px] text-oz2-text">
                          {item.data.name}
                          {network && (
                            <span className="text-oz2-text-muted"> · {network.name}</span>
                          )}
                        </div>
                        <div className="truncate text-[11.5px] text-oz2-text-muted">
                          {item.data.description}
                        </div>
                      </div>
                    </div>
                    <div className="flex shrink-0 items-center gap-4">
                      {item.type === SearchType.Network && (
                        <div className="inline-flex items-center gap-1.5 text-[11px] leading-none text-oz2-text-muted">
                          <LayersIcon size={12} />
                          {item.data?.resources?.length} Resource(s)
                        </div>
                      )}
                      {item.type === SearchType.NetworkResource && (
                        <div className="font-mono text-[10.5px] text-oz2-text-muted">
                          {item.data?.address}
                        </div>
                      )}
                      <div>
                        <CornerDownLeft
                          size={14}
                          className={
                            "opacity-0 group-aria-selected/list-item:opacity-100 group-list-item-aria-selected:opacity-100"
                          }
                        />
                      </div>
                    </div>
                  </div>
                );
              }}
            />
          )}
          <KeyboardShortcutsFooter />
        </ModalContent>
      </Modal>
    </div>
  );
};

const ResourceIcon = ({ type }: { type: NetworkResource["type"] }) => {
  const size = 14;
  switch (type) {
    case "host":
      return <WorkflowIcon size={size} />;
    case "domain":
      return <GlobeIcon size={size} />;
    case "subnet":
      return <NetworkIcon size={size} />;
    default:
      return <WorkflowIcon size={size} />;
  }
};

const BlankState = () => {
  return (
    <div className="flex items-center justify-center pb-8">
      <div className="text-center">
        <div className="mb-3 mt-3 flex items-center justify-center gap-2">
          <HintTile>
            <NetworkIcon size={16} />
          </HintTile>
          <HintTile>
            <WorkflowIcon size={16} />
          </HintTile>
          <HintTile>
            <GlobeIcon size={16} />
          </HintTile>
        </div>

        <div className="mb-1 text-[13.5px] font-medium text-oz2-text">
          Search for Networks and Resources
        </div>
        <div className="max-w-sm text-[12.5px] leading-[1.55] text-oz2-text-muted">
          Quickly find networks and associated resources. Start typing to
          search by name, description or address.
        </div>
      </div>
    </div>
  );
};

function HintTile({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid h-8 w-8 place-items-center rounded-oz2-input bg-oz2-acc-soft text-oz2-acc-text">
      {children}
    </div>
  );
}

const NotFoundState = () => {
  return (
    <div className="flex items-center justify-center pb-8">
      <div className="text-center">
        <div className="mb-3 mt-3 flex items-center justify-center">
          <div className="grid h-8 w-8 place-items-center rounded-oz2-input bg-oz2-bg-sunken text-oz2-text-muted">
            <TextSearchIcon size={16} />
          </div>
        </div>

        <div className="mb-1 text-[13.5px] font-medium text-oz2-text">
          Could not find any results
        </div>
        <div className="max-w-xs text-[12.5px] leading-[1.55] text-oz2-text-muted">
          {`We couldn't find any results. Please try a different search term.`}
        </div>
      </div>
    </div>
  );
};

const LoadingState = () => {
  return (
    <div className={"flex flex-col gap-1 px-3 mb-4 opacity-50"}>
      <Skeleton width={"100%"} height={40} />
      <Skeleton width={"100%"} height={40} />
      <Skeleton width={"100%"} height={40} />
    </div>
  );
};

// Bottom-of-modal hint strip teaching the four operations the
// operator can do from here: ↑↓ to scan results, ↵ to open, esc to
// dismiss, and the global ⌘K / Ctrl+K toggle. Platform-aware so the
// hint matches the actual key the user has to press.
const KeyboardShortcutsFooter = () => {
  const [mac, setMac] = React.useState(false);
  React.useEffect(() => {
    if (typeof navigator === "undefined") return;
    setMac(/Mac|iPod|iPhone|iPad/.test(navigator.platform));
  }, []);
  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-2 border-t border-oz2-border-soft bg-oz2-bg-sunken px-4 py-3 text-[12px] text-oz2-text-muted">
      <Shortcut label="Navigate">
        <KbdHint>
          <ArrowUpIcon size={11} />
        </KbdHint>
        <KbdHint>
          <ArrowDownIcon size={11} />
        </KbdHint>
      </Shortcut>
      <Shortcut label="Open">
        <KbdHint>
          <CornerDownLeft size={11} />
        </KbdHint>
      </Shortcut>
      <Shortcut label="Close">
        <KbdHint>esc</KbdHint>
      </Shortcut>
      <Shortcut label="Toggle">
        <KbdHint>{mac ? "⌘" : "Ctrl"}</KbdHint>
        <KbdHint>K</KbdHint>
      </Shortcut>
    </div>
  );
};

function Shortcut({
  children,
  label,
}: {
  children: React.ReactNode;
  label: string;
}) {
  return (
    <div className="flex items-center gap-1.5">
      <div className="flex items-center gap-1">{children}</div>
      <span className="text-oz2-text-muted">{label}</span>
    </div>
  );
}

function KbdHint({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="inline-flex h-[18px] min-w-[18px] items-center justify-center rounded-[4px] border border-oz2-border-soft bg-oz2-surface px-1 font-mono text-[10.5px] font-medium text-oz2-text-faint">
      {children}
    </kbd>
  );
}
