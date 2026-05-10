"use client";

import { notify } from "@components/Notification";
import { useApiCall } from "@utils/api";
import useFetchApi from "@utils/api";
import React, { useState } from "react";
import { useSWRConfig } from "swr";
import {
  FlowExport,
  FlowExportType,
} from "@/interfaces/FlowExport";
import FlowExportModal from "@/modules/flow-exports/FlowExportModal";
import IntegrationCard from "@/modules/integrations/v2/sections/IntegrationCard";
import SectionShell from "@/modules/integrations/v2/sections/SectionShell";

// FlowExportsSectionV2 — v2 paint over the legacy FlowExportsSection.
// Renders each FlowExport destination as a card via IntegrationCard
// instead of the legacy <table> rows. SWR endpoint, modal flow and
// delete confirm are unchanged.

const TYPE_META: Record<
  FlowExportType,
  { label: string; logo: string; color: string }
> = {
  elastic: { label: "Elastic", logo: "E", color: "#00BFB3" },
  s3: { label: "S3", logo: "S", color: "#FF9900" },
  http: { label: "HTTP", logo: "H", color: "#7C3AED" },
  datadog: { label: "Datadog", logo: "D", color: "#632CA6" },
  gcs: { label: "GCS", logo: "G", color: "#4285F4" },
};

function endpointLabel(row: FlowExport): string {
  if (row.type === "elastic") {
    return (row.config as { url?: string })?.url ?? "";
  }
  if (row.type === "s3") {
    const c = row.config as { bucket?: string; endpoint?: string };
    return c?.endpoint ? `${c.endpoint}/${c.bucket}` : (c?.bucket ?? "");
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
    return c?.prefix
      ? `gs://${c.bucket}/${c.prefix}`
      : `gs://${c?.bucket ?? ""}`;
  }
  return "";
}

export default function FlowExportsSectionV2() {
  const { data, isLoading } = useFetchApi<FlowExport[]>("/admin/flow-exports");
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
    <>
      <SectionShell
        description={
          <>
            Stream traffic events to your SIEM (Elastic) or cold-archive them
            to S3-compatible storage (AWS S3, Cloudflare R2, Backblaze B2,
            Google Cloud Storage via Interoperability mode, MinIO). Each
            destination runs independently — a slow SIEM never blocks your
            archive, and vice-versa.
          </>
        }
        hint={
          <>
            Credentials are encrypted at rest with the management&apos;s
            DataStoreEncryptionKey. The dashboard never reads them back —
            edit a destination and re-enter the value to rotate it.
          </>
        }
        addLabel="Add destination"
        onAdd={openCreate}
        isLoading={isLoading}
        isEmpty={!data || data.length === 0}
        emptyMessage="No destinations configured. Click Add destination to start streaming traffic events."
      >
        {data?.map((row) => (
          <FlowExportCard key={row.id} row={row} onEdit={() => openEdit(row)} />
        ))}
      </SectionShell>

      <FlowExportModal
        open={modalOpen}
        setOpen={setModalOpen}
        existing={editing}
      />
    </>
  );
}

function FlowExportCard({
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

  const meta = TYPE_META[row.type];
  return (
    <IntegrationCard
      color={meta.color}
      logo={meta.logo}
      name={row.name}
      typeLabel={meta.label}
      endpoint={endpointLabel(row)}
      enabled={row.enabled}
      onEdit={onEdit}
      onDelete={onDelete}
    />
  );
}
