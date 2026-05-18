"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";
import ControlCenterView from "@/modules/control-center/ControlCenterView";

// Control Center — read-only access-graph view (openZro #39,
// ADR-0017 Phase 2). Chrome (OzShell + sidebar + topbar) lives in
// (v2-dashboard)/layout.tsx → V2DashboardLayout.
//
// RBAC mirrors the backend 1:1: GET /api/control-center is gated to
// modules.Settings + operations.Update (admin-only, owner-decided —
// the access graph is a sensitive audit surface), so the UI guard is
// permission.settings.update. The backend is the real gate (403);
// this just hides the screen for non-admins.
//
// P2 is the gated, navigable shell. Data + focus tabs (P3) and the
// xyflow graph (P4) land in subsequent Phase-2 commits.

export default function ControlCenterPage() {
  const { permission } = usePermissions();

  return (
    <RestrictedAccess hasAccess={permission.settings.update}>
      <ControlCenterView />
    </RestrictedAccess>
  );
}
