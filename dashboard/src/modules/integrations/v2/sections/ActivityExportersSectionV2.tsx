"use client";

import { notify } from "@components/Notification";
import { useApiCall } from "@utils/api";
import useFetchApi from "@utils/api";
import { PlusCircle } from "lucide-react";
import React, { useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import {
  ActivityExporter,
  ActivityExporterType,
} from "@/interfaces/ActivityExporter";
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import ActivityExporterModal from "@/modules/activity-exporters/ActivityExporterModal";
import IntegrationCard from "@/modules/integrations/v2/sections/IntegrationCard";
import SectionShell from "@/modules/integrations/v2/sections/SectionShell";

// ActivityExportersSectionV2 — v2 card-grid version of the legacy
// ActivityExportersSection. Same SWR endpoint, modal, and delete
// flow; only the row-table flips to a card grid.

const TYPE_META: Record<
  ActivityExporterType,
  { label: string; logo: string; color: string }
> = {
  http: { label: "HTTP", logo: "H", color: "#7C3AED" },
  datadog: { label: "Datadog", logo: "D", color: "#632CA6" },
  elastic: { label: "Elastic", logo: "E", color: "#00BFB3" },
};

function endpointLabel(row: ActivityExporter): string {
  const cfg = (row.config ?? {}) as Record<string, unknown>;
  if (row.type === "datadog") {
    if (cfg.url) return String(cfg.url);
    if (cfg.site) return `site:${cfg.site}`;
    return "datadoghq.com";
  }
  if (typeof cfg.url === "string") return cfg.url;
  return "";
}

export default function ActivityExportersSectionV2() {
  const { data, isLoading } = useFetchApi<ActivityExporter[]>(
    "/admin/activity-exporters",
  );
  const [editing, setEditing] = useState<ActivityExporter | null>(null);
  const [modalOpen, setModalOpen] = useState(false);

  const openCreate = () => {
    setEditing(null);
    setModalOpen(true);
  };
  const openEdit = (row: ActivityExporter) => {
    setEditing(row);
    setModalOpen(true);
  };

  useV2TopbarRight(
    <OzButton variant="primary" type="button" onClick={openCreate}>
      <PlusCircle size={14} />
      Add exporter
    </OzButton>,
  );

  return (
    <>
      <SectionShell
        description={
          <>
            Stream the audit log to your SIEM in real time. Every Activity
            event (peer login, posture denial, settings change, admission
            revocation, …) fans out to the destinations listed here in
            addition to whatever the operator configured via{" "}
            <code className="font-mono text-[12px]">
              OPENZRO_ACTIVITY_EXPORT_*
            </code>{" "}
            env vars at boot.
          </>
        }
        hint={
          <>
            Credentials are encrypted at rest. Custom payload templates let
            you reshape events to match the SIEM&apos;s expected schema —
            bring your own JSON layout without standing up Vector or Fluent
            Bit in the middle.
          </>
        }
        isLoading={isLoading}
        isEmpty={!data || data.length === 0}
        emptyMessage="No activity exporters configured. Click Add exporter to start streaming the audit log to Datadog, Elastic, or any HTTP receiver."
      >
        {data?.map((row) => (
          <ActivityExporterCard
            key={row.id}
            row={row}
            onEdit={() => openEdit(row)}
          />
        ))}
      </SectionShell>

      <ActivityExporterModal
        open={modalOpen}
        setOpen={setModalOpen}
        existing={editing}
      />
    </>
  );
}

function ActivityExporterCard({
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
    } catch {
      // useApiCall surfaces toast
    }
  };

  const meta = TYPE_META[row.type];
  // The legacy table had a "Template" column showing "custom" vs
  // "default" — surface it as a tiny annotation on the card.
  const templateNote = (
    <span className="font-mono text-[10.5px] uppercase tracking-wider text-oz2-text-faint">
      Template: {row.template ? "custom" : "default"}
    </span>
  );

  return (
    <IntegrationCard
      color={meta.color}
      logo={meta.logo}
      name={row.name}
      typeLabel={meta.label}
      endpoint={endpointLabel(row)}
      meta={templateNote}
      enabled={row.enabled}
      onEdit={onEdit}
      onDelete={onDelete}
    />
  );
}
