"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import useFetchApi from "@utils/api";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { NameserverSettings } from "@/interfaces/NameserverSettings";
import DnsSettingsV2 from "@/modules/dns-nameservers/v2/DnsSettingsV2";
import { useGroupIdsToGroups } from "@/modules/groups/useGroupIdsToGroups";

// /dns/settings — phase-5.15 entry point. Chrome (OzShell + sidebar
// + topbar) lives in (v2-dashboard)/layout.tsx → V2DashboardLayout.
// Page owns the data fetch + the read-permission gate; the v2 body
// owns the header + DnsTabs + form.

export default function NameServerSettings() {
  const { permission } = usePermissions();
  const { data: settings, isLoading } =
    useFetchApi<NameserverSettings>("/dns/settings");
  const initialGroups = useGroupIdsToGroups(
    settings?.disabled_management_groups,
  );

  return (
    <RestrictedAccess hasAccess={permission.dns.read}>
      <DnsSettingsV2
        settings={settings}
        initialGroups={initialGroups}
        isLoading={isLoading}
      />
    </RestrictedAccess>
  );
}
