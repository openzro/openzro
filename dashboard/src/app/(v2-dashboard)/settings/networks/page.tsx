"use client";

import React from "react";
import { useAccount } from "@/modules/account/useAccount";
import NetworkSettingsTab from "@/modules/settings/NetworkSettingsTab";
import SettingsPageShell from "@/modules/settings/v2/SettingsPageShell";

export default function SettingsNetworksPage() {
  const account = useAccount();
  return (
    <SettingsPageShell value="networks">
      {account && <NetworkSettingsTab account={account} />}
    </SettingsPageShell>
  );
}
