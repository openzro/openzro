"use client";

import Breadcrumbs from "@components/Breadcrumbs";
import Button from "@components/Button";
import Code from "@components/Code";
import HelpText from "@components/HelpText";
import InlineLink from "@components/InlineLink";
import { notify } from "@components/Notification";
import Paragraph from "@components/Paragraph";
import Separator from "@components/Separator";
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
  Trash2Icon,
  UsersIcon,
} from "lucide-react";
import React, { useState } from "react";
import { useSWRConfig } from "swr";
import SettingsIcon from "@/assets/icons/SettingsIcon";
import { Account } from "@/interfaces/Account";
import { FlowExport, FlowExportType } from "@/interfaces/FlowExport";
import FlowExportModal from "@/modules/flow-exports/FlowExportModal";

type Props = {
  account: Account;
};

// IntegrationsTab is the dashboard surface for the
// /api/admin/flow-exports backend that landed in PR-G. Operators
// configure SIEM streams (Elastic), cold archives (S3), and HTTP
// webhooks here without touching env vars.
export default function IntegrationsTab(_: Readonly<Props>) {
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
    <Tabs.Content value="integrations" className="px-default pb-8 pt-6">
      <Breadcrumbs>
        <Breadcrumbs.Item
          label={"Settings"}
          href={"/settings"}
          icon={<SettingsIcon size={13} />}
        />
        <Breadcrumbs.Item
          label={"Integrations"}
          icon={<CableIcon size={13} />}
        />
      </Breadcrumbs>

      <h2 className="mt-2">Integrations</h2>
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
          <div className="rounded-md border border-dashed border-nb-gray-700 p-8 text-center">
            <Paragraph className="text-nb-gray-300">
              No destinations configured. Click <b>Add destination</b> to
              start streaming traffic events.
            </Paragraph>
          </div>
        )}
        {data && data.length > 0 && (
          <table className="w-full text-sm">
            <thead className="text-left text-nb-gray-300 text-xs uppercase">
              <tr>
                <th className="py-2">Type</th>
                <th>Name</th>
                <th>Endpoint</th>
                <th>Status</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {data.map((row) => (
                <Row key={row.id} row={row} onEdit={() => openEdit(row)} />
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

      <div className="my-10">
        <Separator />
      </div>

      <SCIMSetupSection />
    </Tabs.Content>
  );
}

// SCIMSetupSection points operators at the static configuration they
// need to plug into Okta / Entra / JumpCloud. The SCIM protocol
// itself is fully server-side (no UI mutations); this section's job
// is just discoverability and copy-paste convenience.
function SCIMSetupSection() {
  // SCIM lives on the management server, NOT on the dashboard's
  // origin. Use apiOrigin (the management's HTTP base) so the URL
  // we tell IdPs to call actually points at the right host.
  const baseURL = API_ORIGIN
    ? `${API_ORIGIN.replace(/\/+$/, "")}/scim/v2`
    : "https://your-management.example.com/scim/v2";

  return (
    <div>
      <h2 className="flex items-center gap-2">
        <UsersIcon size={18} className="text-violet-300" />
        SCIM 2.0 Provisioning
      </h2>
      <Paragraph>
        Connect your enterprise IdP (Okta, Microsoft Entra, JumpCloud,
        Authentik, …) to auto-provision Users and Groups into openZro.
        Membership in a SCIM group becomes the user&apos;s AutoGroups
        list — every peer the user registers automatically inherits
        the group&apos;s policies.
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
            Bearer token — issue a Personal Access Token to a
            service user with the <b>admin</b> or <b>owner</b> role and
            paste the token into your IdP&apos;s SCIM connector.
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
              your openZro app → <b>Provisioning</b> → <b>Integration</b>.
            </li>
            <li>
              <b>Enable API integration</b>. Set <b>Base URL</b> to{" "}
              <code className="font-mono text-xs">{baseURL}</code>.
            </li>
            <li>
              Set <b>API Token</b> to the PAT you generated above
              (format: <code className="font-mono text-xs">nbp_...</code>).
            </li>
            <li>
              Click <b>Test API Credentials</b>. Save.
            </li>
            <li>
              Under <b>To App</b>, enable <i>Create Users</i>,{" "}
              <i>Update User Attributes</i>, and <i>Deactivate Users</i>.
            </li>
          </ol>
        </details>

        <details className="rounded-md border border-nb-gray-800 bg-nb-gray-940 p-4">
          <summary className="cursor-pointer text-sm">
            Microsoft Entra (Azure AD) — Provisioning configuration
          </summary>
          <ol className="mt-3 list-decimal pl-5 text-sm text-nb-gray-200 space-y-1">
            <li>
              In Entra admin center, <b>Enterprise applications</b> →
              your openZro app → <b>Provisioning</b>.
            </li>
            <li>
              Set <b>Provisioning Mode</b> to <b>Automatic</b>.
            </li>
            <li>
              <b>Tenant URL</b>:{" "}
              <code className="font-mono text-xs">{baseURL}</code>.
            </li>
            <li>
              <b>Secret Token</b>: your PAT (
              <code className="font-mono text-xs">nbp_...</code>).
            </li>
            <li>
              Click <b>Test Connection</b>. Save. Set <b>Provisioning Status</b> to{" "}
              <b>On</b>.
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
            the SCIM endpoint and the PAT as the bearer token. Our
            ServiceProviderConfig advertises the supported features
            (PATCH, userName-eq filtering, no bulk, no sort);
            well-behaved IdPs read it on first connect and adapt.
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

function Row({
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
      // useApiCall already surfaces the error toast
    }
  };

  const endpoint = endpointLabel(row);

  return (
    <tr className="border-t border-nb-gray-900">
      <td className="py-3">
        <TypeBadge type={row.type} />
      </td>
      <td className="py-3">{row.name}</td>
      <td className="py-3 font-mono text-xs text-nb-gray-300">
        {endpoint}
      </td>
      <td className="py-3">
        <span
          className={
            row.enabled ? "text-emerald-400" : "text-nb-gray-400"
          }
        >
          {row.enabled ? "Enabled" : "Disabled"}
        </span>
      </td>
      <td className="py-3 text-right">
        <Button variant="secondary" onClick={onEdit} className="mr-2">
          Edit
        </Button>
        <Button variant="danger-outline" onClick={onDelete}>
          <Trash2Icon size={14} />
        </Button>
      </td>
    </tr>
  );
}

function TypeBadge({ type }: { type: FlowExportType }) {
  const map = {
    elastic: { icon: <CableIcon size={12} />, label: "Elastic" },
    s3: { icon: <CloudIcon size={12} />, label: "S3" },
    http: { icon: <GlobeIcon size={12} />, label: "HTTP" },
  };
  const m = map[type];
  return (
    <span className="inline-flex items-center gap-1 rounded bg-nb-gray-900 px-2 py-1 text-xs text-violet-300">
      {m.icon}
      {m.label}
    </span>
  );
}

function endpointLabel(row: FlowExport): string {
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
