import { cn } from "@utils/helpers";
import { GroupIssued } from "@/interfaces/Group";
import {
  groupIssued,
  groupIssuedDescription,
  groupIssuedLabel,
} from "@/modules/groups/groupIdentity";

type Props = {
  issued?: GroupIssued;
  className?: string;
};

const variants: Record<GroupIssued, string> = {
  [GroupIssued.API]:
    "border-neutral-300 bg-neutral-100 text-neutral-600 dark:border-nb-gray-700 dark:bg-nb-gray-900 dark:text-nb-gray-300",
  [GroupIssued.JWT]:
    "border-sky-300 bg-sky-100 text-sky-700 dark:border-sky-700 dark:bg-sky-950/50 dark:text-sky-300",
  [GroupIssued.INTEGRATION]:
    "border-violet-300 bg-violet-100 text-violet-700 dark:border-violet-700 dark:bg-violet-950/50 dark:text-violet-300",
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
