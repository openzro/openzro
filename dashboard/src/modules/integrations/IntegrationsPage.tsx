"use client";

import Breadcrumbs from "@components/Breadcrumbs";
import Button from "@components/Button";
import Code from "@components/Code";
import HelpText from "@components/HelpText";
import InlineLink from "@components/InlineLink";
import { notify } from "@components/Notification";
import Paragraph from "@components/Paragraph";
import { cn } from "@utils/helpers";
import useFetchApi, { useApiCall } from "@utils/api";
import { API_ORIGIN } from "@utils/openzro";
import {
  CableIcon,
  CloudIcon,
  ExternalLinkIcon,
  GlobeIcon,
  KeyRoundIcon,
  PlusCircleIcon,
  RadioTowerIcon,
  ShieldCheckIcon,
  Trash2Icon,
  UsersIcon,
} from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import React, { useEffect, useState } from "react";
import { useSWRConfig } from "swr";
import {
  ActivityExporter,
  ActivityExporterType,
} from "@/interfaces/ActivityExporter";
import { FlowExport, FlowExportType } from "@/interfaces/FlowExport";
import { MDMProvider, MDMProviderType } from "@/interfaces/MDMProvider";
import ActivityExporterModal from "@/modules/activity-exporters/ActivityExporterModal";
import FlowExportModal from "@/modules/flow-exports/FlowExportModal";
import MDMProviderModal from "@/modules/mdm-providers/MDMProviderModal";

// IntegrationsPage is the top-level dashboard surface for
// runtime-configurable external integrations. Four sub-sections,
// each on its own deep-linkable sub-route:
//
//   /integrations?subtab=flow      Flow Exports
//   /integrations?subtab=mdm       MDM / EDR
//   /integrations?subtab=activity  Activity Streamer
//   /integrations?subtab=idp-sync  Identity Provider Sync (SCIM)
//
// Sub-tab is rendered as a vertical left rail inside the content
// area. Each click pushes the URL so a refresh / share-link /
// browser back keeps the user on the right section.
//
// The four sections share the same encrypted-at-rest credential
// envelope on the backend; they're grouped here because they're
// all "outbound connections to corporate systems" from the
// operator's perspective.

type SubTab = {
  value: string;
  label: string;
  icon: React.ReactNode;
};

const SUB_TABS: SubTab[] = [
  { value: "flow", label: "Flow Exports", icon: <CableIcon size={14} /> },
  { value: "activity", label: "Activity Streamer", icon: <RadioTowerIcon size={14} /> },
  { value: "mdm", label: "MDM / EDR", icon: <ShieldCheckIcon size={14} /> },
  { value: "idp-sync", label: "Identity Provider Sync", icon: <UsersIcon size={14} /> },
];

const DEFAULT_SUB_TAB = "flow";

function isValidSubTab(value: string | null): boolean {
  return SUB_TABS.some((t) => t.value === value);
}

export default function IntegrationsPage() {
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();

  // Read ?subtab= once on mount, then track locally. We do not
  // re-derive from params on every render because the click handler
  // already updates both the URL and local state — keeping them in
  // a useEffect would race with browser back/forward and cause a
  // flicker. The effect below handles back/forward by syncing only
  // when the URL value diverges.
  const initial =
    isValidSubTab(params.get("subtab"))
      ? (params.get("subtab") as string)
      : DEFAULT_SUB_TAB;
  const [active, setActive] = useState<string>(initial);

  useEffect(() => {
    const fromURL = params.get("subtab");
    if (isValidSubTab(fromURL) && fromURL !== active) {
      setActive(fromURL as string);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params]);

  const select = (value: string) => {
    setActive(value);
    router.push(`${pathname}?subtab=${value}`, {
      scroll: false,
    });
  };

  const activeMeta = SUB_TABS.find((t) => t.value === active) ?? SUB_TABS[0];

  return (
    <>
      <div className={"p-default py-6"}>
        <Breadcrumbs>
          <Breadcrumbs.Item
            href={"/integrations"}
            label={"Integrations"}
            icon={<CableIcon size={14} />}
          />
          <Breadcrumbs.Item
            href={`/integrations?subtab=${activeMeta.value}`}
            label={activeMeta.label}
            icon={activeMeta.icon}
            active
          />
        </Breadcrumbs>
        <h1>Integrations</h1>
        <Paragraph>
          External destinations and providers — SIEM streaming + cold
          archive for traffic events, MDM/EDR vendors for posture
          compliance, and SCIM 2.0 for user provisioning from your IdP.
        </Paragraph>

        <div className={"mt-6 flex flex-col gap-6 lg:flex-row"}>
          {/*
            Vertical sub-nav. Each entry deep-links via
            ?subtab=<value> so refresh / share-link keeps the user
            on the right section. The list is shrink-0 so the
            content column flexes to fill.
          */}
          <nav
            className={cn(
              "shrink-0 lg:w-56 lg:border-r border-neutral-200 dark:border-nb-gray-930",
              "lg:pr-4 lg:py-2",
            )}
          >
            <ul className={"flex lg:flex-col gap-1 m-0 p-0 list-none overflow-x-auto"}>
              {SUB_TABS.map((t) => (
                <li key={t.value} className={"shrink-0"}>
                  <button
                    type={"button"}
                    onClick={() => select(t.value)}
                    className={cn(
                      "w-full flex items-center gap-2 px-4 py-2 text-sm rounded-md transition-all whitespace-nowrap",
                      "lg:text-left",
                      active === t.value
                        // Mirror the VerticalTabs active state from
                        // the Settings sidebar so the two sidebars
                        // share visual language across the dashboard.
                        ? "bg-neutral-100 text-neutral-900 dark:bg-nb-gray-920 dark:text-white"
                        : "text-neutral-600 hover:bg-neutral-100 dark:text-nb-gray-300 dark:hover:bg-nb-gray-900/50",
                    )}
                  >
                    {t.icon}
                    {t.label}
                  </button>
                </li>
              ))}
            </ul>
          </nav>

          <div className={"flex-1 min-w-0 lg:pl-4"}>
            {active === "flow" && <FlowExportsSection />}
            {active === "mdm" && <MDMProvidersSection />}
            {active === "activity" && <ActivityExportersSection />}
            {active === "idp-sync" && <SCIMSetupSection />}
          </div>
        </div>
      </div>
    </>
  );
}

// ----- Flow Exports section ------------------------------------------

export function FlowExportsSection() {
  const { data, isLoading } =
    useFetchApi<FlowExport[]>("/admin/flow-exports");

  const [editing, setEditing] = useState<FlowExport | null>(null);
  const [modalOpen, setModalOpen] = useState(false);

  const openCreate = () => {
    setEditing(null);
    setModalOpen(true);
  };
  const openEdit = (row: FlowExport) => {
    setEditing(row);
    setModalOpen(true);
  };

  return (
    <div>
      <Paragraph>
        Stream traffic events to your SIEM (Elastic) or cold-archive
        them to S3-compatible storage (AWS S3, Cloudflare R2,
        Backblaze B2, Google Cloud Storage via Interoperability mode,
        MinIO). Each destination runs independently — a slow SIEM
        never blocks your archive, and vice-versa.
      </Paragraph>
      <HelpText>
        Credentials are encrypted at rest with the management&apos;s
        DataStoreEncryptionKey. The dashboard never reads them back —
        edit a destination and re-enter the value to rotate it.
      </HelpText>

      <div className="mt-6 flex justify-end">
        <Button variant="primary" onClick={openCreate}>
          <PlusCircleIcon size={16} /> Add destination
        </Button>
      </div>

      <div className="mt-4">
        {isLoading && (
          <Paragraph className="text-neutral-600 dark:text-nb-gray-300">
            Loading…
          </Paragraph>
        )}
        {!isLoading && (!data || data.length === 0) && (
          <EmptyState message="No destinations configured. Click Add destination to start streaming traffic events." />
        )}
        {data && data.length > 0 && (
          <table className="w-full text-sm">
            <TableHead cols={["Type", "Name", "Endpoint", "Status", ""]} />
            <tbody>
              {data.map((row) => (
                <FlowExportRow
                  key={row.id}
                  row={row}
                  onEdit={() => openEdit(row)}
                />
              ))}
            </tbody>
          </table>
        )}
      </div>

      <FlowExportModal
        open={modalOpen}
        setOpen={setModalOpen}
        existing={editing}
      />
    </div>
  );
}

function FlowExportRow({
  row,
  onEdit,
}: {
  row: FlowExport;
  onEdit: () => void;
}) {
  const { mutate } = useSWRConfig();
  const api = useApiCall(`/admin/flow-exports/${row.id}`);

  const onDelete = async () => {
    if (!confirm(`Delete "${row.name}"? This cannot be undone.`)) return;
    try {
      await api.del();
      await mutate("/admin/flow-exports");
      notify({ title: "Deleted", description: row.name });
    } catch {
      // useApiCall surfaces toast
    }
  };

  return (
    <tr className="border-t border-nb-gray-900">
      <td className="py-3">
        <FlowExportTypeBadge type={row.type} />
      </td>
      <td className="py-3">{row.name}</td>
      <td className="py-3 font-mono text-xs text-nb-gray-300">
        {flowEndpointLabel(row)}
      </td>
      <td className="py-3">
        <EnabledStatus enabled={row.enabled} />
      </td>
      <td className="py-3 text-right">
        <RowActions onEdit={onEdit} onDelete={onDelete} />
      </td>
    </tr>
  );
}

function FlowExportTypeBadge({ type }: { type: FlowExportType }) {
  const map: Record<FlowExportType, { icon: React.ReactNode; label: string }> = {
    elastic: { icon: <CableIcon size={12} />, label: "Elastic" },
    s3: { icon: <CloudIcon size={12} />, label: "S3" },
    http: { icon: <GlobeIcon size={12} />, label: "HTTP" },
    datadog: { icon: <CableIcon size={12} />, label: "Datadog" },
    gcs: { icon: <CloudIcon size={12} />, label: "GCS" },
  };
  const m = map[type];
  return (
    <span className="inline-flex items-center gap-1 rounded bg-nb-gray-900 px-2 py-1 text-xs text-violet-300">
      {m.icon} {m.label}
    </span>
  );
}

function flowEndpointLabel(row: FlowExport): string {
  if (row.type === "elastic") {
    return (row.config as { url?: string })?.url ?? "";
  }
  if (row.type === "s3") {
    const c = row.config as { bucket?: string; endpoint?: string };
    return c?.endpoint ? `${c.endpoint}/${c.bucket}` : c?.bucket ?? "";
  }
  if (row.type === "http") {
    return (row.config as { url?: string })?.url ?? "";
  }
  if (row.type === "datadog") {
    const c = row.config as { url?: string; site?: string };
    if (c?.url) return c.url;
    if (c?.site) return `site:${c.site}`;
    return "datadoghq.com";
  }
  if (row.type === "gcs") {
    const c = row.config as { bucket?: string; prefix?: string };
    return c?.prefix ? `gs://${c.bucket}/${c.prefix}` : `gs://${c?.bucket ?? ""}`;
  }
  return "";
}

// ----- MDM / EDR section ---------------------------------------------

export function MDMProvidersSection() {
  const { data, isLoading } =
    useFetchApi<MDMProvider[]>("/admin/mdm-providers");

  const [editing, setEditing] = useState<MDMProvider | null>(null);
  const [modalOpen, setModalOpen] = useState(false);

  return (
    <div>
      <Paragraph>
        Connect Microsoft Intune, SentinelOne, Huntress, or CrowdStrike
        Falcon to require devices in good security standing before
        they&apos;re allowed in the network. Configured providers can
        be referenced from any posture check via the &quot;Endpoint
        Security&quot; check type.
      </Paragraph>
      <HelpText>
        Credentials are encrypted at rest. Posture lookups are cached
        for 5 minutes per device to avoid hammering the vendor API.
      </HelpText>

      <div className="mt-6 flex justify-end">
        <Button
          variant="primary"
          onClick={() => {
            setEditing(null);
            setModalOpen(true);
          }}
        >
          <PlusCircleIcon size={16} /> Add provider
        </Button>
      </div>

      <div className="mt-4">
        {isLoading && (
          <Paragraph className="text-neutral-600 dark:text-nb-gray-300">
            Loading…
          </Paragraph>
        )}
        {!isLoading && (!data || data.length === 0) && (
          <EmptyState message="No MDM/EDR providers configured. Click Add provider to connect Intune, SentinelOne, Huntress, or CrowdStrike Falcon." />
        )}
        {data && data.length > 0 && (
          <table className="w-full text-sm">
            <TableHead
              cols={["Type", "Name", "Tenant / Endpoint", "Status", ""]}
            />
            <tbody>
              {data.map((row) => (
                <MDMRow
                  key={row.id}
                  row={row}
                  onEdit={() => {
                    setEditing(row);
                    setModalOpen(true);
                  }}
                />
              ))}
            </tbody>
          </table>
        )}
      </div>

      <MDMProviderModal
        open={modalOpen}
        setOpen={setModalOpen}
        existing={editing}
      />
    </div>
  );
}

function MDMRow({
  row,
  onEdit,
}: {
  row: MDMProvider;
  onEdit: () => void;
}) {
  const { mutate } = useSWRConfig();
  const api = useApiCall(`/admin/mdm-providers/${row.id}`);

  const onDelete = async () => {
    if (!confirm(`Delete "${row.name}"? This cannot be undone.`)) return;
    try {
      await api.del();
      await mutate("/admin/mdm-providers");
      notify({ title: "Deleted", description: row.name });
    } catch {}
  };

  return (
    <tr className="border-t border-nb-gray-900">
      <td className="py-3">
        <MDMTypeBadge type={row.type} />
      </td>
      <td className="py-3">{row.name}</td>
      <td className="py-3 font-mono text-xs text-nb-gray-300">
        {mdmEndpointLabel(row)}
      </td>
      <td className="py-3">
        <EnabledStatus enabled={row.enabled} />
      </td>
      <td className="py-3 text-right">
        <RowActions onEdit={onEdit} onDelete={onDelete} />
      </td>
    </tr>
  );
}

function MDMTypeBadge({ type }: { type: MDMProviderType }) {
  const labels: Record<MDMProviderType, string> = {
    intune: "Intune",
    sentinelone: "SentinelOne",
    huntress: "Huntress",
    crowdstrike: "CrowdStrike",
  };
  return (
    <span className="inline-flex items-center gap-1 rounded bg-nb-gray-900 px-2 py-1 text-xs text-violet-300">
      <ShieldCheckIcon size={12} /> {labels[type]}
    </span>
  );
}

function mdmEndpointLabel(row: MDMProvider): string {
  if (row.type === "intune") {
    const c = row.config as { tenant_id?: string };
    return c?.tenant_id ? `tenant:${c.tenant_id}` : "";
  }
  if (row.type === "sentinelone") {
    return (row.config as { management_url?: string })?.management_url ?? "";
  }
  if (row.type === "huntress") {
    return "api.huntress.io";
  }
  if (row.type === "crowdstrike") {
    const c = row.config as { cloud?: string };
    return c?.cloud ? `cloud:${c.cloud}` : "cloud:us-1";
  }
  return "";
}

// ----- Activity Streamer section -------------------------------------

export function ActivityExportersSection() {
  const { data, isLoading } =
    useFetchApi<ActivityExporter[]>("/admin/activity-exporters");

  const [editing, setEditing] = useState<ActivityExporter | null>(null);
  const [modalOpen, setModalOpen] = useState(false);

  return (
    <div>
      <Paragraph>
        Stream the audit log to your SIEM in real time. Every Activity
        event (peer login, posture denial, settings change, admission
        revocation, …) fans out to the destinations listed here in
        addition to whatever the operator configured via{" "}
        <code className={"font-mono text-xs"}>
          OPENZRO_ACTIVITY_EXPORT_*
        </code>{" "}
        env vars at boot.
      </Paragraph>
      <HelpText>
        Credentials are encrypted at rest. Custom payload templates let
        you reshape events to match the SIEM&apos;s expected schema —
        bring your own JSON layout without standing up Vector or Fluent
        Bit in the middle.
      </HelpText>

      <div className="mt-6 flex justify-end">
        <Button
          variant="primary"
          onClick={() => {
            setEditing(null);
            setModalOpen(true);
          }}
        >
          <PlusCircleIcon size={16} /> Add exporter
        </Button>
      </div>

      <div className="mt-4">
        {isLoading && (
          <Paragraph className="text-neutral-600 dark:text-nb-gray-300">
            Loading…
          </Paragraph>
        )}
        {!isLoading && (!data || data.length === 0) && (
          <EmptyState message="No activity exporters configured. Click Add exporter to start streaming the audit log to Datadog, Elastic, or any HTTP receiver." />
        )}
        {data && data.length > 0 && (
          <table className="w-full text-sm">
            <TableHead
              cols={["Type", "Name", "Endpoint", "Template", "Status", ""]}
            />
            <tbody>
              {data.map((row) => (
                <ActivityExporterRow
                  key={row.id}
                  row={row}
                  onEdit={() => {
                    setEditing(row);
                    setModalOpen(true);
                  }}
                />
              ))}
            </tbody>
          </table>
        )}
      </div>

      <ActivityExporterModal
        open={modalOpen}
        setOpen={setModalOpen}
        existing={editing}
      />
    </div>
  );
}

function ActivityExporterRow({
  row,
  onEdit,
}: {
  row: ActivityExporter;
  onEdit: () => void;
}) {
  const { mutate } = useSWRConfig();
  const api = useApiCall(`/admin/activity-exporters/${row.id}`);

  const onDelete = async () => {
    if (!confirm(`Delete "${row.name}"? This cannot be undone.`)) return;
    try {
      await api.del();
      await mutate("/admin/activity-exporters");
      notify({ title: "Deleted", description: row.name });
    } catch {}
  };

  return (
    <tr className="border-t border-nb-gray-900">
      <td className="py-3">
        <ActivityExporterTypeBadge type={row.type} />
      </td>
      <td className="py-3">{row.name}</td>
      <td className="py-3 font-mono text-xs text-nb-gray-300">
        {activityEndpointLabel(row)}
      </td>
      <td className="py-3 text-xs text-nb-gray-300">
        {row.template ? "custom" : "default"}
      </td>
      <td className="py-3">
        <EnabledStatus enabled={row.enabled} />
      </td>
      <td className="py-3 text-right">
        <RowActions onEdit={onEdit} onDelete={onDelete} />
      </td>
    </tr>
  );
}

function ActivityExporterTypeBadge({ type }: { type: ActivityExporterType }) {
  const map: Record<ActivityExporterType, { label: string }> = {
    http: { label: "HTTP" },
    datadog: { label: "Datadog" },
    elastic: { label: "Elastic" },
  };
  return (
    <span className="inline-flex items-center gap-1 rounded bg-nb-gray-900 px-2 py-1 text-xs text-violet-300">
      <CableIcon size={12} /> {map[type].label}
    </span>
  );
}

function activityEndpointLabel(row: ActivityExporter): string {
  const cfg = (row.config ?? {}) as Record<string, unknown>;
  if (row.type === "datadog") {
    if (cfg.url) return String(cfg.url);
    if (cfg.site) return `site:${cfg.site}`;
    return "datadoghq.com";
  }
  if (typeof cfg.url === "string") return cfg.url;
  return "";
}

// ----- SCIM Provisioning section -------------------------------------

export function SCIMSetupSection() {
  const baseURL = API_ORIGIN
    ? `${API_ORIGIN.replace(/\/+$/, "")}/scim/v2`
    : "https://your-management.example.com/scim/v2";

  return (
    <div>
      <Paragraph>
        Connect your enterprise IdP (Okta, Microsoft Entra, JumpCloud,
        Authentik, …) to auto-provision Users and Groups into openZro.
        Membership in a SCIM group becomes the user&apos;s AutoGroups
        list.
      </Paragraph>
      <HelpText>
        SCIM-provisioned users carry an{" "}
        <code className="font-mono text-xs">issued = integration</code>{" "}
        marker. Edits made through the dashboard to those users will
        be overwritten on the next sync from the IdP — that&apos;s
        the IdP-as-source-of-truth contract, intentional.
      </HelpText>

      <div className="mt-6 space-y-4">
        <div>
          <label className="text-xs text-nb-gray-300 uppercase tracking-wide">
            Tenant URL
          </label>
          <Code message="Copied!">{baseURL}</Code>
        </div>

        <div>
          <label className="text-xs text-nb-gray-300 uppercase tracking-wide">
            Authentication
          </label>
          <Paragraph className="text-sm">
            Bearer token — issue a Personal Access Token to a service
            user with the <b>admin</b> or <b>owner</b> role and paste
            the token into your IdP&apos;s SCIM connector.
          </Paragraph>
          <InlineLink href="/team/service-users">
            <KeyRoundIcon size={12} /> Manage service users & tokens
          </InlineLink>
        </div>

        <details className="mt-4 rounded-md border border-nb-gray-800 bg-nb-gray-940 p-4">
          <summary className="cursor-pointer text-sm">
            Okta — Provisioning configuration
          </summary>
          <ol className="mt-3 list-decimal pl-5 text-sm text-nb-gray-200 space-y-1">
            <li>
              In the Okta admin console, go to <b>Applications</b> →
              your openZro app → <b>Provisioning</b> →{" "}
              <b>Integration</b>.
            </li>
            <li>
              <b>Enable API integration</b>. Set <b>Base URL</b> to{" "}
              <code className="font-mono text-xs">{baseURL}</code>.
            </li>
            <li>
              Set <b>API Token</b> to your PAT (
              <code className="font-mono text-xs">nbp_...</code>).
            </li>
            <li>
              Click <b>Test API Credentials</b>. Save.
            </li>
            <li>
              Under <b>To App</b>, enable <i>Create Users</i>,{" "}
              <i>Update User Attributes</i>, <i>Deactivate Users</i>.
            </li>
          </ol>
        </details>

        <details className="rounded-md border border-nb-gray-800 bg-nb-gray-940 p-4">
          <summary className="cursor-pointer text-sm">
            Microsoft Entra (Azure AD) — Provisioning configuration
          </summary>
          <ol className="mt-3 list-decimal pl-5 text-sm text-nb-gray-200 space-y-1">
            <li>
              <b>Enterprise applications</b> → your openZro app →{" "}
              <b>Provisioning</b>.
            </li>
            <li>
              <b>Provisioning Mode</b> = <b>Automatic</b>.
            </li>
            <li>
              <b>Tenant URL</b>:{" "}
              <code className="font-mono text-xs">{baseURL}</code>.
            </li>
            <li>
              <b>Secret Token</b>: your PAT.
            </li>
            <li>
              <b>Test Connection</b>, save, set <b>Provisioning Status</b>{" "}
              to <b>On</b>.
            </li>
          </ol>
        </details>

        <details className="rounded-md border border-nb-gray-800 bg-nb-gray-940 p-4">
          <summary className="cursor-pointer text-sm">
            JumpCloud, Authentik, others
          </summary>
          <Paragraph className="mt-3 text-sm">
            Any SCIM 2.0-compliant IdP works. Use{" "}
            <code className="font-mono text-xs">{baseURL}</code> as
            the SCIM endpoint and the PAT as the bearer token.
          </Paragraph>
          <InlineLink
            href={`${baseURL}/ServiceProviderConfig`}
            target="_blank"
            className="mt-2"
          >
            View ServiceProviderConfig <ExternalLinkIcon size={12} />
          </InlineLink>
        </details>
      </div>
    </div>
  );
}

// ----- Shared subcomponents ------------------------------------------

function TableHead({ cols }: { cols: string[] }) {
  return (
    <thead className="text-left text-nb-gray-300 text-xs uppercase">
      <tr>
        {cols.map((c, i) => (
          <th key={i} className="py-2">
            {c}
          </th>
        ))}
      </tr>
    </thead>
  );
}

function EmptyState({ message }: { message: string }) {
  return (
    <div className="rounded-md border border-dashed border-nb-gray-700 p-8 text-center">
      <Paragraph className="text-nb-gray-300">{message}</Paragraph>
    </div>
  );
}

function EnabledStatus({ enabled }: { enabled: boolean }) {
  return (
    <span className={enabled ? "text-emerald-400" : "text-nb-gray-400"}>
      {enabled ? "Enabled" : "Disabled"}
    </span>
  );
}

function RowActions({
  onEdit,
  onDelete,
}: {
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <>
      <Button variant="secondary" onClick={onEdit} className="mr-2">
        Edit
      </Button>
      <Button variant="danger-outline" onClick={onDelete}>
        <Trash2Icon size={14} />
      </Button>
    </>
  );
}
