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
import { useState } from "react";
import { useSWRConfig } from "swr";
import { useDialog } from "@/contexts/DialogProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { GroupedRoute, Route } from "@/interfaces/Route";
import GroupedRouteUpdateModal from "@/modules/route-group/GroupedRouteUpdateModal";

// V2 paint of GroupedRouteActionCell — single kebab overflow opening
// a dropdown with Edit / Delete. Mirrors the v2 row pattern used in
// PeersTableV2 / NetworksTableV2 instead of the legacy inline
// Edit + Delete buttons. Logic (delete confirm + batch revoke) is
// preserved verbatim from the legacy cell.

type Props = {
  groupedRoute: GroupedRoute;
};

export default function GroupedRouteActionCellV2({ groupedRoute }: Props) {
  const { permission } = usePermissions();
  const { confirm } = useDialog();
  const routeRequest = useApiCall<Route>("/routes");
  const { mutate } = useSWRConfig();
  const [editModal, setEditModal] = useState(false);

  const handleRevoke = async () => {
    if (!groupedRoute.routes) return Promise.resolve();
    const batch = groupedRoute.routes.map((route) => {
      if (route.id) return routeRequest.del("", `/${route.id}`);
      return Promise.resolve(route);
    });

    notify({
      title: "Delete Network " + groupedRoute.network_id,
      description: "Network was successfully removed",
      promise: Promise.all(batch).then(() => {
        mutate("/routes");
      }),
      loadingMessage: "Deleting the network...",
    });
  };

  const handleConfirm = async () => {
    const choice = await confirm({
      title: `Delete network '${groupedRoute.network_id}'?`,
      description:
        "Are you sure you want to delete this network? All routes inside this network will be deleted. This action cannot be undone.",
      confirmText: "Delete",
      cancelText: "Cancel",
      type: "danger",
    });
    if (!choice) return;
    handleRevoke().then();
  };

  return (
    <div className="flex justify-end pr-2">
      {editModal && (
        <GroupedRouteUpdateModal
          groupedRoute={groupedRoute}
          open={editModal}
          onOpenChange={setEditModal}
        />
      )}
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
          <DropdownMenuItem
            disabled={!permission.routes.update}
            onClick={(e) => {
              e.stopPropagation();
              setEditModal(true);
            }}
          >
            <div className="flex items-center gap-3">
              <PencilLine size={14} className="shrink-0" />
              Edit
            </div>
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={!permission.routes.delete}
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
