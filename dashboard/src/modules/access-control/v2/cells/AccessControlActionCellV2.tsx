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
import { Policy } from "@/interfaces/Policy";
import { Route } from "@/interfaces/Route";

// V2 paint of AccessControlActionCell — single kebab overflow opening
// a dropdown with Edit + Delete. Edit re-fires the parent row-click
// handler so the existing edit-modal flow stays put. Delete logic
// preserved verbatim from the legacy cell.

type Props = {
  policy: Policy;
  onEdit?: () => void;
};

export default function AccessControlActionCellV2({
  policy,
  onEdit,
}: Readonly<Props>) {
  const { confirm } = useDialog();
  const policyRequest = useApiCall<Route>("/policies");
  const { mutate } = useSWRConfig();
  const { permission } = usePermissions();

  const deleteRule = async () => {
    notify({
      title: "Access Control Policy " + policy.name,
      description: "The policy was successfully removed.",
      promise: policyRequest.del("", `/${policy.id}`).then(() => {
        mutate("/policies");
      }),
      loadingMessage: "Deleting the policy...",
    });
  };

  const handleConfirm = async () => {
    const choice = await confirm({
      title: `Delete '${policy.name}'?`,
      description:
        "Are you sure you want to delete this access control policy? This action cannot be undone.",
      confirmText: "Delete",
      cancelText: "Cancel",
      type: "danger",
    });
    if (!choice) return;
    deleteRule().then();
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
            disabled={!permission.policies.delete}
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
