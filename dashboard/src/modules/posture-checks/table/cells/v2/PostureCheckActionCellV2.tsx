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
import { PostureCheck } from "@/interfaces/PostureCheck";

// V2 paint of PostureCheckActionCell — single kebab overflow opening
// a dropdown. Edit dispatches the parent's row-click handler so the
// existing edit-modal flow stays put. Delete is gated by policy
// usage (same rule as the legacy cell).

type Props = {
  check: PostureCheck & { policies?: { id?: string }[] };
  onEdit?: () => void;
};

export const PostureCheckActionCellV2 = ({ check, onEdit }: Props) => {
  const { permission } = usePermissions();
  const deleteRequest = useApiCall("/posture-checks");
  const { confirm } = useDialog();
  const { mutate } = useSWRConfig();

  const hasPolicies = check.policies ? check.policies.length > 0 : false;

  const handleDelete = async () => {
    const choice = await confirm({
      title: `Delete '${check.name}'?`,
      description:
        "Are you sure you want to delete this posture check? This action cannot be undone.",
      confirmText: "Delete",
      cancelText: "Cancel",
      type: "danger",
    });
    if (!choice) return;
    notify({
      title: check.name,
      description: "Posture check was successfully deleted",
      promise: deleteRequest.del({}, `/${check.id}`).then(() => {
        mutate("/posture-checks").then();
      }),
      loadingMessage: "Deleting posture check...",
    });
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
            disabled={hasPolicies || !permission.policies.delete}
            onClick={(e) => {
              e.stopPropagation();
              handleDelete();
            }}
            variant="danger"
          >
            <div className="flex items-center gap-3">
              <Trash2 size={14} className="shrink-0" />
              Delete
              {hasPolicies && (
                <span className="ml-auto text-[10.5px] text-oz2-text-faint">
                  in use
                </span>
              )}
            </div>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
};
