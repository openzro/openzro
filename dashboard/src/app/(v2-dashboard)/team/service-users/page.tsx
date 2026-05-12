"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import useFetchApi from "@utils/api";
import React from "react";
import { useGroups } from "@/contexts/GroupsProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { User } from "@/interfaces/User";
import ServiceUsersTableV2 from "@/modules/users/v2/ServiceUsersTableV2";

// /team/service-users — v2 entry. Chrome (OzShell + sidebar + topbar)
// lives in (v2-dashboard)/layout.tsx → V2DashboardLayout, which wraps
// the page in GroupsProvider; this component just owns the data fetch
// + the read-permission gate. Header H1 + TeamTabs render inside the
// table component to keep the page-level wrapper trivial.

export default function ServiceUsers() {
  const { isLoading: isGroupsLoading } = useGroups();
  const { permission } = usePermissions();
  const { data: users, isLoading } = useFetchApi<User[]>(
    "/users?service_user=true",
  );

  return (
    <RestrictedAccess hasAccess={permission.users.read}>
      <ServiceUsersTableV2
        users={users}
        isLoading={isLoading || isGroupsLoading}
      />
    </RestrictedAccess>
  );
}
