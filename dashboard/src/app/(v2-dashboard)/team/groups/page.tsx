"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";
import GroupsTableV2 from "@/modules/groups/v2/GroupsTableV2";

// /team/groups — phase-5.9 entry point. Chrome (OzShell + sidebar +
// topbar) lives in (v2-dashboard)/layout.tsx → V2DashboardLayout,
// which already wraps the page in GroupsProvider so useGroupsUsage()
// inside GroupsTableV2 works without extra wiring. This component
// only owns the read-permission gate.

export default function TeamGroupsPage() {
  const { permission } = usePermissions();

  return (
    <RestrictedAccess hasAccess={permission.groups.read}>
      <GroupsTableV2 />
    </RestrictedAccess>
  );
}
