import dayjs from "dayjs";
import React from "react";
import OzPill from "@/components/v2/OzPill";
import { User } from "@/interfaces/User";

// UserMFACellV2 shows whether the user has two-factor authentication.
// The server fills mfa_enrolled for admins only, so without it the cell
// shows a dash. A lockout after repeated wrong codes is shown with the
// time it ends.

type Props = {
  user: User;
};

export default function UserMFACellV2({ user }: Readonly<Props>) {
  if (user.mfa_enrolled === undefined || user.is_service_user) {
    return <span className="text-[13px] text-oz2-text-faint">—</span>;
  }
  if (user.mfa_locked_until) {
    const until = dayjs(user.mfa_locked_until).format("HH:mm");
    return (
      <OzPill
        variant="err"
        title={`Too many wrong codes. Locked until ${until}.`}
      >
        Locked until {until}
      </OzPill>
    );
  }
  return user.mfa_enrolled ? (
    <OzPill variant="ok">On</OzPill>
  ) : (
    <OzPill variant="warn">Off</OzPill>
  );
}
