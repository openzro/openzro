import React from "react";

// PeerLoginMFAHint says what the device sign-in 2FA toggle would do with
// the rules chosen above: nothing when neither covers anyone, or a
// refused save while a user it would apply to has no 2FA. The count is
// the one the server checks; it is absent for non-admins.

type Props = {
  enabled: boolean;
  local: boolean;
  federated: boolean;
  unenrolled?: { local: number; federated: number };
};

export default function PeerLoginMFAHint({
  enabled,
  local,
  federated,
  unenrolled,
}: Readonly<Props>) {
  if (!enabled) return null;
  let text: string | undefined;
  if (!local && !federated) {
    text =
      "Neither 2FA rule above is on, so no device sign-in asks for a code.";
  } else if (unenrolled) {
    const missing =
      (local ? unenrolled.local : 0) + (federated ? unenrolled.federated : 0);
    if (missing > 0) {
      text = `${missing} active ${
        missing === 1 ? "user it applies to has" : "users it applies to have"
      } no 2FA yet. Saving is refused until they set it up.`;
    }
  }
  if (!text) return null;
  return (
    <span
      data-cy="mfa-peer-login-hint"
      className="mt-1.5 block font-medium text-oz2-text-2"
    >
      {text}
    </span>
  );
}
