"use client";

import Breadcrumbs from "@components/Breadcrumbs";
import Button from "@components/Button";
import Code from "@components/Code";
import HelpText from "@components/HelpText";
import InlineLink from "@components/InlineLink";
import { notify } from "@components/Notification";
import Paragraph from "@components/Paragraph";
import { SegmentedTabs } from "@components/SegmentedTabs";
import * as Tabs from "@radix-ui/react-tabs";
import useFetchApi, { useApiCall } from "@utils/api";
import { API_ORIGIN } from "@utils/openzro";
import {
  CableIcon,
  CloudIcon,
  ExternalLinkIcon,
  GlobeIcon,
  KeyRoundIcon,
  PlusCircleIcon,
  ShieldCheckIcon,
  Trash2Icon,
  UsersIcon,
} from "lucide-react";
import React, { useState } from "react";
import { useSWRConfig } from "swr";
import SettingsIcon from "@/assets/icons/SettingsIcon";
import { Account } from "@/interfaces/Account";
import { FlowExport, FlowExportType } from "@/interfaces/FlowExport";
import { MDMProvider, MDMProviderType } from "@/interfaces/MDMProvider";
import FlowExportModal from "@/modules/flow-exports/FlowExportModal";
import MDMProviderModal from "@/modules/mdm-providers/MDMProviderModal";

type Props = {
  account: Account;
};

// IntegrationsTab is the dashboard surface for runtime-configurable
// external integrations. Three sub-tabs:
//
//   Flow Exports — SIEM/archive destinations for traffic events
//   MDM / EDR    — Intune/SentinelOne/Huntress posture providers
//   SCIM         — read-only setup info for IdP provisioning
//
// All three share the same encrypted-at-rest credential envelope on
// the backend; they're grouped here because they're all "outbound
// connections to corporate systems" from the operator's perspective.
export default function IntegrationsTab(_: Readonly<Props>) {
  const [inner, setInner] = useState<string>("flow");

  return (
    <Tabs.Content value="integrations">
      <div className={"p-default py-6"}>
        <Breadcrumbs>
          <Breadcrumbs.Item
            href={"/settings"}
            label={"Settings"}
            icon={<SettingsIcon size={13} />}
          />
          <Breadcrumbs.Item
            href={"/settings?tab=integrations"}
            label={"Integrations"}
            icon={<CableIcon size={14} />}
            active
          />
        </Breadcrumbs>
        <h1>Integrations</h1>
        <Paragraph>
          External destinations and providers — SIEM streaming + cold
          archive for traffic events, MDM/EDR vendors for posture
          compliance, and SCIM 2.0 for user provisioning from your IdP.
        </Paragraph>

        <div className={"mt-6"}>
          <SegmentedTabs value={inner} onChange={setInner}>
            <SegmentedTabs.List>
              <SegmentedTabs.Trigger value="flow">
                <CableIcon size={14} /> Flow Exports
              </SegmentedTabs.Trigger>
              <SegmentedTabs.Trigger value="mdm">
                <ShieldCheckIcon size={14} /> MDM / EDR
              </SegmentedTabs.Trigger>
              <SegmentedTabs.Trigger value="scim">
                <UsersIcon size={14} /> SCIM Provisioning
              </SegmentedTabs.Trigger>
            </SegmentedTabs.List>

            <SegmentedTabs.Content value="flow">
              <FlowExportsSection />
            </SegmentedTabs.Content>
            <SegmentedTabs.Content value="mdm">
              <MDMProvidersSection />
            </SegmentedTabs.Content>
            <SegmentedTabs.Content value="scim">
              <SCIMSetupSection />
            </SegmentedTabs.Content>
          </SegmentedTabs>
        </div>
      </div>
    </Tabs.Content>
  );
}

// ----- Flow Exports section ------------------------------------------

function FlowExportsSection() {
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
          <Paragraph className="text-nb-gray-300">Loading…</Paragraph>
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
  const map = {
    elastic: { icon: <CableIcon size={12} />, label: "Elastic" },
    s3: { icon: <CloudIcon size={12} />, label: "S3" },
    http: { icon: <GlobeIcon size={12} />, label: "HTTP" },
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
  return "";
}

// ----- MDM / EDR section ---------------------------------------------

function MDMProvidersSection() {
  const { data, isLoading } =
    useFetchApi<MDMProvider[]>("/admin/mdm-providers");

  const [editing, setEditing] = useState<MDMProvider | null>(null);
  const [modalOpen, setModalOpen] = useState(false);

  return (
    <div>
      <Paragraph>
        Connect Microsoft Intune, SentinelOne, or Huntress to require
        devices in good security standing before they&apos;re allowed
        in the network. Configured providers can be referenced from
        any posture check via the &quot;Endpoint Security&quot; check
        type.
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
          <Paragraph className="text-nb-gray-300">Loading…</Paragraph>
        )}
        {!isLoading && (!data || data.length === 0) && (
          <EmptyState message="No MDM/EDR providers configured. Click Add provider to connect Intune, SentinelOne, or Huntress." />
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
  return "";
}

// ----- SCIM Provisioning section -------------------------------------

function SCIMSetupSection() {
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
