"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import React from "react";
import GroupsProvider from "@/contexts/GroupsProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import PoliciesProvider from "@/contexts/PoliciesProvider";
import PolicyEditorShell from "@/modules/access-control/v2/PolicyEditorShell";

// /access-control/new — dedicated page for creating a policy. The
// modal-based create flow lived on until the page editor grew a Live
// Preview + Impact rail; now that the rail makes the page genuinely
// useful during creation (the operator watches the policy take shape
// as they fill the form), the two flows converge here. Modal create
// path is gone from AccessControlTableV2 — only the inline
// "Create policy for this route" flow inside RouteModal still uses
// the legacy AccessControlModalContent.

export default function NewAccessControlPolicyPage() {
  const { permission } = usePermissions();

  return (
    <RestrictedAccess hasAccess={permission.policies.create}>
      <GroupsProvider>
        <PoliciesProvider>
          <PolicyEditorShell />
        </PoliciesProvider>
      </GroupsProvider>
    </RestrictedAccess>
  );
}
