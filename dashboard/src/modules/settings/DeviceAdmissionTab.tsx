"use client";

import InlineLink from "@components/InlineLink";
import { notify } from "@components/Notification";
import { useHasChanges } from "@hooks/useHasChanges";
import * as Tabs from "@radix-ui/react-tabs";
import useFetchApi, { useApiCall, useOpenzroFetch } from "@utils/api";
import { API_ORIGIN } from "@utils/openzro";
import { Download, ExternalLinkIcon } from "lucide-react";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzCheckbox from "@/components/v2/OzCheckbox";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Account } from "@/interfaces/Account";
import { Group } from "@/interfaces/Group";
import { PostureCheck } from "@/interfaces/PostureCheck";
import OzSettingsCard from "@/modules/settings/v2/OzSettingsCard";
import OzSettingsToggle from "@/modules/settings/v2/OzSettingsToggle";

// DeviceAdmissionTab — settings sub-page body for
// /settings/device-admission. Functionality preserved verbatim:
// admission_enforcement_enabled, admission_posture_checks (list of
// IDs), admission_exempt_groups (list of IDs) saved through
// /accounts/{id}. The CSV export hits /api/events/admission.csv
// through the authed fetch. Only paint changes — enforcement toggle
// + posture-checks list + exempt-groups list split into three
// OzSettingsCards; checkbox rows render in a tight selectable list
// with hover affordance.

type Props = {
  account: Account;
};

export default function DeviceAdmissionTab({ account }: Readonly<Props>) {
  const { permission } = usePermissions();
  const { mutate } = useSWRConfig();
  const saveRequest = useApiCall<Account>("/accounts/" + account.id, true);
  const { fetch: authedFetch } = useOpenzroFetch();

  const { data: postureChecks, isLoading: postureChecksLoading } =
    useFetchApi<PostureCheck[]>("/posture-checks");

  const { data: groups } = useFetchApi<Group[]>("/groups");

  const [enforcementEnabled, setEnforcementEnabled] = useState<boolean>(
    !!account.settings.admission_enforcement_enabled,
  );
  const [selectedIds, setSelectedIds] = useState<string[]>(
    account.settings.admission_posture_checks ?? [],
  );
  const [exemptGroupIds, setExemptGroupIds] = useState<string[]>(
    account.settings.admission_exempt_groups ?? [],
  );

  const { hasChanges, updateRef } = useHasChanges([
    enforcementEnabled,
    selectedIds.slice().sort().join(","),
    exemptGroupIds.slice().sort().join(","),
  ]);

  const toggleCheck = (id: string) => {
    setSelectedIds((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id],
    );
  };

  const toggleExemptGroup = (id: string) => {
    setExemptGroupIds((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id],
    );
  };

  const saveChanges = async () => {
    notify({
      title: "Device Admission",
      description: "Admission policy updated.",
      promise: saveRequest
        .put({
          id: account.id,
          settings: {
            ...account.settings,
            admission_enforcement_enabled: enforcementEnabled,
            admission_posture_checks: selectedIds,
            admission_exempt_groups: exemptGroupIds,
          },
        })
        .then(() => {
          mutate("/accounts");
          updateRef([
            enforcementEnabled,
            selectedIds.slice().sort().join(","),
            exemptGroupIds.slice().sort().join(","),
          ]);
        }),
      loadingMessage: "Saving admission policy...",
    });
  };

  const noChecksConfigured = useMemo(
    () => !postureChecksLoading && (postureChecks?.length ?? 0) === 0,
    [postureChecks, postureChecksLoading],
  );

  const downloadAuditCsv = async () => {
    const res = await authedFetch(`${API_ORIGIN}/api/events/admission.csv`, {
      method: "GET",
      headers: { Accept: "text/csv" },
    });
    if (!res.ok) {
      notify({
        title: "Audit export",
        description: `Failed: ${res.status} ${res.statusText}`,
        promise: Promise.reject(new Error("admission audit export failed")),
        loadingMessage: "Downloading admission audit...",
      });
      return;
    }
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `openzro-admission-audit-${new Date()
      .toISOString()
      .slice(0, 10)}.csv`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  };

  const editDisabled = !permission.settings.update;

  return (
    <Tabs.Content value="device-admission" className="flex flex-col gap-5">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="text-[18px] font-semibold tracking-tight text-oz2-text">
            Device Admission
          </h2>
          <p className="mt-1 max-w-2xl text-[13px] leading-[1.55] text-oz2-text-muted">
            Block non-compliant devices from joining the mesh. Required for
            regulated environments (e.g. Bacen Resolução 4.893 / Circular 3.909
            for fintechs and banks) that mandate provable endpoint admission
            control with an audit trail.
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <OzButton variant="default" type="button" onClick={downloadAuditCsv}>
            <Download size={13} />
            Audit CSV
          </OzButton>
          <OzButton
            variant="primary"
            type="button"
            disabled={!hasChanges || editDisabled}
            onClick={saveChanges}
          >
            Save Changes
          </OzButton>
        </div>
      </header>

      <OzSettingsCard
        title="Enforcement"
        sub="When on, every peer Login and Sync is gated on the posture checks below. Peers that fail any check are refused at the control plane and never reach the data plane."
      >
        <OzSettingsToggle
          value={enforcementEnabled}
          onChange={setEnforcementEnabled}
          disabled={editDisabled}
          label="Enforce admission on Login & Sync"
          desc={
            <>
              Failures are recorded under{" "}
              <InlineLink href="/events/audit">Activity</InlineLink> with the
              failing check and reason.{" "}
              <InlineLink
                href="https://docs.openzro.io/how-to/manage-posture-checks#device-admission"
                target="_blank"
              >
                Learn more
                <ExternalLinkIcon size={11} />
              </InlineLink>
            </>
          }
        />
      </OzSettingsCard>

      {/* The two list cards (Posture checks + Exempt groups) share the
          same checkbox-list shape and sit at the same level of the
          admission policy — selecting which checks gate the gate, and
          which groups bypass it. Pairing them side-by-side on md+
          viewports keeps the page from sprawling vertically; on narrow
          screens they stack. Enforcement above stays full-width as the
          master switch that activates the whole gate. */}
      <div className="grid grid-cols-1 items-start gap-5 md:grid-cols-2">
        <OzSettingsCard
          title="Posture checks that gate admission"
          sub={
            <>
              ALL selected checks must pass for a peer to be admitted. Order
              does not matter. Configure new checks under{" "}
              <InlineLink href="/posture-checks">Posture Checks</InlineLink>.
            </>
          }
        >
          {noChecksConfigured ? (
            <div className="rounded-oz2-card border border-dashed border-oz2-border bg-oz2-bg-sunken/40 px-4 py-5 text-center text-[12.5px] text-oz2-text-muted">
              No posture checks defined yet. Create one under{" "}
              <InlineLink href="/posture-checks">Posture Checks</InlineLink>{" "}
              first — for example, an Endpoint Security check pointed at your
              MDM/EDR provider.
            </div>
          ) : (
            <ul className="flex flex-col gap-1">
              {(postureChecks ?? []).map((pc) => (
                <li key={pc.id}>
                  <label className="flex cursor-pointer select-none items-start gap-3 rounded-[8px] px-2 py-2 transition-colors hover:bg-oz2-hover">
                    <OzCheckbox
                      checked={selectedIds.includes(pc.id)}
                      onCheckedChange={() => toggleCheck(pc.id)}
                      disabled={editDisabled}
                      className="mt-[2px]"
                    />
                    <div className="flex min-w-0 flex-col">
                      <span className="text-[13px] font-medium text-oz2-text">
                        {pc.name}
                      </span>
                      {pc.description && (
                        <span className="mt-[2px] text-[12px] text-oz2-text-muted">
                          {pc.description}
                        </span>
                      )}
                    </div>
                  </label>
                </li>
              ))}
            </ul>
          )}
        </OzSettingsCard>

        <OzSettingsCard
          title="Exempt groups"
          sub="Peers in any of these groups skip the admission gate entirely. Use this for routing/gateway peers (cloud VMs, K8s pods, on-prem servers) that aren't enrolled in MDM/EDR — without an exempt group, the gate would lock your own infrastructure out."
        >
          {(groups ?? []).length === 0 ? (
            <div className="rounded-oz2-card border border-dashed border-oz2-border bg-oz2-bg-sunken/40 px-4 py-5 text-center text-[12.5px] text-oz2-text-muted">
              No groups defined yet. Create a group (e.g.{" "}
              <code className="font-mono text-[11.5px]">infrastructure-peers</code>
              ) under <InlineLink href="/team/groups">Groups</InlineLink> first,
              then attach it to the setup keys you use to enrol gateway peers.
            </div>
          ) : (
            <ul className="flex flex-col gap-1">
              {(groups ?? [])
                .filter((g) => g.id)
                .map((g) => (
                  <li key={g.id}>
                    <label className="flex cursor-pointer select-none items-start gap-3 rounded-[8px] px-2 py-2 transition-colors hover:bg-oz2-hover">
                      <OzCheckbox
                        checked={exemptGroupIds.includes(g.id as string)}
                        onCheckedChange={() =>
                          toggleExemptGroup(g.id as string)
                        }
                        disabled={editDisabled}
                        className="mt-[2px]"
                      />
                      <div className="flex min-w-0 flex-col">
                        <span className="text-[13px] font-medium text-oz2-text">
                          {g.name}
                        </span>
                        <span className="mt-[2px] text-[12px] text-oz2-text-muted">
                          {g.peers_count ?? 0} peer
                          {(g.peers_count ?? 0) === 1 ? "" : "s"}
                        </span>
                      </div>
                    </label>
                  </li>
                ))}
            </ul>
          )}
        </OzSettingsCard>
      </div>
    </Tabs.Content>
  );
}
