"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import useFetchApi from "@utils/api";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { NameserverGroup } from "@/interfaces/Nameserver";
import NameserversTableV2 from "@/modules/dns-nameservers/v2/NameserversTableV2";

// /dns/nameservers — phase-5.14 entry point. Chrome (OzShell +
// sidebar + topbar) lives in (v2-dashboard)/layout.tsx →
// V2DashboardLayout. The page itself just owns the data fetch +
// the read-permission gate; the v2 body owns the header + DnsTabs
// + topbar Add Nameserver CTA.

export default function NameServers() {
  const { permission } = usePermissions();
  const { data: nameserverGroups, isLoading } =
    useFetchApi<NameserverGroup[]>("/dns/nameservers");

  return (
    <RestrictedAccess hasAccess={permission.nameservers.read}>
      <NameserversTableV2
        nameserverGroups={nameserverGroups}
        isLoading={isLoading}
      />
    </RestrictedAccess>
  );
}
