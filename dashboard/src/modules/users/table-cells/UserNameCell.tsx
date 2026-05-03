import { cn, generateColorFromUser } from "@utils/helpers";
import { Ban, Clock, Cog } from "lucide-react";
import React from "react";
import { User, UserIssued } from "@/interfaces/User";
import SCIMBadge from "@/modules/common/SCIMBadge";

type Props = {
  user: User;
};
export default function UserNameCell({ user }: Readonly<Props>) {
  const status = user.status;
  const isCurrent = user.is_current;

  return (
    <div
      className={cn("flex gap-4 px-2 py-1 items-center")}
      data-cy={"user-name-cell"}
    >
      <div
        className={
          // Avatar circle: pick a soft neutral on white so the
          // single-letter initial pops without competing with the
          // page surface. Dark theme keeps its existing nb-gray-900
          // bubble. The per-user `style.color` (derived from the user
          // hash) supplies the letter colour in both themes.
          "w-10 h-10 rounded-full relative flex items-center justify-center uppercase text-md font-medium bg-neutral-200 dark:bg-nb-gray-900"
        }
        style={{
          color: generateColorFromUser(user),
        }}
      >
        {!user?.name && !user?.id && <Cog size={12} />}
        {user?.name?.charAt(0) || user?.id?.charAt(0)}
        {(status == "invited" || status == "blocked") && (
          <div
            className={cn(
              "w-5 h-5 absolute -right-1 -bottom-1 rounded-full flex items-center justify-center border-2",
              "bg-neutral-100 border-white",
              "dark:bg-nb-gray-930 dark:border-nb-gray-950",
              status == "invited" && "bg-yellow-400 text-yellow-900",
              status == "blocked" && "bg-red-500 text-red-100",
            )}
          >
            {status == "invited" && <Clock size={12} />}
            {status == "blocked" && <Ban size={12} />}
          </div>
        )}
      </div>
      <div className={"flex flex-col justify-center"}>
        <span className={cn("text-base font-medium flex items-center gap-3")}>
          {user.name || user.id}
          {isCurrent && (
            <span
              className={cn(
                "rounded-full text-[9px] uppercase tracking-wider px-2 py-2 leading-[0] border",
                "bg-sky-100 border-sky-300 text-sky-800",
                "dark:bg-sky-900 dark:border-sky-700 dark:text-sky-200",
              )}
            >
              You
            </span>
          )}
          {user.issued === UserIssued.INTEGRATION && <SCIMBadge />}
        </span>
        <span className={cn("text-sm text-neutral-500 dark:text-nb-gray-400")}>
          {user.email}
        </span>
      </div>
    </div>
  );
}
