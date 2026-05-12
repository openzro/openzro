"use client";

import React from "react";
import { useAccount } from "@/modules/account/useAccount";
import GroupsTab from "@/modules/settings/GroupsTab";
import SettingsPageShell from "@/modules/settings/v2/SettingsPageShell";

export default function SettingsGroupsPage() {
  const account = useAccount();
  return (
    <SettingsPageShell value="groups">
      {account && <GroupsTab account={account} />}
    </SettingsPageShell>
  );
}
