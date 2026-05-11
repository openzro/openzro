import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@components/DropdownMenu";
import {
  Edit,
  FolderSearch,
  MinusCircleIcon,
  MoreVertical,
  PlusCircle,
} from "lucide-react";
import React from "react";
import OzButton from "@/components/v2/OzButton";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { PostureCheck } from "@/interfaces/PostureCheck";
import { PostureCheckChecksCell } from "@/modules/posture-checks/table/cells/PostureCheckChecksCell";
import { PostureCheckNameCell } from "@/modules/posture-checks/table/cells/PostureCheckNameCell";
import { PostureCheckNoChecksInfo } from "@/modules/posture-checks/ui/PostureCheckNoChecksInfo";

// PostureCheckMinimalTable — v2 paint. Renders the list of posture
// checks attached to a policy with a kebab per row. The legacy
// version carried its own header (Label + HelpText + two large
// buttons); now that this lives inside an OzCard with its own header
// banner, the per-table header collapses to the action buttons only.

type Props = {
  data: PostureCheck[];
  onAddClick: () => void;
  onBrowseClick: () => void;
  onRemoveClick: (check: PostureCheck) => void;
  onEditClick: (check: PostureCheck) => void;
};

export default function PostureCheckMinimalTable({
  data,
  onAddClick,
  onBrowseClick,
  onRemoveClick,
  onEditClick,
}: Props) {
  const { permission } = usePermissions();
  const canManage =
    permission.policies.update || permission.policies.create;

  if (!data || data.length === 0) {
    return (
      <PostureCheckNoChecksInfo
        onAddClick={onAddClick}
        onBrowseClick={onBrowseClick}
      />
    );
  }

  return (
    <div>
      <div className="mb-3 flex items-center justify-end gap-2">
        <OzButton
          variant="default"
          onClick={onBrowseClick}
          disabled={!canManage}
        >
          <FolderSearch size={13} />
          Browse Checks
        </OzButton>
        <OzButton
          variant="primary"
          onClick={onAddClick}
          disabled={!canManage}
        >
          <PlusCircle size={13} />
          New Posture Check
        </OzButton>
      </div>

      <div className="overflow-hidden rounded-oz2-card border border-oz2-border-soft bg-oz2-bg-sunken/40">
        {data.map((check, i) => (
          <div
            key={check.id}
            className={
              "flex cursor-pointer items-center justify-between gap-3 px-4 py-2 transition-colors hover:bg-oz2-hover" +
              (i === 0 ? "" : " border-t border-oz2-border-soft")
            }
            onClick={() => canManage && onEditClick(check)}
          >
            <PostureCheckNameCell small check={check} />
            <div className="flex shrink-0 items-center gap-3">
              <PostureCheckChecksCell check={check} />
              {canManage && (
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
                  <DropdownMenuContent align="end" className="w-52">
                    <DropdownMenuItem
                      onClick={() => onEditClick(check)}
                      disabled={!permission.policies.update}
                    >
                      <div className="flex items-center gap-3">
                        <Edit size={14} className="shrink-0" />
                        Edit Posture Check
                      </div>
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      onClick={() => onRemoveClick(check)}
                      disabled={!permission.policies.delete}
                      variant="danger"
                    >
                      <div className="flex items-center gap-3">
                        <MinusCircleIcon size={14} className="shrink-0" />
                        Remove Posture Check
                      </div>
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
