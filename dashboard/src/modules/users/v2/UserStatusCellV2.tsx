import React from "react";
import OzPill, { type OzPillVariants } from "@/components/v2/OzPill";
import { User } from "@/interfaces/User";

// UserStatusCellV2 — replaces the raw-Tailwind dot+label of the
// legacy UserStatusCell with an OzPill carrying the appropriate
// variant. Maps the API status string to v2 semantic tones:
//
//   active  → ok     (Active)
//   invited → warn   (Pending)
//   blocked → err    (Blocked)
//
// Unknown statuses fall through to the neutral default pill so
// future server-side states still render legibly.

type Props = {
  user: User;
};

const STATUS_META: Record<
  string,
  { label: string; variant: OzPillVariants["variant"] }
> = {
  active: { label: "Active", variant: "ok" },
  invited: { label: "Pending", variant: "warn" },
  blocked: { label: "Blocked", variant: "err" },
};

export default function UserStatusCellV2({ user }: Readonly<Props>) {
  const meta = STATUS_META[user.status] ?? {
    label: user.status || "—",
    variant: "default" as const,
  };
  return <OzPill variant={meta.variant}>{meta.label}</OzPill>;
}
