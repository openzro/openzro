"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import useFetchApi from "@utils/api";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Network } from "@/interfaces/Network";
import NetworksTableV2 from "@/modules/networks/v2/NetworksTableV2";

// Networks — phase-5.2 entry point. Chrome (OzShell + sidebar +
// topbar) lives in (v2-dashboard)/layout.tsx → V2DashboardLayout.
// This component owns the data fetch + the read-permission gate.

export default function Networks() {
  const { data: networks, isLoading } = useFetchApi<Network[]>("/networks");
  const { permission } = usePermissions();

  return (
    <RestrictedAccess hasAccess={permission.networks.read}>
      <NetworksTableV2 data={networks} isLoading={isLoading} />
    </RestrictedAccess>
  );
}
