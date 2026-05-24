"use client";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@components/DropdownMenu";
import { notify } from "@components/Notification";
import { useApiCall } from "@utils/api";
import { MoreVertical, PencilLine, Trash2 } from "lucide-react";
import * as React from "react";
import { useSWRConfig } from "swr";
import { useDialog } from "@/contexts/DialogProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { DNSZone } from "@/interfaces/DNSZone";

type Props = {
  zone: DNSZone;
  onEdit?: () => void;
};

export default function DNSZoneActionCell({ zone, onEdit }: Readonly<Props>) {
  const { confirm } = useDialog();
  const zoneRequest = useApiCall<DNSZone>("/dns/zones");
  const { mutate } = useSWRConfig();
  const { permission } = usePermissions();

  const deleteZone = async () => {
    notify({
      title: "Zone " + zone.name,
      description: "The zone was successfully removed.",
      promise: zoneRequest.del("", `/${zone.id}`).then(() => {
        mutate("/dns/zones");
      }),
      loadingMessage: "Deleting the zone...",
    });
  };

  const handleConfirm = async () => {
    const choice = await confirm({
      title: `Delete '${zone.name}'?`,
      description:
        "Are you sure you want to delete this zone? Records under this zone will be removed and peers will lose authoritative resolution for the domain. This action cannot be undone.",
      confirmText: "Delete",
      cancelText: "Cancel",
      type: "danger",
    });
    if (!choice) return;
    deleteZone().then();
  };

  return (
    <div className="flex justify-end pr-2">
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger
          asChild
          onClick={(e) => {
            e.stopPropagation();
            e.preventDefault();
          }}
        >
          <button
            type="button"
            aria-label="Row actions"
            className="grid h-8 w-8 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 transition-colors hover:bg-oz2-hover hover:border-oz2-border-strong"
          >
            <MoreVertical size={14} className="shrink-0" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-44">
          {onEdit && (
            <DropdownMenuItem
              onClick={(e) => {
                e.stopPropagation();
                onEdit();
              }}
            >
              <div className="flex items-center gap-3">
                <PencilLine size={14} className="shrink-0" />
                Edit
              </div>
            </DropdownMenuItem>
          )}
          <DropdownMenuItem
            disabled={!permission.dns_zones.delete}
            onClick={(e) => {
              e.stopPropagation();
              handleConfirm();
            }}
            variant="danger"
          >
            <div className="flex items-center gap-3">
              <Trash2 size={14} className="shrink-0" />
              Delete
            </div>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
