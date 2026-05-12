"use client";

import React from "react";
import AuthenticationProvidersTab from "@/modules/auth-providers/AuthenticationProvidersTab";
import SettingsPageShell from "@/modules/settings/v2/SettingsPageShell";

export default function SettingsAuthProvidersPage() {
  return (
    <SettingsPageShell value="auth-providers">
      <AuthenticationProvidersTab />
    </SettingsPageShell>
  );
}
