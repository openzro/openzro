"use client";

import FullScreenLoading from "@components/ui/FullScreenLoading";
import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import React, { useEffect, useState } from "react";
import GroupsProvider from "@/contexts/GroupsProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import PoliciesProvider from "@/contexts/PoliciesProvider";
import { Policy } from "@/interfaces/Policy";
import PolicyEditorShell from "@/modules/access-control/v2/PolicyEditorShell";

// /access-control/new — dedicated page for creating a policy. Most of
// the time `policy` is undefined and the editor starts blank. When
// RouteModal asks the operator "do you want to create a policy for
// this route?", it drops a seed Policy (no id, rules pre-populated)
// into sessionStorage and navigates here. The hydration below picks
// it up on mount and passes it through to PolicyEditorShell.
//
// The shell distinguishes seed-create from real edit by id, so the
// header still says "New access policy" and the save button still
// says "Add policy" even when fields are pre-filled.

const SEED_KEY = "oz2-policy-seed";

export default function NewAccessControlPolicyPage() {
  const { permission } = usePermissions();
  const [seed, setSeed] = useState<Policy | undefined>();
  const [hydrated, setHydrated] = useState(false);

  useEffect(() => {
    if (typeof window === "undefined") {
      setHydrated(true);
      return;
    }
    const raw = window.sessionStorage.getItem(SEED_KEY);
    if (raw) {
      window.sessionStorage.removeItem(SEED_KEY);
      try {
        setSeed(JSON.parse(raw));
      } catch {
        // ignore — seed is best-effort, fall back to a blank form
      }
    }
    setHydrated(true);
  }, []);

  if (!hydrated) return <FullScreenLoading height="auto" />;

  return (
    <RestrictedAccess hasAccess={permission.policies.create}>
      <GroupsProvider>
        <PoliciesProvider>
          <PolicyEditorShell policy={seed} />
        </PoliciesProvider>
      </GroupsProvider>
    </RestrictedAccess>
  );
}
