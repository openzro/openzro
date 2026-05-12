"use client";

import React from "react";
import { useAccount } from "@/modules/account/useAccount";
import ClientSettingsTab from "@/modules/settings/ClientSettingsTab";
import SettingsPageShell from "@/modules/settings/v2/SettingsPageShell";

export default function SettingsClientsPage() {
  const account = useAccount();
  return (
    <SettingsPageShell value="clients">
      {account && <ClientSettingsTab account={account} />}
    </SettingsPageShell>
  );
}
