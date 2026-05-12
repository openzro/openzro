import { Bot } from "lucide-react";
import React from "react";
import OzPill, { type OzPillVariants } from "@/components/v2/OzPill";
import { User } from "@/interfaces/User";

// ServiceUserNameCellV2 — paint for the service-user row identity cell.
// Mirrors the handoff exemplar: small neutral avatar with a Bot glyph
// (machines, not humans) + monospace name + an inline status dot/pill.
// Status moves inline because service users only have two practical
// states (active / blocked) — promoting it to its own column wastes a
// row's worth of horizontal space when the same information fits next
// to the name as a quiet pill.

const STATUS_META: Record<
  string,
  { label: string; variant: OzPillVariants["variant"] }
> = {
  active: { label: "active", variant: "ok" },
  invited: { label: "pending", variant: "warn" },
  blocked: { label: "blocked", variant: "err" },
};

type Props = {
  user: User;
};

export default function ServiceUserNameCellV2({ user }: Readonly<Props>) {
  const status = STATUS_META[user.status];

  return (
    <div className="flex items-center gap-3 px-2 py-1" data-cy="user-name-cell">
      <div
        className="grid h-9 w-9 flex-shrink-0 place-items-center rounded-full border border-oz2-border bg-oz2-bg-sunken text-oz2-text-2"
        aria-hidden
      >
        <Bot size={15} />
      </div>
      <div className="flex min-w-0 flex-col justify-center">
        <span className="flex items-center gap-2">
          <span className="truncate font-mono text-[13px] font-medium text-oz2-text">
            {user.name || user.id || "—"}
          </span>
          {status ? (
            <OzPill variant={status.variant}>{status.label}</OzPill>
          ) : user.status ? (
            <OzPill variant="default">{user.status}</OzPill>
          ) : null}
        </span>
      </div>
    </div>
  );
}
