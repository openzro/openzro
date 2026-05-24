"use client";

import { notify } from "@components/Notification";
import { TooltipProvider } from "@components/Tooltip";
import { useApiCall } from "@utils/api";
import {
  ArrowLeft,
  ExternalLinkIcon,
  Layers,
  PencilLine,
  Power,
  Trash2,
} from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import React, { useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";
import { useDialog } from "@/contexts/DialogProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { DNSZone } from "@/interfaces/DNSZone";
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import DNSRecordsSection from "@/modules/dns-zones/DNSRecordsSection";
import DNSZoneModal from "@/modules/dns-zones/DNSZoneModal";

type Props = {
  zone: DNSZone;
};

export default function DNSZoneDetailPage({ zone }: Props) {
  const router = useRouter();
  const { confirm } = useDialog();
  const { mutate } = useSWRConfig();
  const { permission } = usePermissions();
  const zoneRequest = useApiCall<DNSZone>("/dns/zones");

  const [editOpen, setEditOpen] = useState(false);

  const enabled = zone.enabled ?? true;
  const recordCount = zone.records?.length ?? 0;
  const groupCount = zone.distribution_groups?.length ?? 0;

  const handleDelete = async () => {
    const choice = await confirm({
      title: `Delete '${zone.name}'?`,
      description:
        "Are you sure you want to delete this zone? Records under this zone will be removed and peers will lose authoritative resolution for the domain. This action cannot be undone.",
      confirmText: "Delete",
      cancelText: "Cancel",
      type: "danger",
    });
    if (!choice) return;
    notify({
      title: "Zone " + zone.name,
      description: "The zone was successfully removed.",
      loadingMessage: "Deleting the zone...",
      promise: zoneRequest.del("", `/${zone.id}`).then(() => {
        mutate("/dns/zones");
        router.push("/dns/zones");
      }),
    });
  };

  useV2TopbarRight(
    <div className="flex items-center gap-2">
      <OzButton
        variant="default"
        type="button"
        onClick={() => router.push("/dns/zones")}
      >
        <ArrowLeft size={14} />
        Back to zones
      </OzButton>
      <OzButton
        variant="default"
        type="button"
        disabled={!permission.dns_zones.update}
        onClick={() => setEditOpen(true)}
      >
        <PencilLine size={14} />
        Edit zone
      </OzButton>
      <OzButton
        variant="default"
        type="button"
        disabled={!permission.dns_zones.delete}
        onClick={handleDelete}
      >
        <Trash2 size={14} className="text-oz2-err" />
        Delete
      </OzButton>
    </div>,
  );

  return (
    <TooltipProvider delayDuration={250} skipDelayDuration={100}>
      {editOpen && (
        <DNSZoneModal
          preset={zone}
          open={editOpen}
          onOpenChange={setEditOpen}
        />
      )}

      <div className="px-8 pb-5 pt-8">
        <Link
          href="/dns/zones"
          className="inline-flex items-center gap-1 text-[12px] text-oz2-text-faint transition-colors hover:text-oz2-text-2"
        >
          <ExternalLinkIcon size={11} className="rotate-180" />
          DNS Zones
        </Link>

        <div className="mt-3 flex max-w-6xl flex-wrap items-start justify-between gap-4">
          <div className="flex min-w-0 items-start gap-4">
            <div
              aria-hidden
              className="relative grid h-12 w-12 shrink-0 place-items-center rounded-[12px] border border-oz2-border-soft bg-oz2-bg-sunken text-oz2-text-2"
            >
              <Layers size={20} />
              <span
                className={
                  "absolute -bottom-1 -right-1 h-3 w-3 rounded-full border-2 border-oz2-bg " +
                  (enabled ? "bg-oz2-ok" : "bg-oz2-text-faint")
                }
              />
            </div>
            <div className="min-w-0">
              <h1 className="text-[24px] font-semibold tracking-tight text-oz2-text">
                {zone.name}
              </h1>
              <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12.5px] text-oz2-text-muted">
                <span className="font-mono text-oz2-text-2">{zone.domain}</span>
                <span className="text-oz2-text-faint">·</span>
                <span className="inline-flex items-center gap-1">
                  <Power size={11} />
                  {enabled ? "Enabled" : "Disabled"}
                </span>
                <span className="text-oz2-text-faint">·</span>
                <span>
                  {groupCount} distribution group
                  {groupCount === 1 ? "" : "s"}
                </span>
                <span className="text-oz2-text-faint">·</span>
                <span>
                  {recordCount} record{recordCount === 1 ? "" : "s"}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div className="px-8 pb-12">
        <OzCard flush className="max-w-6xl">
          <DNSRecordsSection zone={zone} />
        </OzCard>
      </div>
    </TooltipProvider>
  );
}
