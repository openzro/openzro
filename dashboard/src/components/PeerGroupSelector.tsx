import Badge from "@components/Badge";
import { CommandItem } from "@components/Command";
import { DropdownInfoText } from "@components/DropdownInfoText";
import FullTooltip from "@components/FullTooltip";
import InlineLink from "@components/InlineLink";
import { Popover, PopoverContent, PopoverTrigger } from "@components/Popover";
import { Radio, RadioItem } from "@components/Radio";
import { ScrollArea } from "@components/ScrollArea";
import {
  OzTabs as Tabs,
  OzTabsContent as TabsContent,
  OzTabsList as TabsList,
  OzTabsTrigger as TabsTrigger,
} from "@/components/v2/OzTabs";
import { AccessControlGroupCount } from "@components/ui/AccessControlGroupCount";
import GroupBadge from "@components/ui/GroupBadge";
import GroupBadgeWithEditPeers from "@components/ui/GroupBadgeWithEditPeers";
import ResourceBadge from "@components/ui/ResourceBadge";
import TextWithTooltip from "@components/ui/TextWithTooltip";
import { VirtualScrollAreaList } from "@components/VirtualScrollAreaList";
import { useSearch } from "@hooks/useSearch";
import useSortedDropdownOptions from "@hooks/useSortedDropdownOptions";
import { IconArrowBack } from "@tabler/icons-react";
import useFetchApi from "@utils/api";
import { cn } from "@utils/helpers";
import { Command, CommandGroup, CommandInput, CommandList } from "cmdk";
import { sortBy, trim, unionBy } from "lodash";
import {
  ChevronsUpDown,
  FolderGit2,
  GlobeIcon,
  Layers3,
  Layers3Icon,
  MonitorSmartphoneIcon,
  NetworkIcon,
  SearchIcon,
  WorkflowIcon,
} from "lucide-react";
import * as React from "react";
import { Fragment, useEffect, useMemo, useState } from "react";
import Skeleton from "react-loading-skeleton";
import OzCheckbox from "@/components/v2/OzCheckbox";
import { useGroups } from "@/contexts/GroupsProvider";
import { useElementSize } from "@/hooks/useElementSize";
import type { Group, GroupPeer, GroupResource } from "@/interfaces/Group";
import { NetworkResource } from "@/interfaces/Network";
import type { Peer } from "@/interfaces/Peer";
import { PolicyRuleResource } from "@/interfaces/Policy";
import { User } from "@/interfaces/User";
import { HorizontalUsersStack } from "@/modules/users/HorizontalUsersStack";

interface MultiSelectProps {
  values: Group[];
  onChange: React.Dispatch<React.SetStateAction<Group[]>>;
  peer?: Peer;
  max?: number;
  disabled?: boolean;
  popoverWidth?: "auto" | number;
  hideAllGroup?: boolean;
  showPeerCount?: boolean;
  disableInlineRemoveGroup?: boolean;
  saveGroupAssignments?: boolean;
  showRoutes?: boolean;
  disabledGroups?: Group[];
  dataCy?: string;
  showResourceCounter?: boolean;
  showResources?: boolean;
  resource?: PolicyRuleResource;
  onResourceChange?: (resource?: PolicyRuleResource) => void;
  placeholder?: string;
  customTrigger?: React.ReactNode;
  align?: "start" | "end";
  side?: "top" | "bottom";
  users?: User[];
}
export function PeerGroupSelector({
  onChange,
  values,
  peer = undefined,
  max,
  disabled = false,
  popoverWidth = "auto",
  hideAllGroup = false,
  showPeerCount = false,
  disableInlineRemoveGroup = false,
  saveGroupAssignments = true,
  showRoutes = false,
  disabledGroups,
  dataCy = "group-selector-dropdown",
  showResourceCounter = true,
  showResources = false,
  resource,
  onResourceChange,
  placeholder = "Add or select group(s)...",
  customTrigger,
  align = "start",
  side = "bottom",
  users,
}: Readonly<MultiSelectProps>) {
  const { groups, dropdownOptions, setDropdownOptions, addDropdownOptions } =
    useGroups();
  const searchRef = React.useRef<HTMLInputElement>(null);
  const [inputRef, { width }] = useElementSize<
    HTMLButtonElement | HTMLSpanElement
  >();
  const [search, setSearch] = useState("");
  const { data: resources, isLoading } = useFetchApi<NetworkResource[]>(
    "/networks/resources",
  );

  // Update dropdown options when groups change
  useEffect(() => {
    if (!groups) return;
    const sortedGroups = sortBy([...groups], "name");

    const clientGroups = dropdownOptions.filter(
      (group) => group.keepClientState,
    );
    let uniqueGroups = unionBy(sortedGroups, dropdownOptions, "name");
    uniqueGroups = unionBy(clientGroups, uniqueGroups, "name");

    uniqueGroups = hideAllGroup
      ? uniqueGroups.filter((group) => group.name !== "All")
      : uniqueGroups;

    setDropdownOptions(uniqueGroups);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [groups]);

  const toggleGroupByName = (name: string) => {
    const isSelected = values.find((group) => group.name == name) != undefined;
    if (isSelected) {
      deselectGroup(name);
    } else {
      selectGroup(name);
    }
  };

  // Add group to the groupOptions if it does not exist
  const selectGroup = (name: string) => {
    onResourceChange?.(undefined);
    const group = groups?.find((group) => group.name == name);
    const option = dropdownOptions.find((option) => option.name == name);
    const groupPeers: GroupPeer[] | undefined =
      (group?.peers as GroupPeer[]) || [];
    const groupResources: GroupResource[] | undefined =
      (group?.resources as GroupResource[]) || [];

    if (peer) groupPeers?.push({ id: peer?.id as string, name: peer?.name });

    if (!group && !option) {
      addDropdownOptions([
        { name: name, peers: groupPeers, resources: groupResources },
      ]);
    }

    if (max == 1 && values.length == 1) {
      onChange([
        {
          name: name,
          id: group?.id,
          peers: groupPeers,
          resources: groupResources,
        },
      ]);
    } else {
      onChange((previous) => [
        ...previous,
        {
          name: name,
          id: group?.id,
          peers: groupPeers,
          resources: groupResources,
        },
      ]);
    }

    if (max == 1) setOpen(false);
  };

  // Remove group from the groupOptions if it does not have an id
  const deselectGroup = (name: string) => {
    onChange((previous) => {
      return previous.filter((group) => group.name != name);
    });
  };

  // Check if the searched group does not exist
  const searchedGroupNotFound = useMemo(() => {
    const isSearching = search.length > 0;
    const groupDoesNotExist =
      dropdownOptions.filter((item) => item.name == trim(search)).length == 0;
    const isAllGroup = search.toLowerCase() == "all";
    return isSearching && groupDoesNotExist && !isAllGroup;
  }, [search, dropdownOptions]);

  const [open, setOpen] = useState(false);

  const folderIcon = useMemo(() => {
    return <FolderGit2 size={12} className={"shrink-0"} />;
  }, []);

  const peerIcon = useMemo(() => {
    return <MonitorSmartphoneIcon size={14} className={"shrink-0"} />;
  }, []);

  const [slice, setSlice] = useState(10);

  const [tab, setTab] = useState("groups");

  useEffect(() => {
    if (open) {
      setTimeout(() => {
        setSlice(dropdownOptions.length);
      }, 100);
    } else {
      setSlice(10);
    }
  }, [open, dropdownOptions]);

  const onPeerAssignmentChange = (oldGroup: Group, newGroup: Group) => {
    const filtered = values.filter((group) => group.name !== oldGroup.name);
    const union = unionBy([newGroup], filtered, "name");
    onChange(union);
  };

  const sortedDropdownOptions = useSortedDropdownOptions(
    dropdownOptions,
    values,
    open,
  );

  // Reset the search input when switching tabs
  useEffect(() => {
    setSearch("");
    setTimeout(() => {
      searchRef.current?.focus();
    }, 0);
  }, [tab]);

  const searchPlaceholder =
    tab === "groups"
      ? 'Search groups or add new group by pressing "Enter"...'
      : "Search resource...";

  const selectResource = (resource?: NetworkResource) => {
    onResourceChange?.(
      resource
        ? ({
            id: resource?.id,
            type: resource?.type,
          } as PolicyRuleResource)
        : undefined,
    );
    onChange([]);
  };

  return (
    <Popover
      open={open}
      onOpenChange={(isOpen) => {
        setOpen(isOpen);
        if (!isOpen && search.length > 0) {
          setTimeout(() => {
            setSearch("");
          }, 200);
        }
      }}
    >
      <PopoverTrigger asChild>
        {customTrigger ? (
          <div ref={inputRef} className={"w-full"}>
            {customTrigger}
          </div>
        ) : (
          <button
            className={cn(
              // Trigger paint adopted from OzSelect for visual unity
              // across v2 forms. Min-height stays generous so multiple
              // group chips wrap without crushing — chips run ~22-26px
              // tall, so 38px floor + py-1.5 gives them room.
              "group relative flex w-full min-h-[38px] items-center justify-between gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 py-1.5 text-[13px] text-oz2-text-faint transition-colors",
              "hover:border-oz2-border-strong hover:bg-oz2-hover",
              // focus-visible (not focus) — mouse click moves focus
              // into the popover content; the trigger keeping the
              // violet ring after open made it look like the popover
              // had a stray blue border.
              "[outline:none] focus-visible:[outline:none] focus-visible:border-oz2-acc focus-visible:ring-2 focus-visible:ring-oz2-acc/30",
              "disabled:cursor-not-allowed disabled:pointer-events-none disabled:opacity-60",
            )}
            disabled={disabled}
            data-cy={dataCy}
            ref={inputRef}
          >
            <div
              className={
                "flex items-center gap-2 flex-wrap h-full"
              }
            >
              {resource && showResources && (
                <ResourceBadge
                  className={"py-[3px]"}
                  resource={resources?.find((r) => r.id === resource.id)}
                  onClick={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    selectResource();
                  }}
                  showX={true}
                />
              )}
              {values.map((group) => {
                return (
                  <div
                    key={group.name}
                    className={cn(
                      showPeerCount
                        ? "flex gap-x-1 gap-y-2 items-center justify-between w-full"
                        : "",
                    )}
                  >
                    {showPeerCount ? (
                      <GroupBadgeWithEditPeers
                        className={"py-[3px]"}
                        group={group}
                        key={group.name}
                        showNewBadge={true}
                        onPeerAssignmentChange={onPeerAssignmentChange}
                        useSave={saveGroupAssignments}
                      />
                    ) : (
                      <GroupBadge
                        className={"py-[3px]"}
                        group={group}
                        key={group.name}
                        showNewBadge={true}
                        onClick={(e) => {
                          e.preventDefault();
                          e.stopPropagation();
                          if (disableInlineRemoveGroup) return;
                          if (peer != undefined && group.name == "All") return; // Prevent removing the "All" group
                          toggleGroupByName(group.name);
                        }}
                        showX={
                          peer != undefined
                            ? group.name !== "All"
                            : !disableInlineRemoveGroup
                        }
                      />
                    )}
                  </div>
                );
              })}

              {values.length == 0 && !resource && (
                <span className={"pl-1"}>{placeholder}</span>
              )}
            </div>

            <div className="pl-2" data-cy="group-selector-open-close">
              <ChevronsUpDown
                size={14}
                className="shrink-0 text-oz2-text-faint transition-colors group-hover:text-oz2-text-2"
              />
            </div>
          </button>
        )}
      </PopoverTrigger>
      <PopoverContent
        // v2 paint override: drop the legacy nb-gray shadow + raise
        // to oz2-bg-elev so the popover reads as an elevated surface
        // sitting above the trigger row. The wider Popover primitive
        // is shared by 20 consumers; overriding inline here keeps the
        // blast radius scoped to the group selector.
        className="w-full overflow-hidden rounded-oz2-card border border-oz2-border bg-oz2-bg-elev p-0 text-oz2-text shadow-oz2-md"
        style={{
          width: popoverWidth === "auto" ? width : popoverWidth,
        }}
        align={align}
        side={side}
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
                data-cy="group-search-input"
                className={cn(
                  "h-10 w-full bg-transparent text-[13px] text-oz2-text outline-none",
                  "placeholder:text-oz2-text-faint",
                  "pl-10 pr-12",
                )}
                ref={searchRef}
                value={search}
                onValueChange={setSearch}
                placeholder={searchPlaceholder}
              />
              <div className="pointer-events-none absolute left-0 top-0 flex h-full items-center pl-4 text-oz2-text-faint">
                <SearchIcon size={14} />
              </div>
              <div className="absolute right-0 top-0 flex h-full items-center pr-3">
                <span className="inline-flex items-center gap-1 rounded-[5px] border border-oz2-border-soft bg-oz2-bg-sunken px-1.5 py-[3px] font-mono text-[10.5px] text-oz2-text-faint">
                  <IconArrowBack size={10} />
                </span>
              </div>
            </div>

            <Tabs defaultValue={"groups"} value={tab} onValueChange={setTab}>
              {showResources && <TabTriggers searchRef={searchRef} />}
              <TabsContent value={"groups"} className={"p-0 my-0"}>
                <CommandGroup>
                  <ScrollArea
                    className={cn(
                      "max-h-[195px] flex flex-col gap-1 pl-2 py-2 pr-3",
                      sortedDropdownOptions.length == 0 && !search && "py-0",
                    )}
                  >
                    {searchedGroupNotFound && (
                      <CommandItem
                        key={search}
                        onSelect={() => {
                          toggleGroupByName(search);
                          searchRef.current?.focus();
                        }}
                        value={search}
                        onClick={(e) => e.preventDefault()}
                      >
                        <span className="inline-flex items-center gap-1.5 rounded-[6px] border border-dashed border-oz2-border bg-oz2-bg-sunken/60 px-1.5 py-0.5 text-[12px] font-medium text-oz2-text-2">
                          {folderIcon}
                          {search}
                        </span>
                        <div className="text-[12px] text-oz2-text-muted">
                          Add this group by pressing{" "}
                          <span className="font-semibold text-oz2-acc-text">
                            {"'Enter'"}
                          </span>
                        </div>
                      </CommandItem>
                    )}

                    {sortedDropdownOptions.slice(0, slice).map((option) => {
                      const isSelected =
                        values.find((group) => group.name == option.name) !=
                        undefined;
                      const peerCount =
                        option.peers?.length ?? option?.peers_count ?? 0;

                      const isDisabled = disabledGroups
                        ? disabledGroups?.findIndex(
                            (g) => g.id === option.id,
                          ) !== -1
                        : false;

                      if (hideAllGroup && option?.name === "All") return;

                      return (
                        <FullTooltip
                          content={
                            <div className={"text-xs max-w-xs"}>
                              This group is already part of the routing peer and
                              can not be used for the access control groups.
                            </div>
                          }
                          disabled={!isDisabled}
                          className={"w-full block"}
                          key={option.name}
                        >
                          <CommandItem
                            key={option.name}
                            value={option.name + option.id}
                            disabled={isDisabled}
                            onSelect={() => {
                              if (peer != undefined && option.name == "All")
                                return; // Prevent removing the "All" group
                              if (isDisabled) return;
                              toggleGroupByName(option.name);
                              searchRef.current?.focus();
                            }}
                            className={cn(isDisabled && "opacity-40")}
                            onClick={(e) => e.preventDefault()}
                          >
                            <div className={"flex items-center gap-2"}>
                              <GroupBadge group={option} showNewBadge={true} />
                            </div>

                            <div className={"flex items-center gap-5"}>
                              {option?.id && showRoutes && (
                                <AccessControlGroupCount group_id={option.id} />
                              )}

                              {showResourceCounter && (
                                <ResourcesCounter group={option} />
                              )}

                              <div className={"flex gap-3 items-center"}>
                                {!users ? (
                                  <div className="flex items-center gap-2 text-[12px] font-medium text-oz2-text-muted">
                                    {peerIcon}
                                    {peerCount} Peer(s)
                                  </div>
                                ) : (
                                  <UsersCounter
                                    group={option}
                                    users={users}
                                    selected={isSelected}
                                  />
                                )}

                                <OzCheckbox checked={isSelected} />
                              </div>
                            </div>
                          </CommandItem>
                        </FullTooltip>
                      );
                    })}
                  </ScrollArea>
                </CommandGroup>
              </TabsContent>
              {showResources && (
                <TabsContent value={"resources"} className={"p-0 my-0"}>
                  <ResourcesList
                    search={search}
                    resources={resources}
                    isLoading={isLoading}
                    value={resource}
                    onChange={selectResource}
                  />
                </TabsContent>
              )}
            </Tabs>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

const TabTriggers = ({
  searchRef,
}: {
  searchRef: React.MutableRefObject<HTMLInputElement | null>;
}) => {
  return (
    <div className="px-3 pt-2">
      <TabsList>
        <TabsTrigger
          value={"groups"}
          onClick={() => searchRef.current?.focus()}
        >
          <FolderGit2
            className="text-oz2-text-faint transition-colors group-data-[state=active]/trigger:text-oz2-acc"
            size={14}
          />
          Groups
        </TabsTrigger>
        <TabsTrigger
          value={"resources"}
          onClick={() => searchRef.current?.focus()}
        >
          <Layers3Icon
            className="text-oz2-text-faint transition-colors group-data-[state=active]/trigger:text-oz2-acc"
            size={14}
          />
          Resource
        </TabsTrigger>
      </TabsList>
    </div>
  );
};

const UsersCounter = ({
  group,
  users,
  selected,
}: {
  group: Group;
  users: User[];
  selected: boolean;
}) => {
  const usersOfGroup =
    users?.filter((user) => user.auto_groups.includes(group.id as string)) ||
    [];

  if (usersOfGroup.length === 0) return null;

  return (
    <HorizontalUsersStack
      users={usersOfGroup}
      max={3}
      avatarClassName={cn(
        "border-oz2-border-soft",
        "bg-oz2-bg-sunken group-hover/user-stack:bg-oz2-hover",
        "group-hover/command-item:border-oz2-border",
      )}
    />
  );
};

const ResourcesCounter = ({ group }: { group: Group }) => {
  return group?.resources_count && group.resources_count > 0 ? (
    <div className="flex items-center gap-2 text-[12px] font-medium text-oz2-text-muted">
      <Layers3 size={14} className="shrink-0" />
      {group.resources_count} Resource(s)
    </div>
  ) : null;
};

const resourcesSearchPredicate = (item: NetworkResource, query: string) => {
  const lowerCaseQuery = query.toLowerCase();
  if (item.name.toLowerCase().includes(lowerCaseQuery)) return true;
  return item.address.toLowerCase().includes(lowerCaseQuery);
};

const ResourcesList = ({
  search,
  resources,
  isLoading,
  value,
  onChange,
}: {
  search: string;
  resources?: NetworkResource[];
  isLoading: boolean;
  value?: PolicyRuleResource;
  onChange: (resource: NetworkResource) => void;
}) => {
  const [filteredItems, _, setSearch] = useSearch(
    resources || [],
    resourcesSearchPredicate,
    { filter: true, debounce: 150 },
  );

  useEffect(() => {
    setSearch(search);
  }, [search, setSearch]);

  if (isLoading) {
    return (
      <div className={"max-h-[195px] flex flex-col gap-1 py-2 px-2"}>
        <Skeleton height={42} className={"rounded-md"} />
        <Skeleton height={42} className={"rounded-md"} />
        <Skeleton height={42} className={"rounded-md"} />
        <Skeleton height={42} className={"rounded-md"} />
      </div>
    );
  }

  if (search != "" && filteredItems.length == 0) {
    return (
      <DropdownInfoText className={"mt-5 max-w-sm mx-auto"}>
        There are no resources matching your search. Please try a different
        search term.
      </DropdownInfoText>
    );
  }

  if (search == "" && filteredItems.length == 0) {
    return (
      <DropdownInfoText className={"mt-5 max-w-sm mx-auto"}>
        There are no resources available yet. <br />
        Go to <InlineLink href={"/networks"}>Networks</InlineLink> to add some
        resources.
      </DropdownInfoText>
    );
  }

  return (
    <Radio defaultValue={value?.id} name={"resource"} value={value?.id}>
      <VirtualScrollAreaList
        items={filteredItems}
        onSelect={onChange}
        itemClassName="aria-selected:bg-oz2-hover"
        renderItem={(res) => {
          return (
            <Fragment key={res.id}>
              <div className={"flex items-center gap-2"}>
                <Badge
                  useHover={true}
                  data-cy={"group-badge"}
                  variant={"gray-ghost"}
                  className={cn("transition-all group whitespace-nowrap")}
                  onClick={(e) => {
                    e.preventDefault();
                  }}
                >
                  {res.type === "host" && (
                    <WorkflowIcon size={12} className={"shrink-0"} />
                  )}
                  {res.type === "domain" && (
                    <GlobeIcon size={12} className={"shrink-0"} />
                  )}
                  {res.type === "subnet" && (
                    <NetworkIcon size={12} className={"shrink-0"} />
                  )}

                  <TextWithTooltip text={res?.name || ""} maxChars={20} />
                </Badge>
              </div>

              <div className="flex items-center gap-5">
                <div className="flex items-center gap-2 text-[12px] font-medium text-oz2-text-muted">
                  {res.address}
                  <RadioItem value={res.id} />
                </div>
              </div>
            </Fragment>
          );
        }}
      />
    </Radio>
  );
};
