"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";
import IntegrationsV2 from "@/modules/integrations/v2/IntegrationsV2";

// /integrations — phase-5.17 entry point. Chrome (OzShell + sidebar
// + topbar) lives in (v2-dashboard)/layout.tsx → V2DashboardLayout.
// The v2 body owns the header + segmented sub-nav + sub-section
// rendering; the page itself is just the read-permission gate.

export default function Integrations() {
  const { permission } = usePermissions();

  return (
    <RestrictedAccess hasAccess={permission.settings.read}>
      <IntegrationsV2 />
    </RestrictedAccess>
  );
}
