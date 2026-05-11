"use client";

import FullScreenLoading from "@components/ui/FullScreenLoading";
import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import useRedirect from "@hooks/useRedirect";
import useFetchApi from "@utils/api";
import { useParams } from "next/navigation";
import React from "react";
import GroupsProvider from "@/contexts/GroupsProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import PoliciesProvider from "@/contexts/PoliciesProvider";
import { Policy } from "@/interfaces/Policy";
import PolicyEditorShell from "@/modules/access-control/v2/PolicyEditorShell";

// /access-control/[id] — page entry point for editing a single policy.
// Fetches /policies/:id directly (the policies list endpoint is fine
// too, but a single-row fetch is leaner on page-direct loads) and
// redirects back to the list if the id is missing or invalid.

export default function EditAccessControlPolicyPage() {
  const { permission } = usePermissions();
  const params = useParams<{ id: string }>();
  const id = params?.id;

  const { data: policy, isLoading } = useFetchApi<Policy>(
    id ? `/policies/${id}` : "",
    true,
  );

  useRedirect("/access-control", false, !id);

  if (!id) return <FullScreenLoading height="auto" />;

  return (
    <RestrictedAccess hasAccess={permission.policies.read}>
      <GroupsProvider>
        <PoliciesProvider>
          {policy && !isLoading ? (
            <PolicyEditorShell policy={policy} />
          ) : (
            <FullScreenLoading height="auto" />
          )}
        </PoliciesProvider>
      </GroupsProvider>
    </RestrictedAccess>
  );
}
