"use client";

import React from "react";
import { useAccount } from "@/modules/account/useAccount";
import DangerZoneTab from "@/modules/settings/DangerZoneTab";
import SettingsPageShell from "@/modules/settings/v2/SettingsPageShell";

export default function SettingsDangerZonePage() {
  const account = useAccount();
  return (
    <SettingsPageShell value="danger-zone" page="danger-zone">
      {account && <DangerZoneTab account={account} />}
    </SettingsPageShell>
  );
}
