import {
  Cog,
  CreditCardIcon,
  EyeIcon,
  NetworkIcon,
  User2,
} from "lucide-react";
import React from "react";
import OpenzroIcon from "@/assets/icons/OpenzroIcon";
import OzPill from "@/components/v2/OzPill";
import { Role, User } from "@/interfaces/User";

// UserRoleCellV2 — handoff-flavored role chip. Owner / Admin land on
// the violet `acc` pill (matches the handoff's `oz-pill acc` for
// elevated roles); everyone else renders on the neutral default
// pill. Role icon + label otherwise mirror the legacy UserRoleCell
// so the column reads identically.

type Props = {
  user: User;
};

const ROLE_META: Record<
  Role,
  { label: string; icon: React.ReactNode; elevated: boolean }
> = {
  [Role.Owner]: {
    label: "Owner",
    icon: <OpenzroIcon size={12} />,
    elevated: true,
  },
  [Role.Admin]: {
    label: "Admin",
    icon: <Cog size={12} />,
    elevated: true,
  },
  [Role.User]: {
    label: "User",
    icon: <User2 size={12} />,
    elevated: false,
  },
  [Role.BillingAdmin]: {
    label: "Billing Admin",
    icon: <CreditCardIcon size={12} />,
    elevated: false,
  },
  [Role.Auditor]: {
    label: "Auditor",
    icon: <EyeIcon size={12} />,
    elevated: false,
  },
  [Role.NetworkAdmin]: {
    label: "Network Admin",
    icon: <NetworkIcon size={12} />,
    elevated: false,
  },
};

export default function UserRoleCellV2({ user }: Readonly<Props>) {
  const meta = ROLE_META[user.role];
  if (!meta) {
    return (
      <OzPill>
        <span>{String(user.role)}</span>
      </OzPill>
    );
  }
  return (
    <OzPill variant={meta.elevated ? "acc" : "default"}>
      <span aria-hidden className="inline-flex h-3 w-3 items-center justify-center">
        {meta.icon}
      </span>
      {meta.label}
    </OzPill>
  );
}
