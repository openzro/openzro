"use client";

import InlineLink from "@components/InlineLink";
import { notify } from "@components/Notification";
import * as Tabs from "@radix-ui/react-tabs";
import { useApiCall } from "@utils/api";
import { isLocalDev, isOpenzroHosted } from "@utils/openzro";
import { isEmpty } from "lodash";
import { AlertCircle, Braces, ShieldCheck } from "lucide-react";
import React, { useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import { useDialog } from "@/contexts/DialogProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { useHasChanges } from "@/hooks/useHasChanges";
import { Account } from "@/interfaces/Account";
import OzSettingsCard from "@/modules/settings/v2/OzSettingsCard";
import OzSettingsField from "@/modules/settings/v2/OzSettingsField";
import OzSettingsToggle from "@/modules/settings/v2/OzSettingsToggle";

// GroupsTab — settings sub-page body for /settings/groups.
// Functionality preserved verbatim: groups_propagation_enabled,
// jwt_groups_enabled + jwt_groups_claim_name + jwt_allow_groups
// saved through /accounts/{id} with a confirm step when an allow
// group is set (to prevent the operator from locking themselves
// out). Only paint changes — propagation + JWT sync toggles split
// into two OzSettingsCards; JWT claim + allow-group inputs render
// inside a sunken sub-card when JWT sync is on (mirrors the
// AuthenticationTab session-expiration expansion).

type Props = {
  account: Account;
};

export default function GroupsTab({ account }: Readonly<Props>) {
  const { permission } = usePermissions();
  const { mutate } = useSWRConfig();
  const { confirm } = useDialog();

  const [groupsPropagation, setGroupsPropagation] = useState<boolean>(
    account.settings.groups_propagation_enabled,
  );

  const [jwtGroupSync, setJwtGroupSync] = useState<boolean>(
    account.settings.jwt_groups_enabled,
  );
  const [jwtGroupsClaimName, setJwtGroupsClaimName] = useState(
    account.settings.jwt_groups_claim_name,
  );
  const [jwtAllowGroups, setJwtAllowGroups] = useState<string[]>(
    account.settings.jwt_allow_groups,
  );
  const [jwtAllowGroupsWarning, setJwtAllowGroupsWarning] = useState(false);

  const { hasChanges, updateRef } = useHasChanges([
    groupsPropagation,
    jwtAllowGroups,
    jwtGroupsClaimName,
    jwtGroupSync,
  ]);

  const saveRequest = useApiCall<Account>("/accounts/" + account.id);

  const saveChanges = async () => {
    const jwtGroupsEntered =
      jwtAllowGroups.filter((g) => !isEmpty(g)).length > 0;
    const showConfirm = jwtGroupSync && jwtGroupsEntered;
    const choice = showConfirm
      ? await confirm({
          title: `JWT allow group - ${jwtAllowGroups[0]}`,
          description: `Only users part of the ${jwtAllowGroups[0]} group will be able to access openZro. Are you sure you want to save the changes?`,
          confirmText: "Save",
          children: (
            <div className="flex items-center gap-2 rounded-md border border-oz2-acc bg-oz2-acc-soft px-4 py-3 text-[12px] text-oz2-acc-text">
              <AlertCircle size={14} />
              To prevent losing access, ensure you are part of this group.
            </div>
          ),
          cancelText: "Cancel",
          type: "default",
        })
      : true;

    if (!choice) return;

    notify({
      title: "Group Settings",
      description: "Group settings were updated successfully.",
      promise: saveRequest
        .put({
          id: account.id,
          settings: {
            ...account.settings,
            groups_propagation_enabled: groupsPropagation,
            jwt_groups_enabled: jwtGroupSync,
            jwt_groups_claim_name: isEmpty(jwtGroupsClaimName)
              ? undefined
              : jwtGroupsClaimName,
            jwt_allow_groups: jwtGroupsEntered ? jwtAllowGroups : undefined,
          },
        })
        .then(() => {
          mutate("/accounts");
          updateRef([
            groupsPropagation,
            jwtAllowGroups,
            jwtGroupsClaimName,
            jwtGroupSync,
          ]);
        }),
      loadingMessage: "Updating group settings...",
    });
  };

  const editDisabled = !permission.settings.update;
  const showJwtSync = !isOpenzroHosted() || isLocalDev();

  return (
    <Tabs.Content value="groups" className="flex flex-col gap-5">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="text-[18px] font-semibold tracking-tight text-oz2-text">
            User Groups
          </h2>
          <p className="mt-1 max-w-2xl text-[13px] leading-[1.55] text-oz2-text-muted">
            How group membership flows between users, peers, and your IdP.
            Manage the groups themselves on the dedicated{" "}
            <InlineLink href="/team/groups">Team → Groups</InlineLink> page.
          </p>
        </div>
        <OzButton
          variant="primary"
          type="button"
          disabled={!hasChanges}
          onClick={saveChanges}
        >
          Save Changes
        </OzButton>
      </header>

      <OzSettingsCard
        title="Group propagation"
        sub="Share user group membership down to the peers they own, so policies can reference user-level groups consistently across the mesh."
      >
        <OzSettingsToggle
          value={groupsPropagation}
          onChange={setGroupsPropagation}
          disabled={editDisabled}
          label="Enable user group propagation"
          desc="Auto-groups assigned to a user are also assigned to every peer that user owns."
        />
      </OzSettingsCard>

      {showJwtSync && (
        <OzSettingsCard
          title="JWT group sync"
          sub="Read group membership directly from your IdP's JWT claims. Groups in the token are auto-created and the user is added to them on every sign-in."
        >
          <OzSettingsToggle
            value={jwtGroupSync}
            onChange={setJwtGroupSync}
            disabled={editDisabled}
            label="Enable JWT group sync"
            desc="Extract & sync groups from JWT claims with the user's auto-groups, auto-creating groups from tokens."
          />

          {jwtGroupSync && (
            <div className="flex flex-col gap-5 rounded-oz2-card border border-oz2-border-soft bg-oz2-bg-sunken p-4">
              <OzSettingsField
                label="JWT claim"
                hint="Specify the JWT claim used for extracting group names (e.g. roles, groups). The claim should contain a list of group names."
              >
                <OzInput
                  prefix={<Braces size={14} />}
                  placeholder="e.g., roles"
                  value={jwtGroupsClaimName ?? ""}
                  onKeyDown={(event) => {
                    if (event.code === "Space") event.preventDefault();
                  }}
                  onChange={(e) => {
                    setJwtGroupsClaimName(e.target.value.replace(/ /g, ""));
                  }}
                  disabled={editDisabled}
                />
              </OzSettingsField>

              <OzSettingsField
                label="JWT allow group"
                hint="Limit access to openZro for the specified group name (e.g. openZro users). The group must already exist in your IdP."
              >
                <OzInput
                  prefix={<ShieldCheck size={14} />}
                  placeholder="e.g., openZro users"
                  value={jwtAllowGroups[0] ?? ""}
                  onChange={(e) => {
                    setJwtAllowGroups([e.target.value]);
                    setJwtAllowGroupsWarning(e.target.value !== "");
                  }}
                  disabled={editDisabled}
                />
              </OzSettingsField>

              {jwtAllowGroupsWarning && (
                <div className="flex items-start gap-2 rounded-oz2-card border border-oz2-warn/40 bg-oz2-warn-bg/40 px-3 py-2.5 text-[12px] text-oz2-warn">
                  <AlertCircle size={13} className="mt-0.5 shrink-0" />
                  <span className="leading-[1.5]">
                    To prevent losing access, ensure you are part of this group.
                  </span>
                </div>
              )}
            </div>
          )}
        </OzSettingsCard>
      )}
    </Tabs.Content>
  );
}
