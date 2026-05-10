"use client";

import { notify } from "@components/Notification";
import * as Tabs from "@radix-ui/react-tabs";
import { useApiCall } from "@utils/api";
import React, { useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { useHasChanges } from "@/hooks/useHasChanges";
import { Account } from "@/interfaces/Account";
import OzSettingsCard from "@/modules/settings/v2/OzSettingsCard";
import OzSettingsToggle from "@/modules/settings/v2/OzSettingsToggle";

// PermissionsTab — settings sub-page body for /settings/permissions.
// Functionality preserved verbatim from the pre-phase-5 legacy:
// regular_users_view_blocked boolean toggle saved through
// /accounts/{id}. Only paint changes — the single FancyToggleSwitch
// becomes an OzSettingsToggle inside one OzSettingsCard.

type Props = {
  account: Account;
};

export default function PermissionsTab({ account }: Readonly<Props>) {
  const { permission } = usePermissions();
  const { mutate } = useSWRConfig();
  const saveRequest = useApiCall<Account>("/accounts/" + account.id);

  const [userViewBlocked, setUserViewBlocked] = useState<boolean>(
    account?.settings.regular_users_view_blocked ?? false,
  );

  const { hasChanges, updateRef } = useHasChanges([userViewBlocked]);

  const saveChanges = async () => {
    notify({
      title: "Permission Settings",
      description: "Permissions were updated successfully.",
      promise: saveRequest
        .put({
          id: account.id,
          settings: {
            ...account.settings,
            regular_users_view_blocked: userViewBlocked,
          },
        })
        .then(() => {
          mutate("/accounts");
          updateRef([userViewBlocked]);
        }),
      loadingMessage: "Updating permissions...",
    });
  };

  const editDisabled = !permission.settings.update;

  return (
    <Tabs.Content value="permissions" className="flex flex-col gap-5">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="text-[18px] font-semibold tracking-tight text-oz2-text">
            Permissions
          </h2>
          <p className="mt-1 max-w-2xl text-[13px] leading-[1.55] text-oz2-text-muted">
            Coarse-grained dashboard access controls. Fine-grained role and
            group permissions live on the Users &amp; Groups screen.
          </p>
        </div>
        <OzButton
          variant="primary"
          type="button"
          disabled={!hasChanges || editDisabled}
          onClick={saveChanges}
        >
          Save Changes
        </OzButton>
      </header>

      <OzSettingsCard
        title="Dashboard visibility"
        sub="Limit what regular (non-admin) users can see when they sign in to the dashboard."
      >
        <OzSettingsToggle
          value={userViewBlocked}
          onChange={setUserViewBlocked}
          disabled={editDisabled}
          label="Restrict dashboard for regular users"
          desc="Regular users won't be able to view any peers or workspace data — only their own session and account settings."
        />
      </OzSettingsCard>
    </Tabs.Content>
  );
}
