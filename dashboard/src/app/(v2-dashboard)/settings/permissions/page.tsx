"use client";

import React from "react";
import { useAccount } from "@/modules/account/useAccount";
import PermissionsTab from "@/modules/settings/PermissionsTab";
import SettingsPageShell from "@/modules/settings/v2/SettingsPageShell";

export default function SettingsPermissionsPage() {
  const account = useAccount();
  return (
    <SettingsPageShell value="permissions">
      {account && <PermissionsTab account={account} />}
    </SettingsPageShell>
  );
}
