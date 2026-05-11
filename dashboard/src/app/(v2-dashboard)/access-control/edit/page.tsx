"use client";

import FullScreenLoading from "@components/ui/FullScreenLoading";
import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import useRedirect from "@hooks/useRedirect";
import useFetchApi from "@utils/api";
import { useSearchParams } from "next/navigation";
import React from "react";
import GroupsProvider from "@/contexts/GroupsProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import PoliciesProvider from "@/contexts/PoliciesProvider";
import { Policy } from "@/interfaces/Policy";
import PolicyEditorShell from "@/modules/access-control/v2/PolicyEditorShell";

// /access-control/edit?id=… — dedicated editor page for an existing
// policy. Uses a query-string id rather than a dynamic [id] segment
// to match the project's existing pattern (/peer?id=…, /network?id=…)
// and to sidestep Next's `output: export` requirement that every
// dynamic segment ship a generateStaticParams.

export default function EditAccessControlPolicyPage() {
  const { permission } = usePermissions();
  const queryParameter = useSearchParams();
  const policyId = queryParameter.get("id");

  const { data: policy, isLoading } = useFetchApi<Policy>(
    policyId ? `/policies/${policyId}` : "",
    true,
  );

  useRedirect("/access-control", false, !policyId);

  if (!policyId) return <FullScreenLoading height="auto" />;

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
