"use client";

import { LockKeyhole } from "lucide-react";
import React from "react";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import {
  LOCAL_PASSWORD_MAX_BYTES,
  LOCAL_PASSWORD_MIN_CHARS,
  localPasswordError,
} from "@/modules/users/local/localPassword";

type Props = {
  password: string;
  confirm: string;
  onPasswordChange: (value: string) => void;
  onConfirmChange: (value: string) => void;
};

// LocalPasswordFields is the password + confirmation pair shared by the
// create and reset modals. The confirmation catches typos in a password
// the administrator has to hand over out of band.
export default function LocalPasswordFields({
  password,
  confirm,
  onPasswordChange,
  onConfirmChange,
}: Readonly<Props>) {
  const mismatch =
    confirm.length > 0 && confirm !== password ? "Passwords don't match." : "";
  return (
    <div className="flex flex-col gap-2">
      <OzLabel>Password</OzLabel>
      <OzHelpText>
        At least {LOCAL_PASSWORD_MIN_CHARS} characters and at most{" "}
        {LOCAL_PASSWORD_MAX_BYTES} bytes. Share it with the user through a
        secure channel.
      </OzHelpText>
      <OzInput
        type="password"
        autoComplete="new-password"
        data-cy="local-user-password"
        prefix={<LockKeyhole size={16} />}
        placeholder="Password"
        value={password}
        onChange={(e) => onPasswordChange(e.target.value)}
        error={localPasswordError(password)}
      />
      <OzInput
        type="password"
        autoComplete="new-password"
        data-cy="local-user-password-confirm"
        prefix={<LockKeyhole size={16} />}
        placeholder="Confirm password"
        value={confirm}
        onChange={(e) => onConfirmChange(e.target.value)}
        error={mismatch}
      />
    </div>
  );
}
