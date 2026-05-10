"use client";

import HelpText from "@components/HelpText";
import { Label } from "@components/Label";
import { notify } from "@components/Notification";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import { useApiCall } from "@utils/api";
import React from "react";
import Skeleton from "react-loading-skeleton";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { useHasChanges } from "@/hooks/useHasChanges";
import { Group } from "@/interfaces/Group";
import { NameserverSettings } from "@/interfaces/NameserverSettings";
import DnsTabs from "@/modules/dns/v2/DnsTabs";
import useGroupHelper from "@/modules/groups/useGroupHelper";

// DnsSettingsV2 — phase-5.15 v2 paint over /api/dns/settings.
// Single-form page: a PeerGroupSelector for the disabled-management
// groups list plus a Save button. Behavior preserved verbatim from
// the legacy NameServerSettings (same SWR endpoint, same useGroupHelper
// + useHasChanges, same notify on save, same RestrictedAccess + perm
// gates). Visual chrome aligns with the rest of the v2 dashboard:
// page header + DnsTabs sub-nav + a single OzCard form.

interface Props {
  settings: NameserverSettings | undefined;
  initialGroups: Group[] | undefined;
  isLoading: boolean;
}

export default function DnsSettingsV2({
  initialGroups,
  isLoading,
}: Props) {
  return (
    <div className="space-y-6 p-8">
      <header>
        <h1 className="text-[24px] font-semibold tracking-tight">DNS</h1>
        <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
          Manage your account&apos;s DNS settings. Learn more about{" "}
          <a
            href="https://docs.openzro.io/how-to/manage-dns-in-your-network"
            target="_blank"
            rel="noopener noreferrer"
            className="text-oz2-acc-text underline-offset-2 hover:underline"
          >
            DNS
          </a>
          .
        </p>
      </header>

      <DnsTabs />

      {isLoading || initialGroups === undefined ? (
        <div className="max-w-xl">
          <Skeleton width="100%" height={240} />
        </div>
      ) : (
        <DisabledManagementGroupsCard initialGroups={initialGroups} />
      )}
    </div>
  );
}

function DisabledManagementGroupsCard({
  initialGroups,
}: {
  initialGroups: Group[];
}) {
  const settingRequest = useApiCall<NameserverSettings>("/dns/settings");
  const { mutate } = useSWRConfig();
  const { permission } = usePermissions();

  const [selectedGroups, setSelectedGroups, { save: saveGroups }] =
    useGroupHelper({ initial: initialGroups });

  const { hasChanges, updateRef: updateChangesRef } = useHasChanges([
    selectedGroups,
  ]);

  const saveSettings = async () => {
    const savedGroups = await saveGroups();
    notify({
      title: "DNS Settings",
      description: "Settings saved successfully.",
      promise: settingRequest
        .put({
          disabled_management_groups: savedGroups.map((g) => g.id),
        })
        .then(() => {
          mutate("/dns/settings");
          updateChangesRef([selectedGroups]);
        }),
      loadingMessage: "Saving the settings...",
    });
  };

  return (
    <OzCard flush className="max-w-xl overflow-hidden">
      <div className="px-6 py-6">
        <Label>Disable DNS management for these groups</Label>
        <HelpText>
          Peers in these groups will require manual domain name resolution
        </HelpText>
        <PeerGroupSelector
          dataCy="dns-groups-selector"
          onChange={setSelectedGroups}
          values={selectedGroups}
          disabled={!permission.dns.update}
        />
      </div>
      <div className="flex items-center justify-end gap-3 border-t border-oz2-border-soft bg-oz2-bg-sunken px-6 py-4">
        <OzButton
          variant="primary"
          type="button"
          onClick={saveSettings}
          disabled={!hasChanges || !permission.dns.update}
          data-cy="save-changes"
        >
          Save Changes
        </OzButton>
      </div>
    </OzCard>
  );
}
