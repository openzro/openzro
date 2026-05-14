"use client";

import { notify } from "@components/Notification";
import { RowSelectionState } from "@tanstack/react-table";
import { useApiCall } from "@utils/api";
import { Trash2, X } from "lucide-react";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import { useDialog } from "@/contexts/DialogProvider";

// GroupsBulkActionsV2 — inline bulk-action bar at the top of the
// /team/groups table card. Mirrors the PeerBulkActionsV2 shape but
// only carries the delete action: the only useful bulk operation on
// groups today is fan-out delete of unused groups. The selection is
// gated upstream (rows whose group can't be deleted aren't selectable
// to begin with), so this component trusts that every id in the
// rowSelection map is a valid delete target.

interface Props {
  selectedIds: RowSelectionState;
  onCanceled: () => void;
}

export default function GroupsBulkActionsV2({
  selectedIds,
  onCanceled,
}: Props) {
  const { mutate } = useSWRConfig();
  const { confirm } = useDialog();
  // Single useApiCall handle — `del(suffix)` lets us reuse it across
  // the per-id requests instead of building one hook per row.
  const groupsApi = useApiCall("/groups");
  const [running, setRunning] = useState(false);

  const ids = useMemo(() => Object.keys(selectedIds), [selectedIds]);
  const count = ids.length;
  if (count === 0) return null;

  const handleConfirm = async () => {
    const choice = await confirm({
      title: count === 1 ? "Delete 1 group?" : `Delete ${count} groups?`,
      description:
        "Selected groups will be deleted permanently. Any references already in use would have blocked the selection — these are safe to remove.",
      confirmText: "Delete",
      cancelText: "Cancel",
      type: "danger",
    });
    if (!choice) return;

    setRunning(true);
    // Backend has no bulk endpoint; fan out N DELETEs. Promise.allSettled
    // so a single failed row doesn't strand the rest of the batch in
    // an unknown state — we report aggregated success / failure to the
    // user once everything has resolved.
    const results = await Promise.allSettled(
      ids.map((id) => groupsApi.del("", `/${id}`)),
    );
    const failed = results.filter((r) => r.status === "rejected").length;
    const ok = count - failed;

    if (failed === 0) {
      notify({
        title: "Groups",
        description:
          ok === 1
            ? "1 group was deleted."
            : `${ok} groups were deleted.`,
      });
    } else {
      notify({
        title: "Groups",
        description: `${ok} deleted, ${failed} failed. Check the rows still selected for details.`,
      });
    }
    await mutate("/groups");
    setRunning(false);
    onCanceled();
  };

  return (
    <div
      role="region"
      aria-label="Bulk actions"
      className="flex items-center gap-3 border-b border-oz2-border-soft bg-oz2-bg-sunken px-[18px] py-2.5 text-[13.5px]"
    >
      <span className="font-medium text-oz2-text">
        {count} selected
      </span>
      <button
        type="button"
        onClick={handleConfirm}
        disabled={running}
        className="inline-flex h-8 items-center gap-1.5 rounded-oz2-input border border-oz2-err-bg bg-oz2-err-bg px-3 text-[13px] font-medium text-oz2-err transition-colors hover:bg-oz2-err hover:text-white disabled:cursor-not-allowed disabled:opacity-50"
      >
        <Trash2 size={14} />
        {running ? "Deleting…" : "Delete selected"}
      </button>
      <button
        type="button"
        onClick={onCanceled}
        aria-label="Clear selection"
        className="ml-auto grid h-7 w-7 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 transition-colors hover:bg-oz2-hover hover:border-oz2-border-strong"
      >
        <X size={14} />
      </button>
    </div>
  );
}
