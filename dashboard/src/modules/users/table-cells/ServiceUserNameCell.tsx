import { IconSettings2 } from "@tabler/icons-react";
import { cn } from "@utils/helpers";
import React from "react";
import { User } from "@/interfaces/User";

type Props = {
  user: User;
};

export default function ServiceUserNameCell({ user }: Readonly<Props>) {
  return (
    <div className={cn("flex gap-4 px-2 py-1 items-center")}>
      <div
        className={cn(
          "w-8 h-8 rounded-full relative flex items-center justify-center uppercase text-md font-medium",
          "bg-neutral-200 text-neutral-700",
          "dark:bg-nb-gray-900 dark:text-white",
        )}
      >
        <IconSettings2 size={14} />
      </div>
      <div className={"flex flex-col justify-center"}>
        <span className={cn("text-base font-medium flex items-center gap-3")}>
          {user.name || user.id}
        </span>
      </div>
    </div>
  );
}
