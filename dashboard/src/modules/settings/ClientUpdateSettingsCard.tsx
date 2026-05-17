"use client";

import { notify } from "@components/Notification";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import { PeerMultiSelector } from "@components/PeerMultiSelector";
import { useApiCall } from "@utils/api";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Account } from "@/interfaces/Account";
import useGroupHelper from "@/modules/groups/useGroupHelper";
import OzSettingsCard from "@/modules/settings/v2/OzSettingsCard";
import OzSettingsField from "@/modules/settings/v2/OzSettingsField";
import OzSettingsToggle from "@/modules/settings/v2/OzSettingsToggle";

// ClientUpdateSettingsCard — the operator-facing client self-update
// directive + Q2 subset targeting (openZro #5). The server resolves
// the subset per peer; this UI only edits the account-level intent.
// Save mirrors the existing /accounts/{id} PUT pattern.

type Props = { account: Account };

const ids = (gs: { id?: string }[]) =>
  gs.map((g) => g.id).filter((x): x is string => !!x);

export default function ClientUpdateSettingsCard({ account }: Readonly<Props>) {
  const s = account.settings;
  const { permission } = usePermissions();
  const { mutate } = useSWRConfig();
  const saveRequest = useApiCall<Account>("/accounts/" + account.id, true);
  const editDisabled = !permission.settings.update;

  const [version, setVersion] = useState(s.client_update_target_version ?? "");
  const [force, setForce] = useState(s.client_update_force ?? false);
  const [targetGroups, setTargetGroups] = useGroupHelper({
    initial: s.client_update_target_groups ?? [],
  });
  const [excludeGroups, setExcludeGroups] = useGroupHelper({
    initial: s.client_update_exclude_groups ?? [],
  });
  const [peers, setPeers] = useState<string[]>(
    s.client_update_target_peers ?? [],
  );
  const [percent, setPercent] = useState(
    s.client_update_rollout_percent === undefined
      ? ""
      : String(s.client_update_rollout_percent),
  );

  const percentErr = useMemo(() => {
    if (percent.trim() === "") return "";
    const n = Number(percent);
    if (!Number.isInteger(n) || n < 0 || n > 100)
      return "Must be an integer 0–100 (leave empty for no ring).";
    return "";
  }, [percent]);

  const scopeHint = useMemo(() => {
    const g = targetGroups.length;
    const p = peers.length;
    const ring =
      percent.trim() === "" ? "no ring" : `ring ${Number(percent)}%`;
    const base =
      g === 0 && p === 0 ? "whole fleet" : `${g} group(s), ${p} peer(s)`;
    const ex = excludeGroups.length
      ? `, minus ${excludeGroups.length} excluded group(s)`
      : "";
    return version.trim() === ""
      ? "No directive — clients do nothing."
      : `Scope: ${base}, ${ring}${ex} (server-resolved per peer).`;
  }, [version, targetGroups, peers, percent, excludeGroups]);

  const onSave = () => {
    if (percentErr) return;
    notify({
      title: "Client updates",
      description: "Client update directive saved.",
      loadingMessage: "Saving client update directive...",
      promise: saveRequest
        .put({
          id: account.id,
          settings: {
            ...s,
            client_update_target_version: version.trim(),
            client_update_force: force,
            client_update_target_groups: ids(targetGroups),
            client_update_target_peers: peers,
            client_update_exclude_groups: ids(excludeGroups),
            client_update_rollout_percent:
              percent.trim() === "" ? undefined : Number(percent),
          },
        })
        .then(() => mutate("/accounts")),
    });
  };

  return (
    <OzSettingsCard
      title="Client updates"
      sub="Direct desktop clients to a target version. Empty = no directive. Force installs silently; otherwise the update is offered. Scope the rollout server-side by group, explicit peers and a staged ring; exclude groups are never updated (even if listed as a peer)."
    >
      <OzSettingsField
        label="Target version"
        hint="e.g. 0.40.0. Empty clears the directive."
      >
        <OzInput
          value={version}
          mono
          placeholder="0.40.0"
          disabled={editDisabled}
          data-cy="client-update-version"
          onChange={(e) => setVersion(e.target.value)}
        />
      </OzSettingsField>

      <OzSettingsToggle
        value={force}
        onChange={setForce}
        disabled={editDisabled}
        label="Force silent install"
        desc="When on, targeted clients install in the background without prompting. When off, the update is offered for a manual install."
      />

      <OzSettingsField
        label="Target groups"
        hint="Member peers are in scope (subject to the ring). Empty + no target peers = whole fleet."
      >
        <PeerGroupSelector
          values={targetGroups}
          onChange={setTargetGroups}
          disabled={editDisabled}
          hideAllGroup
          saveGroupAssignments={false}
        />
      </OzSettingsField>

      <OzSettingsField
        label="Target peers"
        hint="Explicit peers always in scope — these pierce the ring (canary / break-glass)."
      >
        <PeerMultiSelector
          value={peers}
          onChange={setPeers}
          disabled={editDisabled}
        />
      </OzSettingsField>

      <OzSettingsField
        label="Exclude groups"
        hint="Member peers NEVER receive the directive, even if listed as a target peer (infra / gateway safety)."
      >
        <PeerGroupSelector
          values={excludeGroups}
          onChange={setExcludeGroups}
          disabled={editDisabled}
          hideAllGroup
          saveGroupAssignments={false}
        />
      </OzSettingsField>

      <OzSettingsField
        label="Rollout ring (%)"
        hint="0–100 staged ring. Empty = no ring (everyone in scope). 0 = nobody (paused)."
      >
        <OzInput
          type="number"
          min={0}
          max={100}
          value={percent}
          placeholder="(no ring)"
          disabled={editDisabled}
          error={percentErr}
          onChange={(e) => setPercent(e.target.value)}
        />
      </OzSettingsField>

      <div className="flex items-center justify-between gap-3 pt-1">
        <p className="text-[11.5px] leading-[1.45] text-oz2-text-faint">
          {scopeHint}
        </p>
        <OzButton
          variant="primary"
          disabled={editDisabled || !!percentErr}
          onClick={onSave}
          data-cy="client-update-save"
        >
          Save
        </OzButton>
      </div>
    </OzSettingsCard>
  );
}
