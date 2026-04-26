"use client";

import Breadcrumbs from "@components/Breadcrumbs";
import Button from "@components/Button";
import HelpText from "@components/HelpText";
import { notify } from "@components/Notification";
import Paragraph from "@components/Paragraph";
import * as Tabs from "@radix-ui/react-tabs";
import useFetchApi, { useApiCall } from "@utils/api";
import {
  CableIcon,
  CloudIcon,
  GlobeIcon,
  PlusCircleIcon,
  Trash2Icon,
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
        Stream traffic events to your SIEM (Elastic), cold-archive
        them to an S3-compatible bucket, or POST them to a generic
        HTTP webhook. Each destination runs independently — a slow
        SIEM never blocks your archive, and vice-versa.
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
    </Tabs.Content>
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
