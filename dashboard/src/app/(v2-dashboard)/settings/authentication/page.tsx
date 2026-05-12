"use client";

import React from "react";
import { useAccount } from "@/modules/account/useAccount";
import AuthenticationTab from "@/modules/settings/AuthenticationTab";
import SettingsPageShell from "@/modules/settings/v2/SettingsPageShell";

export default function SettingsAuthenticationPage() {
  const account = useAccount();
  return (
    <SettingsPageShell value="authentication">
      {account && <AuthenticationTab account={account} />}
    </SettingsPageShell>
  );
}
