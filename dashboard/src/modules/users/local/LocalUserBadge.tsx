"use client";

import { Tooltip, TooltipContent, TooltipTrigger } from "@components/Tooltip";
import { KeyRound } from "lucide-react";
import React from "react";
import { User } from "@/interfaces/User";

// isLocalUser is true for users whose password lives in openZro's
// embedded Dex (ADR-0024). Pending reservations and tombstones never
// reach the console (ADR-0026 D2).
export function isLocalUser(user: User): boolean {
  return user.local_credential_state !== undefined;
}

export default function LocalUserBadge({ user }: Readonly<{ user: User }>) {
  const deactivated = user.local_credential_state === "deactivated";
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          data-cy="local-user-badge"
          className="inline-flex items-center gap-1 rounded border border-oz2-acc-soft-2 bg-oz2-acc-soft px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wide text-oz2-acc-text"
        >
          <KeyRound size={10} />
          {deactivated ? "Local · deactivated" : "Local"}
        </span>
      </TooltipTrigger>
      <TooltipContent>
        <span className="text-xs">
          {deactivated
            ? "Deactivated local user. The password was removed and the account cannot sign in again."
            : "Local user. Signs in with an email and password managed by openZro."}
        </span>
      </TooltipContent>
    </Tooltip>
  );
}
