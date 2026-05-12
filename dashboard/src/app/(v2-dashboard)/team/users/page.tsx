"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import useFetchApi from "@utils/api";
import React from "react";
import { useGroups } from "@/contexts/GroupsProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { User } from "@/interfaces/User";
import UsersTableV2 from "@/modules/users/v2/UsersTableV2";

// /team/users — phase-5.8 entry point. Chrome (OzShell + sidebar +
// topbar) lives in (v2-dashboard)/layout.tsx → V2DashboardLayout,
// which already wraps the page in GroupsProvider so the cells'
// useGroups() resolves naturally. This component just owns the data
// fetch + the read-permission gate.

export default function TeamUsers() {
  const { isLoading: isGroupsLoading } = useGroups();
  const { permission } = usePermissions();
  const { data: users, isLoading } = useFetchApi<User[]>(
    "/users?service_user=false",
  );

  return (
    <RestrictedAccess hasAccess={permission.users.read}>
      <UsersTableV2 users={users} isLoading={isLoading || isGroupsLoading} />
    </RestrictedAccess>
  );
}
