"use client";

import React from "react";
import { useAccount } from "@/modules/account/useAccount";
import DeviceAdmissionTab from "@/modules/settings/DeviceAdmissionTab";
import SettingsPageShell from "@/modules/settings/v2/SettingsPageShell";

export default function SettingsDeviceAdmissionPage() {
  const account = useAccount();
  return (
    <SettingsPageShell value="device-admission">
      {account && <DeviceAdmissionTab account={account} />}
    </SettingsPageShell>
  );
}
