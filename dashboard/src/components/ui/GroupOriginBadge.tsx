import { cn } from "@utils/helpers";
import { GroupIssued } from "@/interfaces/Group";
import {
  groupIssued,
  groupIssuedDescription,
  groupIssuedLabel,
  type GroupOrigin,
  unknownGroupIssued,
} from "@/modules/groups/groupIdentity";

type Props = {
  issued?: GroupIssued;
  className?: string;
};

const variants: Record<GroupOrigin, string> = {
  [GroupIssued.API]:
    "border-neutral-300 bg-neutral-100 text-neutral-600 dark:border-nb-gray-700 dark:bg-nb-gray-900 dark:text-nb-gray-300",
  [GroupIssued.JWT]:
    "border-violet-200 bg-violet-50 text-violet-700 dark:border-violet-800 dark:bg-violet-950/30 dark:text-violet-200",
  [GroupIssued.INTEGRATION]:
    "border-violet-300 bg-violet-100 text-violet-800 dark:border-violet-700 dark:bg-violet-950/50 dark:text-violet-200",
  [unknownGroupIssued]:
    "border-neutral-400 bg-neutral-50 text-neutral-800 dark:border-nb-gray-600 dark:bg-nb-gray-900 dark:text-nb-gray-200",
};

export default function GroupOriginBadge({ issued, className }: Props) {
  const origin = groupIssued(issued);

  return (
    <span
      aria-label={groupIssuedDescription(origin)}
      title={groupIssuedDescription(origin)}
      className={cn(
        "inline-flex shrink-0 items-center rounded-[4px] border px-1 py-[1px] text-[9px] font-semibold leading-none",
        variants[origin],
        className,
      )}
    >
      {groupIssuedLabel(origin)}
    </span>
  );
}
