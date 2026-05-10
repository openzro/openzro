import { cn } from "@utils/helpers";
import { Ban, Clock, Cog } from "lucide-react";
import React from "react";
import { User, UserIssued } from "@/interfaces/User";
import SCIMBadge from "@/modules/common/SCIMBadge";

// UserNameCellV2 — handoff (screens-2.jsx, TeamScreen) take on the
// user identity cell: gradient avatar (violet → pink) with 2-letter
// initials for human users, neutral surface with border for service
// users. Status overlay (invited / blocked) and the "You" + SCIM
// badges from the legacy UserNameCell are preserved verbatim — only
// the avatar paint and initial-letter logic change.

type Props = {
  user: User;
};

function initialsFor(user: User): string {
  const name = user.name?.trim();
  if (name) {
    // Pull initials from the first two words; fallback to first two
    // letters of a single word so single-name users still render two
    // characters in the bubble.
    const parts = name.split(/\s+/);
    if (parts.length >= 2) {
      return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
    }
    return name.slice(0, 2).toUpperCase();
  }
  if (user.email) return user.email.slice(0, 2).toUpperCase();
  if (user.id) return user.id.slice(0, 2).toUpperCase();
  return "";
}

export default function UserNameCellV2({ user }: Readonly<Props>) {
  const status = user.status;
  const isCurrent = user.is_current;
  const isService = Boolean(user.is_service_user);

  const initials = initialsFor(user);

  return (
    <div
      className="flex items-center gap-3 px-2 py-1"
      data-cy="user-name-cell"
    >
      <div
        className={cn(
          "relative grid h-9 w-9 flex-shrink-0 place-items-center rounded-full text-[12px] font-semibold uppercase",
          isService
            ? "border border-oz2-border bg-oz2-bg-sunken text-oz2-text-2"
            : "text-white",
        )}
        style={
          // Service users keep the neutral surface paint; everyone
          // else gets the handoff's violet → pink gradient bubble.
          isService
            ? undefined
            : { background: "linear-gradient(135deg,#a78bfa,#f472b6)" }
        }
      >
        {initials || <Cog size={14} />}
        {(status === "invited" || status === "blocked") && (
          <span
            aria-hidden
            className={cn(
              "absolute -bottom-0.5 -right-0.5 grid h-4 w-4 place-items-center rounded-full border-2 border-oz2-surface",
              status === "invited" && "bg-oz2-warn text-oz2-text-on-acc",
              status === "blocked" && "bg-oz2-err text-oz2-text-on-acc",
            )}
          >
            {status === "invited" && <Clock size={10} />}
            {status === "blocked" && <Ban size={10} />}
          </span>
        )}
      </div>
      <div className="flex min-w-0 flex-col justify-center">
        <span className="flex items-center gap-2 text-[14px] font-medium text-oz2-text">
          <span className="truncate">{user.name || user.id || "—"}</span>
          {isCurrent && (
            <span className="rounded-full border border-oz2-acc-soft-2 bg-oz2-acc-soft px-2 py-0.5 font-mono text-[10px] uppercase tracking-wider text-oz2-acc-text">
              You
            </span>
          )}
          {user.issued === UserIssued.INTEGRATION && <SCIMBadge />}
        </span>
        {user.email && (
          <span className="truncate text-[12.5px] text-oz2-text-muted">
            {user.email}
          </span>
        )}
      </div>
    </div>
  );
}
