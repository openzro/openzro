import React from "react";

// MFAUnenrolledHint tells the admin how many active users would be asked
// to set up 2FA if the toggle above it were on. The server returns the
// count to admins only; without it nothing is shown.

type Props = {
  count?: number;
  kind: "local" | "federated";
  dataCy: string;
};

export default function MFAUnenrolledHint({
  count,
  kind,
  dataCy,
}: Readonly<Props>) {
  if (count === undefined) return null;
  const who = kind === "local" ? "local" : "federated";
  const text =
    count === 0
      ? `No active ${who} user is missing 2FA.`
      : `${count} active ${who} ${
          count === 1 ? "user has" : "users have"
        } no 2FA yet. The Users list shows who.`;
  return (
    <span data-cy={dataCy} className="mt-1.5 block font-medium text-oz2-text-2">
      {text}
    </span>
  );
}
