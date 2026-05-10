"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import React from "react";
import PeersProvider from "@/contexts/PeersProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import NetworkTrafficV2 from "@/modules/network-traffic/v2/NetworkTrafficV2";

// /events/network-traffic — phase-5.12 entry point. Chrome (OzShell
// + sidebar + topbar) lives in (v2-dashboard)/layout.tsx →
// V2DashboardLayout. PeersProvider wraps the body so the timeline
// can resolve source_ip / dest_ip / peer_id to friendly peer names
// and country flags; without it every row falls through to the bare
// IP fallback.

export default function NetworkTraffic() {
  const { permission } = usePermissions();

  return (
    <RestrictedAccess hasAccess={permission.events.read}>
      <PeersProvider>
        <NetworkTrafficV2 />
      </PeersProvider>
    </RestrictedAccess>
  );
}
