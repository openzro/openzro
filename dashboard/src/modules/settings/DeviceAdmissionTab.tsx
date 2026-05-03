import Breadcrumbs from "@components/Breadcrumbs";
import Button from "@components/Button";
import { Checkbox } from "@components/Checkbox";
import FancyToggleSwitch from "@components/FancyToggleSwitch";
import HelpText from "@components/HelpText";
import InlineLink from "@components/InlineLink";
import { Label } from "@components/Label";
import { notify } from "@components/Notification";
import Paragraph from "@components/Paragraph";
import { useHasChanges } from "@hooks/useHasChanges";
import * as Tabs from "@radix-ui/react-tabs";
import useFetchApi, { useApiCall, useOpenzroFetch } from "@utils/api";
import { API_ORIGIN } from "@utils/openzro";
import { DownloadIcon, ExternalLinkIcon, ShieldHalf } from "lucide-react";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import SettingsIcon from "@/assets/icons/SettingsIcon";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Account } from "@/interfaces/Account";
import { Group } from "@/interfaces/Group";
import { PostureCheck } from "@/interfaces/PostureCheck";

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
    const res = await authedFetch(
      `${API_ORIGIN}/api/events/admission.csv`,
      {
        method: "GET",
        headers: { Accept: "text/csv" },
      },
    );
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

  return (
    <Tabs.Content value={"device-admission"}>
      <div className={"p-default py-6 max-w-2xl"}>
        <Breadcrumbs>
          <Breadcrumbs.Item
            href={"/settings"}
            label={"Settings"}
            icon={<SettingsIcon size={13} />}
          />
          <Breadcrumbs.Item
            href={"/settings?tab=device-admission"}
            label={"Device Admission"}
            icon={<ShieldHalf size={14} />}
            active
          />
        </Breadcrumbs>
        <div className={"flex items-start justify-between"}>
          <div>
            <h1>Device Admission</h1>
            <Paragraph className={"max-w-xl"}>
              Block non-compliant devices from joining the mesh. When enabled,
              every peer Login and Sync is gated on the posture checks
              selected below — peers that fail any check are refused at the
              control plane and never reach the data plane. Required for
              regulated environments (e.g. Bacen Resolução 4.893 / Circular
              3.909 for fintechs and banks) that mandate provable endpoint
              admission control with an audit trail.
            </Paragraph>
          </div>
          <div className={"flex gap-2"}>
            <Button variant={"secondary"} onClick={downloadAuditCsv}>
              <DownloadIcon size={14} />
              Audit CSV
            </Button>
            <Button
              variant={"primary"}
              disabled={!hasChanges || !permission.settings.update}
              onClick={saveChanges}
            >
              Save Changes
            </Button>
          </div>
        </div>

        <div className={"flex flex-col gap-6 w-full mt-8"}>
          <FancyToggleSwitch
            value={enforcementEnabled}
            onChange={setEnforcementEnabled}
            label={
              <>
                <ShieldHalf size={15} />
                Enforce admission on Login &amp; Sync
              </>
            }
            helpText={
              <>
                When on, every peer must pass the posture checks below before
                joining the mesh. Failures are recorded under{" "}
                <InlineLink href={"/activity"}>Activity</InlineLink> with the
                failing check and reason.{" "}
                <InlineLink
                  href={
                    "https://docs.openzro.io/how-to/manage-posture-checks#device-admission"
                  }
                  target={"_blank"}
                  onClick={(e) => e.stopPropagation()}
                >
                  Learn more
                  <ExternalLinkIcon size={12} />
                </InlineLink>
              </>
            }
            disabled={!permission.settings.update}
          />

          <div>
            <Label>Posture checks that gate admission</Label>
            <HelpText>
              ALL selected checks must pass for a peer to be admitted. Order
              does not matter. Configure new posture checks under{" "}
              <InlineLink href={"/posture-checks"}>Posture Checks</InlineLink>.
            </HelpText>

            {noChecksConfigured ? (
              <Paragraph className={"text-xs text-neutral-600 dark:text-nb-gray-300 mt-2"}>
                No posture checks defined yet. Create one under{" "}
                <InlineLink href={"/posture-checks"}>Posture Checks</InlineLink>{" "}
                first — for example, an Endpoint Security check pointed at your
                MDM/EDR provider.
              </Paragraph>
            ) : (
              <div
                className={
                  "mt-3 flex flex-col gap-2 border border-neutral-200 dark:border-nb-gray-900 rounded-md p-4"
                }
              >
                {(postureChecks ?? []).map((pc) => (
                  <label
                    key={pc.id}
                    className={
                      "flex items-start gap-3 cursor-pointer select-none py-1"
                    }
                  >
                    <Checkbox
                      checked={selectedIds.includes(pc.id)}
                      onCheckedChange={() => toggleCheck(pc.id)}
                      disabled={!permission.settings.update}
                    />
                    <div className={"flex flex-col"}>
                      <span className={"text-sm font-medium"}>{pc.name}</span>
                      {pc.description && (
                        <span className={"text-xs text-neutral-600 dark:text-nb-gray-300"}>
                          {pc.description}
                        </span>
                      )}
                    </div>
                  </label>
                ))}
              </div>
            )}
          </div>

          <div>
            <Label>Exempt groups</Label>
            <HelpText>
              Peers in any of these groups skip the admission gate
              entirely. Use this for routing / gateway peers (cloud
              VMs, K8s pods, on-prem servers) that aren&apos;t enrolled
              in MDM/EDR — without an exempt group, the gate would
              lock your own infrastructure out.
            </HelpText>

            {(groups ?? []).length === 0 ? (
              <Paragraph className={"text-xs text-neutral-600 dark:text-nb-gray-300 mt-2"}>
                No groups defined yet. Create a group (e.g.{" "}
                <code className={"font-mono text-xs"}>
                  infrastructure-peers
                </code>
                ) under{" "}
                <InlineLink href={"/team/groups"}>Groups</InlineLink>{" "}
                first, then attach it to the setup keys you use to
                enrol gateway peers.
              </Paragraph>
            ) : (
              <div
                className={
                  "mt-3 flex flex-col gap-2 border border-neutral-200 dark:border-nb-gray-900 rounded-md p-4"
                }
              >
                {(groups ?? [])
                  .filter((g) => g.id)
                  .map((g) => (
                    <label
                      key={g.id}
                      className={
                        "flex items-start gap-3 cursor-pointer select-none py-1"
                      }
                    >
                      <Checkbox
                        checked={exemptGroupIds.includes(g.id as string)}
                        onCheckedChange={() =>
                          toggleExemptGroup(g.id as string)
                        }
                        disabled={!permission.settings.update}
                      />
                      <div className={"flex flex-col"}>
                        <span className={"text-sm font-medium"}>{g.name}</span>
                        <span className={"text-xs text-neutral-600 dark:text-nb-gray-300"}>
                          {g.peers_count ?? 0} peer
                          {(g.peers_count ?? 0) === 1 ? "" : "s"}
                        </span>
                      </div>
                    </label>
                  ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </Tabs.Content>
  );
}
