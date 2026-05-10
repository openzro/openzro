"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import useFetchApi from "@utils/api";
import React from "react";
import GroupsProvider from "@/contexts/GroupsProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import PoliciesProvider from "@/contexts/PoliciesProvider";
import { Policy } from "@/interfaces/Policy";
import AccessControlTableV2 from "@/modules/access-control/v2/AccessControlTableV2";

// Access Control — phase-5.6 entry point. Chrome (OzShell + sidebar
// + topbar) lives in (v2-dashboard)/layout.tsx → V2DashboardLayout.
// GroupsProvider + PoliciesProvider wrap the table so the cell
// renderers and the inline create/edit modals resolve usePolicies()
// and useGroups() from this tree (the modals themselves render inside
// AccessControlTableV2 in the same React subtree).

export default function AccessControlPage() {
  const { data: policies, isLoading } = useFetchApi<Policy[]>("/policies");
  const { permission } = usePermissions();

  return (
    <RestrictedAccess hasAccess={permission.policies.read}>
      <GroupsProvider>
        <PoliciesProvider>
          <AccessControlTableV2 policies={policies} isLoading={isLoading} />
        </PoliciesProvider>
      </GroupsProvider>
    </RestrictedAccess>
  );
}
