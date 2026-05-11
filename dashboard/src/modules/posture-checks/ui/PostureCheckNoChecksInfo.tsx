import { IconCirclePlus } from "@tabler/icons-react";
import useFetchApi from "@utils/api";
import { FolderSearch } from "lucide-react";
import * as React from "react";
import OzButton from "@/components/v2/OzButton";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { PostureCheck } from "@/interfaces/PostureCheck";

// PostureCheckNoChecksInfo — empty state shown when no posture checks
// are attached to the policy yet. v2 paint: drops the legacy Button +
// Paragraph in favor of OzButton sm + sans-text muted body.

export function PostureCheckNoChecksInfo({
  onAddClick,
  onBrowseClick,
}: {
  onAddClick: () => void;
  onBrowseClick: () => void;
}) {
  const { permission } = usePermissions();
  const { data: postureChecks } =
    useFetchApi<PostureCheck[]>("/posture-checks");

  const canManage =
    permission.policies.create && permission.policies.update;
  const canBrowse =
    canManage && postureChecks && postureChecks.length > 0;

  return (
    <div className="rounded-oz2-card border border-dashed border-oz2-border bg-oz2-bg-sunken/30 px-6 py-8 text-center">
      <h2 className="text-[14px] font-semibold text-oz2-text">
        No posture checks attached
      </h2>
      <p className="mx-auto mt-1.5 max-w-md text-[12.5px] leading-[1.55] text-oz2-text-muted">
        Add posture checks to further restrict access — e.g. require a
        specific Openzro client version, operating system, or geofence.
      </p>
      <div className="mt-5 flex items-center justify-center gap-2">
        <OzButton
          variant="default"
          onClick={onBrowseClick}
          disabled={!canBrowse}
        >
          <FolderSearch size={13} />
          Browse Checks
        </OzButton>
        <OzButton
          variant="primary"
          onClick={onAddClick}
          disabled={!canManage}
        >
          <IconCirclePlus size={13} />
          New Posture Check
        </OzButton>
      </div>
    </div>
  );
}
