"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import useFetchApi from "@utils/api";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { ActivityEvent } from "@/interfaces/ActivityEvent";
import AuditTimelineV2 from "@/modules/activity/v2/AuditTimelineV2";

// /events/audit — phase-5.11 entry point. Chrome (OzShell + sidebar
// + topbar) lives in (v2-dashboard)/layout.tsx → V2DashboardLayout,
// which already wraps the page in UsersProvider so the timeline's
// initiator-name resolution works without extra wiring. This
// component just owns the data fetch + the read-permission gate.

export default function Activity() {
  const { permission } = usePermissions();
  const { data: events, isLoading } =
    useFetchApi<ActivityEvent[]>("/events/audit");

  return (
    <RestrictedAccess hasAccess={permission.events.read}>
      <AuditTimelineV2 events={events} isLoading={isLoading} />
    </RestrictedAccess>
  );
}
