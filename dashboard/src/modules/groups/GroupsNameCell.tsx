import { GroupBadgeIcon } from "@components/ui/GroupBadgeIcon";
import TextWithTooltip from "@components/ui/TextWithTooltip";
import { cn } from "@utils/helpers";
import React from "react";
import CircleIcon from "@/assets/icons/CircleIcon";
import { Group, GroupIssued } from "@/interfaces/Group";
import SCIMBadge from "@/modules/common/SCIMBadge";

type Props = {
  active: boolean;
  group: Group;
};
export default function GroupsNameCell({ active, group }: Readonly<Props>) {
  return (
    <div className={cn("gap-3 dark:text-neutral-300 text-neutral-500 min-w-0")}>
      <div className={"flex flex-col gap-1"}>
        <div className={"flex gap-2.5 items-center"}>
          <div className={"flex items-center justify-center h-full"}>
            <GroupBadgeIcon id={group?.id} issued={group?.issued} />
          </div>

          <div className={"flex flex-col min-w-0"}>
            <div
              className={"font-medium flex gap-2 items-center justify-center"}
            >
              <TextWithTooltip text={group?.name} maxChars={25} />
              {group?.issued === GroupIssued.INTEGRATION && (
                <SCIMBadge description="Group provisioned via SCIM. Membership changes made here will be overwritten on the next IdP sync." />
              )}
            </div>
          </div>
          <CircleIcon
            size={8}
            active={active}
            inactiveDot={"gray"}
            className={"shrink-0"}
          />
        </div>
      </div>
    </div>
  );
}
