"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import useFetchApi from "@utils/api";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { SetupKey } from "@/interfaces/SetupKey";
import SetupKeysTableV2 from "@/modules/setup-keys/v2/SetupKeysTableV2";

// Setup Keys — phase-5.4 entry point. Chrome (OzShell + sidebar +
// topbar) lives in (v2-dashboard)/layout.tsx → V2DashboardLayout.
// auto_groups → Group hydration happens inside SetupKeysTableV2 via
// useGroups, so the page itself just owns the data fetch + the
// read-permission gate.

export default function SetupKeys() {
  const { data: setupKeys, isLoading } =
    useFetchApi<SetupKey[]>("/setup-keys");
  const { permission } = usePermissions();

  return (
    <RestrictedAccess hasAccess={permission.setup_keys.read}>
      <SetupKeysTableV2 setupKeys={setupKeys} isLoading={isLoading} />
    </RestrictedAccess>
  );
}
