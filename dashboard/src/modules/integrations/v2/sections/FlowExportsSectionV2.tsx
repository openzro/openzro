"use client";

import { notify } from "@components/Notification";
import { useApiCall } from "@utils/api";
import useFetchApi from "@utils/api";
import { PlusCircle } from "lucide-react";
import React, { useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import {
  FlowExport,
  FlowExportType,
} from "@/interfaces/FlowExport";
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
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

  // Register the Add destination CTA in the V2 topbar slot. When
  // the operator switches sub-tab, this section unmounts and the
  // slot clears — the next section's own useV2TopbarRight kicks in.
  useV2TopbarRight(
    <OzButton variant="primary" type="button" onClick={openCreate}>
      <PlusCircle size={14} />
      Add destination
    </OzButton>,
  );

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
        isLoading={isLoading}
        // Always render the grid — the trailing AddDestinationCard
        // covers the cold-start case visually, so we don't need a
        // separate empty-state OzCard. The grid degrades gracefully
        // when data is empty (just the Add card alone).
        isEmpty={false}
        emptyMessage="No destinations configured. Click Add destination to start streaming traffic events."
      >
        {data?.map((row) => (
          <FlowExportCard key={row.id} row={row} onEdit={() => openEdit(row)} />
        ))}
        {/* Trailing "+ Add destination" card — handoff-flavored
            secondary entry point right inside the grid. Same
            dimensions as the populated cards so the grid alignment
            holds; dashed border + ghost paint signals it's the
            "create" affordance instead of an existing destination. */}
        <AddDestinationCard onClick={openCreate} />
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

// Trailing "create new" card — dashed border, centered icon + label,
// stretches to match the IntegrationCard's height in the same grid
// row. Click triggers the same openCreate flow the topbar Add
// button uses. Pointer-cursor + hover paint signals it's interactive.
function AddDestinationCard({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="group flex min-h-[120px] flex-col items-center justify-center gap-2 rounded-oz2-card border-2 border-dashed border-oz2-border bg-transparent p-4 text-oz2-text-muted transition-colors hover:border-oz2-acc hover:bg-oz2-acc-soft hover:text-oz2-acc-text"
    >
      <span
        aria-hidden
        className="grid h-10 w-10 place-items-center rounded-[10px] border border-dashed border-oz2-border-strong text-oz2-text-faint transition-colors group-hover:border-oz2-acc group-hover:text-oz2-acc-text"
      >
        <PlusCircle size={18} />
      </span>
      <span className="text-[13.5px] font-medium">Add destination</span>
      <span className="text-[11.5px] text-oz2-text-faint">
        Stream or archive traffic events to a new destination
      </span>
    </button>
  );
}
