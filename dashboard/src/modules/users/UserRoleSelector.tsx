import { CommandItem } from "@components/Command";
import { Popover, PopoverContent, PopoverTrigger } from "@components/Popover";
import { ScrollArea } from "@components/ScrollArea";
import { cn } from "@utils/helpers";
import { isOpenzroHosted } from "@utils/openzro";
import { Command, CommandGroup, CommandList } from "cmdk";
import { trim } from "lodash";
import {
  ChevronsUpDown,
  Cog,
  CreditCard,
  EyeIcon,
  NetworkIcon,
  User2,
} from "lucide-react";
import * as React from "react";
import { useState } from "react";
import OpenzroIcon from "@/assets/icons/OpenzroIcon";
import { useDialog } from "@/contexts/DialogProvider";
import { useLoggedInUser } from "@/contexts/UsersProvider";
import { useElementSize } from "@/hooks/useElementSize";
import { Role, User } from "@/interfaces/User";

interface MultiSelectProps {
  value?: Role;
  onChange: (item: Role) => void;
  disabled?: boolean;
  popoverWidth?: "auto" | number;
  hideOwner?: boolean;
  currentUser?: User;
  customTrigger?: React.ReactNode;
  side?: "top" | "bottom" | "left" | "right";
  align?: "start" | "center" | "end";
}

export const UserRoles = [
  {
    name: "Owner",
    value: Role.Owner,
    icon: OpenzroIcon,
  },
  {
    name: "Admin",
    value: Role.Admin,
    icon: Cog,
  },
  {
    name: "Network Admin",
    value: Role.NetworkAdmin,
    icon: NetworkIcon,
  },
  {
    name: "Billing Admin",
    value: Role.BillingAdmin,
    icon: CreditCard,
  },
  {
    name: "Auditor",
    value: Role.Auditor,
    icon: EyeIcon,
  },
  {
    name: "User",
    value: Role.User,
    icon: User2,
  },
];

export function UserRoleSelector({
  onChange,
  value,
  disabled = false,
  popoverWidth = "auto",
  hideOwner = false,
  currentUser,
  customTrigger,
  side = "bottom",
  align = "start",
}: Readonly<MultiSelectProps>) {
  const [inputRef, { width }] = useElementSize<
    HTMLButtonElement | HTMLDivElement
  >();
  const { isOwner } = useLoggedInUser();
  const { confirm } = useDialog();

  const toggle = async (item: Role) => {
    if (item === Role.Owner) {
      let ok = await confirm({
        title: "Transfer Ownership?",
        type: "warning",
        description: (
          <div className={"inline-block"}>
            This action will transfer the{" "}
            <span className={"text-openzro inline font-medium"}>Owner</span>{" "}
            role to{" "}
            {currentUser ? (
              <span className={"text-openzro inline font-medium"}>
                {currentUser.name}
              </span>
            ) : (
              "this user"
            )}{" "}
            and leave you with the{" "}
            <span className={"text-openzro inline font-medium"}>Admin</span>{" "}
            role. This action can only be undone if the new owner transfers the
            role back to you.
          </div>
        ),
      });
      if (!ok) return;
    }

    const isSelected = value == item;
    if (!isSelected) onChange && onChange(item);
    setOpen(false);
  };

  const [open, setOpen] = useState(false);

  const selectedRole = UserRoles.find((role) => role.value === value);

  return (
    <Popover
      open={open}
      onOpenChange={(isOpen) => {
        setOpen(isOpen);
      }}
    >
      {customTrigger ? (
        <PopoverTrigger asChild>
          <div ref={inputRef} className={"group/user-role-selector"}>
            {customTrigger}
          </div>
        </PopoverTrigger>
      ) : (
        <PopoverTrigger
          ref={inputRef}
          disabled={disabled}
          data-cy={"user-role-selector"}
          className={cn(
            "group/user-role-selector inline-flex h-[34px] w-full items-center justify-between gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 text-[13px] text-oz2-text transition-colors",
            "hover:border-oz2-border-strong hover:bg-oz2-hover",
            "[outline:none] focus-visible:border-oz2-acc focus-visible:ring-2 focus-visible:ring-oz2-acc/30",
            "disabled:cursor-not-allowed disabled:opacity-60",
          )}
        >
          {selectedRole && (
            <div className="flex items-center gap-2.5">
              <selectedRole.icon size={14} className="text-oz2-text-faint" />
              <span className="font-medium text-oz2-text whitespace-nowrap">
                {selectedRole.name}
              </span>
            </div>
          )}
          <ChevronsUpDown size={14} className="shrink-0 text-oz2-text-faint" />
        </PopoverTrigger>
      )}
      <PopoverContent
        className="w-full overflow-hidden rounded-oz2-card border border-oz2-border bg-oz2-bg-elev p-0 text-oz2-text shadow-oz2-md"
        style={{
          width: popoverWidth === "auto" ? width : popoverWidth,
        }}
        align={align}
        side={side}
        sideOffset={6}
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
            <ScrollArea
              className={
                "max-h-[380px] overflow-y-auto flex flex-col gap-1 pl-2 py-2 pr-3"
              }
            >
              <CommandGroup>
                <div className={"grid grid-cols-1 gap-1"}>
                  {UserRoles.map((item) => {
                    if (!isOwner && item.value === Role.Owner) return null;
                    if (hideOwner && item.value === Role.Owner) return null;

                    if (item.value === Role.BillingAdmin && !isOpenzroHosted())
                      return null;

                    const isSelected = item.value === value;
                    return (
                      <CommandItem
                        key={item.value}
                        value={item.value}
                        data-cy={"user-role-selector-item"}
                        className={
                          "flex items-center gap-2.5 rounded-[7px] px-2.5 py-2 text-[13px] font-medium transition-colors " +
                          (isSelected
                            ? "bg-oz2-acc-soft text-oz2-acc-text"
                            : "text-oz2-text hover:bg-oz2-hover")
                        }
                        onSelect={() => toggle(item.value)}
                        onClick={(e) => e.preventDefault()}
                      >
                        <item.icon
                          size={14}
                          className={
                            isSelected ? "text-oz2-acc-text" : "text-oz2-text-faint"
                          }
                        />
                        <span className="whitespace-nowrap">{item.name}</span>
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
