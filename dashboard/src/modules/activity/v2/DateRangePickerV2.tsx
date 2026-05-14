"use client";

import { Popover, PopoverContent, PopoverTrigger } from "@components/Popover";
import { AbsoluteDateTimeInput } from "@components/ui/AbsoluteDateTimeInput";
import { Calendar } from "@components/ui/Calendar";
import { cn } from "@utils/helpers";
import dayjs from "dayjs";
import { debounce } from "lodash";
import { Calendar as CalendarIcon, ChevronDown } from "lucide-react";
import React, { useMemo, useState } from "react";
import { DateRange } from "@components/ui/Calendar";

// DateRangePickerV2 — handoff-flavored repaint of the legacy
// DatePickerWithRange. Same calendar + presets popover under the
// hood (Calendar, AbsoluteDateTimeInput, the seven preset ranges);
// only the trigger button changes — it now renders as a 32px-tall
// v2 outline button matching the rest of the AuditTimelineV2
// toolbar (search input, refresh button, etc.).

interface Props {
  value?: DateRange;
  onChange?: (range: DateRange | undefined) => void;
  className?: string;
}

const defaultRanges = {
  today: {
    from: dayjs().startOf("day").toDate(),
    to: dayjs().endOf("day").toDate(),
  },
  yesterday: {
    from: dayjs().subtract(1, "day").startOf("day").toDate(),
    to: dayjs().subtract(1, "day").endOf("day").toDate(),
  },
  last14Days: {
    from: dayjs().subtract(14, "day").startOf("day").toDate(),
    to: dayjs().endOf("day").toDate(),
  },
  last7Days: {
    from: dayjs().subtract(7, "day").startOf("day").toDate(),
    to: dayjs().endOf("day").toDate(),
  },
  lastMonth: {
    from: dayjs().subtract(1, "month").startOf("day").toDate(),
    to: dayjs().endOf("day").toDate(),
  },
  allTime: {
    from: dayjs("1970-01-01").startOf("day").toDate(),
    to: dayjs().endOf("day").toDate(),
  },
};

const isEqualDateRange = (a: DateRange | undefined, b: DateRange) => {
  if (!a) return false;
  const aFromDay = dayjs(a.from).format("YYYY-MM-DD");
  const aToDay = dayjs(a.to).format("YYYY-MM-DD");
  const bFromDay = dayjs(b.from).format("YYYY-MM-DD");
  const bToDay = dayjs(b.to).format("YYYY-MM-DD");
  return aFromDay === bFromDay && aToDay === bToDay;
};

export default function DateRangePickerV2({
  className,
  value,
  onChange,
}: Readonly<Props>) {
  const [open, setOpen] = useState(false);

  const isActive = useMemo(
    () => ({
      today: isEqualDateRange(value, defaultRanges.today),
      yesterday: isEqualDateRange(value, defaultRanges.yesterday),
      last14Days: isEqualDateRange(value, defaultRanges.last14Days),
      last7Days: isEqualDateRange(value, defaultRanges.last7Days),
      lastMonth: isEqualDateRange(value, defaultRanges.lastMonth),
      allTime: isEqualDateRange(value, defaultRanges.allTime),
    }),
    [value],
  );

  const displayDateValue = useMemo(() => {
    if (!value) return "Select range";
    if (isActive.allTime) return "All time";
    if (isActive.lastMonth) return "Last month";
    if (isActive.last14Days) return "Last 14 days";
    if (isActive.last7Days) return "Last 7 days";
    if (isActive.yesterday) return "Yesterday";
    if (isActive.today) return "Today";
    if (!value.to) return dayjs(value.from).format("MMM DD, YYYY");
    return `${dayjs(value.from).format("MMM DD")} – ${dayjs(value.to).format(
      "MMM DD, YYYY",
    )}`;
  }, [value, isActive]);

  const debouncedOnChange = useMemo(
    () => (onChange ? debounce(onChange, 500) : undefined),
    [onChange],
  );

  const handleOnSelect = (range?: DateRange) => {
    const from = range?.from
      ? dayjs(range.from).startOf("day").toDate()
      : undefined;
    const to = range?.to ? dayjs(range.to).endOf("day").toDate() : undefined;
    if (!from && !to) {
      onChange?.(undefined);
      return;
    }
    onChange?.({ from, to });
  };

  const updateRangeAndClose = (range: DateRange) => onChange?.(range);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={cn(
            "inline-flex h-8 items-center gap-2 whitespace-nowrap rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 text-[13px] font-medium text-oz2-text-2 transition-colors",
            "hover:bg-oz2-hover hover:border-oz2-border-strong",
            className,
          )}
        >
          <CalendarIcon size={13} className="shrink-0 text-oz2-text-faint" />
          <span className="truncate">{displayDateValue}</span>
          <ChevronDown size={13} className="shrink-0 text-oz2-text-faint" />
        </button>
      </PopoverTrigger>
      <PopoverContent
        className="w-auto p-0"
        align="start"
        side="bottom"
        sideOffset={6}
      >
        {/* Preset row — kept identical in behavior, just retoned with
            v2 tokens. The active preset gets the violet acc-soft fill;
            inactive ones use a transparent ghost. */}
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-oz2-border-soft px-3 py-2">
          <PresetBtn
            active={isActive.allTime}
            onClick={() => updateRangeAndClose(defaultRanges.allTime)}
          >
            <CalendarIcon size={12} />
            All time
          </PresetBtn>
          <div className="flex flex-wrap gap-1.5">
            <PresetBtn
              active={isActive.lastMonth}
              onClick={() => updateRangeAndClose(defaultRanges.lastMonth)}
            >
              Last month
            </PresetBtn>
            <PresetBtn
              active={isActive.last14Days}
              onClick={() => updateRangeAndClose(defaultRanges.last14Days)}
            >
              Last 14 days
            </PresetBtn>
            <PresetBtn
              active={isActive.last7Days}
              onClick={() => updateRangeAndClose(defaultRanges.last7Days)}
            >
              Last 7 days
            </PresetBtn>
            <PresetBtn
              active={isActive.yesterday}
              onClick={() => updateRangeAndClose(defaultRanges.yesterday)}
            >
              Yesterday
            </PresetBtn>
            <PresetBtn
              active={isActive.today}
              onClick={() => updateRangeAndClose(defaultRanges.today)}
            >
              Today
            </PresetBtn>
          </div>
        </div>
        <Calendar
          initialFocus
          mode="range"
          defaultMonth={value?.from}
          selected={value}
          onSelect={handleOnSelect}
          numberOfMonths={2}
        />
        <AbsoluteDateTimeInput value={value} onChange={debouncedOnChange} />
      </PopoverContent>
    </Popover>
  );
}

function PresetBtn({
  active,
  onClick,
  children,
}: {
  active?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-[12px] leading-none transition-colors",
        active
          ? "bg-oz2-acc-soft text-oz2-acc-text"
          : "bg-transparent text-oz2-text-2 hover:bg-oz2-hover hover:text-oz2-text",
      )}
    >
      {children}
    </button>
  );
}
