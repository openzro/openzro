"use client";

import { notify } from "@components/Notification";
import { useApiCall } from "@utils/api";
import useFetchApi from "@utils/api";
import { PlusCircle } from "lucide-react";
import React, { useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import {
  MDMProvider,
  MDMProviderType,
} from "@/interfaces/MDMProvider";
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import IntegrationCard from "@/modules/integrations/v2/sections/IntegrationCard";
import SectionShell from "@/modules/integrations/v2/sections/SectionShell";
import MDMProviderModal from "@/modules/mdm-providers/MDMProviderModal";

// MDMProvidersSectionV2 — v2 card-grid version of the legacy
// MDMProvidersSection. Same data + modal + delete; just renders
// each provider as an IntegrationCard.

const TYPE_META: Record<
  MDMProviderType,
  { label: string; logo: string; color: string }
> = {
  intune: { label: "Microsoft Intune", logo: "M", color: "#0078D4" },
  sentinelone: { label: "SentinelOne", logo: "S", color: "#6B0AC9" },
  huntress: { label: "Huntress", logo: "H", color: "#00C2A8" },
  crowdstrike: { label: "CrowdStrike Falcon", logo: "C", color: "#FA0000" },
};

function endpointLabel(row: MDMProvider): string {
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

export default function MDMProvidersSectionV2() {
  const { data, isLoading } = useFetchApi<MDMProvider[]>(
    "/admin/mdm-providers",
  );
  const [editing, setEditing] = useState<MDMProvider | null>(null);
  const [modalOpen, setModalOpen] = useState(false);

  const openCreate = () => {
    setEditing(null);
    setModalOpen(true);
  };
  const openEdit = (row: MDMProvider) => {
    setEditing(row);
    setModalOpen(true);
  };

  useV2TopbarRight(
    <OzButton variant="primary" type="button" onClick={openCreate}>
      <PlusCircle size={14} />
      Add provider
    </OzButton>,
  );

  return (
    <>
      <SectionShell
        description={
          <>
            Connect Microsoft Intune, SentinelOne, Huntress, or CrowdStrike
            Falcon to require devices in good security standing before
            they&apos;re allowed in the network. Configured providers can be
            referenced from any posture check via the &quot;Endpoint
            Security&quot; check type.
          </>
        }
        hint={
          <>
            Credentials are encrypted at rest. Posture lookups are cached
            for 5 minutes per device to avoid hammering the vendor API.
          </>
        }
        isLoading={isLoading}
        isEmpty={!data || data.length === 0}
        emptyMessage="No MDM/EDR providers configured. Click Add provider to connect Intune, SentinelOne, Huntress, or CrowdStrike Falcon."
      >
        {data?.map((row) => (
          <MDMCard key={row.id} row={row} onEdit={() => openEdit(row)} />
        ))}
      </SectionShell>

      <MDMProviderModal
        open={modalOpen}
        setOpen={setModalOpen}
        existing={editing}
      />
    </>
  );
}

function MDMCard({
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
