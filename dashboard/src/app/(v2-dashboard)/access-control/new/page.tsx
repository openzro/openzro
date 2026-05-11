"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import React from "react";
import GroupsProvider from "@/contexts/GroupsProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import PoliciesProvider from "@/contexts/PoliciesProvider";
import PolicyEditorShell from "@/modules/access-control/v2/PolicyEditorShell";

// /access-control/new — page entry point for the create-policy flow.
// Mirrors /access-control's provider stack (Groups + Policies) so the
// editor body's hooks resolve from the same tree. usePermissions
// gates against accidental URL access by non-creators.

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
