import FullTooltip from "@components/FullTooltip";
import * as React from "react";
import { ScheduleCheck, weekdayLabels } from "@/interfaces/PostureCheck";

type Props = {
  check?: ScheduleCheck;
  children: React.ReactNode;
};

export const ScheduleTooltip = ({ check, children }: Props) => {
  if (!check) return <>{children}</>;

  const verb = check.action === "allow" ? "Allow" : "Deny";
  const verbClass =
    check.action === "allow"
      ? "text-green-500 font-semibold"
      : "text-red-500 font-semibold";

  const tz = check.timezone && check.timezone.trim() !== "" ? check.timezone : "UTC";
  const dayText = formatDays(check.window?.days_of_week);
  const wraps = wrapsMidnight(check.window?.start_time, check.window?.end_time);

  return (
    <FullTooltip
      className={"w-full"}
      interactive={false}
      content={
        <div className={"text-neutral-300 flex flex-col text-sm gap-1 min-w-[220px]"}>
          <div className={"flex items-center gap-1 flex-wrap"}>
            <span className={verbClass}>{verb}</span>
            <span>{dayText}</span>
            <span className={"font-mono text-[12px]"}>
              {check.window?.start_time}–{check.window?.end_time}
            </span>
            <span className={"text-neutral-400 text-xs"}>({tz})</span>
          </div>
          {wraps && (
            <div className={"text-[11.5px] text-neutral-400"}>
              (wraps midnight)
            </div>
          )}
        </div>
      }
    >
      {children}
    </FullTooltip>
  );
};

const formatDays = (days?: number[]): string => {
  if (!days || days.length === 0 || days.length === 7) return "every day";
  const unique = Array.from(new Set(days)).sort((a, b) => a - b);
  return unique
    .map((d) => weekdayLabels.find((w) => w.value === d)?.short ?? String(d))
    .join(", ");
};

const wrapsMidnight = (start?: string, end?: string): boolean => {
  if (!start || !end) return false;
  const [sh, sm] = start.split(":").map(Number);
  const [eh, em] = end.split(":").map(Number);
  if ([sh, sm, eh, em].some((n) => Number.isNaN(n))) return false;
  return eh * 60 + em <= sh * 60 + sm;
};
