"use client";

import classNames from "classnames";
import dayjs, { Dayjs } from "dayjs";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { useMemo, useState } from "react";

// Calendar — v2 range-mode date picker, built from scratch on top of
// dayjs. Replaces the react-day-picker wrapper (whose default CSS
// kept bleeding legacy chrome into the popover) with a thin,
// fully-tokened component. Range mode only — the only mode the
// dashboard consumes via DateRangePickerV2 and DatePickerWithRange.

// Matches react-day-picker's DateRange shape exactly so existing
// consumers (DateRangePickerV2 + DatePickerWithRange) can keep
// importing the type from `react-day-picker` until we eventually
// flip them over to this module.
export interface DateRange {
  from: Date | undefined;
  to?: Date | undefined;
}

interface CalendarProps {
  /** Only "range" is implemented — kept as a discriminator so the
   *  call-site reads identical to the previous react-day-picker
   *  API and we can extend to "single" later if needed. */
  mode?: "range";
  selected?: DateRange;
  onSelect?: (range: DateRange | undefined) => void;
  /** First visible month. Falls back to `selected.from`, then today. */
  defaultMonth?: Date;
  /** Side-by-side months. Defaults to 1. */
  numberOfMonths?: number;
  /** Render days that spill from neighboring months (faded). */
  showOutsideDays?: boolean;
  /** No-op — kept so existing call-sites compile unchanged. */
  initialFocus?: boolean;
  className?: string;
}

const WEEKDAYS = ["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"];

export function Calendar({
  selected,
  onSelect,
  defaultMonth,
  numberOfMonths = 1,
  showOutsideDays = true,
  className,
}: CalendarProps) {
  // First-visible-month anchor. The `viewMonth + viewMonth+1` window
  // slides forward/backward when the chevrons fire.
  const [viewMonth, setViewMonth] = useState<Dayjs>(() =>
    dayjs(defaultMonth ?? selected?.from ?? new Date()).startOf("month"),
  );

  const months = useMemo(
    () =>
      Array.from({ length: numberOfMonths }, (_, i) =>
        viewMonth.add(i, "month"),
      ),
    [viewMonth, numberOfMonths],
  );

  // Range-mode click model:
  //   1st click  -> start a new range { from: clicked, to: undefined }
  //   2nd click  -> complete the range (auto-swap if user clicks
  //                  earlier than the existing start)
  //   3rd click  -> previous range is committed; restart from clicked
  const handleDayClick = (day: Dayjs) => {
    if (!onSelect) return;
    const date = day.startOf("day").toDate();
    const cur = selected;
    if (!cur || !cur.from || (cur.from && cur.to)) {
      onSelect({ from: date, to: undefined });
      return;
    }
    if (cur.from && !cur.to) {
      const curFrom = dayjs(cur.from).startOf("day").toDate();
      if (date < curFrom) {
        onSelect({ from: date, to: curFrom });
      } else {
        onSelect({ from: curFrom, to: dayjs(day).endOf("day").toDate() });
      }
    }
  };

  return (
    <div className={classNames("p-4", className)}>
      {/* `relative` on the months row so the nav-chevrons absolute
          anchor lands on this container (the row that holds both
          month captions + day grids). z-10 keeps them clickable
          above any day cell that paints later in DOM order. */}
      <div className="relative flex flex-col gap-6 sm:flex-row sm:gap-8">
        <div className="absolute right-0 top-0 z-10 flex h-9 items-center gap-1">
          <NavButton
            ariaLabel="Previous month"
            onClick={() => setViewMonth(viewMonth.subtract(1, "month"))}
          >
            <ChevronLeft className="h-4 w-4" strokeWidth={2.5} />
          </NavButton>
          <NavButton
            ariaLabel="Next month"
            onClick={() => setViewMonth(viewMonth.add(1, "month"))}
          >
            <ChevronRight className="h-4 w-4" strokeWidth={2.5} />
          </NavButton>
        </div>
        {months.map((m) => (
          <MonthGrid
            key={m.toISOString()}
            month={m}
            selected={selected}
            onDayClick={handleDayClick}
            showOutsideDays={showOutsideDays}
          />
        ))}
      </div>
    </div>
  );
}

function NavButton({
  ariaLabel,
  onClick,
  children,
}: {
  ariaLabel: string;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-label={ariaLabel}
      onClick={onClick}
      className="grid h-8 w-8 place-items-center rounded-oz2-input border border-transparent bg-transparent text-oz2-text-2 appearance-none transition-colors hover:border-oz2-border hover:bg-oz2-hover hover:text-oz2-text"
    >
      {children}
    </button>
  );
}

interface MonthGridProps {
  month: Dayjs;
  selected?: DateRange;
  onDayClick: (day: Dayjs) => void;
  showOutsideDays: boolean;
}

function MonthGrid({
  month,
  selected,
  onDayClick,
  showOutsideDays,
}: MonthGridProps) {
  // 6×7 = 42 cells covers every possible month layout regardless of
  // the day-of-week the 1st lands on, keeping the popover height
  // stable as the user scrolls months.
  const days = useMemo(() => {
    const startOfMonth = month.startOf("month");
    const startDay = startOfMonth.day();
    const startOfGrid = startOfMonth.subtract(startDay, "day");
    return Array.from({ length: 42 }, (_, i) => startOfGrid.add(i, "day"));
  }, [month]);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex h-9 items-center justify-center pb-1">
        <span className="text-[14px] font-semibold tracking-tight text-oz2-text">
          {month.format("MMMM YYYY")}
        </span>
      </div>
      <div className="flex pb-1">
        {WEEKDAYS.map((w) => (
          <div
            key={w}
            className="w-9 text-center font-mono text-[10px] font-medium uppercase tracking-[0.08em] text-oz2-text-faint"
          >
            {w}
          </div>
        ))}
      </div>
      <div className="flex flex-col gap-0.5">
        {Array.from({ length: 6 }, (_, weekIdx) => (
          <div key={weekIdx} className="flex">
            {days.slice(weekIdx * 7, weekIdx * 7 + 7).map((d) => (
              <DayCell
                key={d.toISOString()}
                day={d}
                isOutside={!d.isSame(month, "month")}
                showOutsideDays={showOutsideDays}
                selected={selected}
                onClick={() => onDayClick(d)}
              />
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}

interface DayCellProps {
  day: Dayjs;
  isOutside: boolean;
  showOutsideDays: boolean;
  selected?: DateRange;
  onClick: () => void;
}

function DayCell({
  day,
  isOutside,
  showOutsideDays,
  selected,
  onClick,
}: DayCellProps) {
  const isToday = day.isSame(dayjs(), "day");
  const from = selected?.from ? dayjs(selected.from) : undefined;
  const to = selected?.to ? dayjs(selected.to) : undefined;

  const isStart = from ? from.isSame(day, "day") : false;
  const isEnd = to ? to.isSame(day, "day") : false;
  const isInRange =
    from && to && day.isAfter(from, "day") && day.isBefore(to, "day");
  const isSelected = isStart || isEnd;

  if (isOutside && !showOutsideDays) {
    return <div className="h-9 w-9" />;
  }

  // Cell-level band: extends across the violet bar between range
  // endpoints. range_start gets left-rounded, range_end right-rounded;
  // single-day selection rounds both sides.
  const cellBg =
    isSelected || isInRange ? "bg-oz2-acc-soft" : "";
  const cellRound = (() => {
    if (isStart && isEnd) return "rounded-oz2-input";
    if (isStart) return "rounded-l-oz2-input";
    if (isEnd) return "rounded-r-oz2-input";
    return "";
  })();

  // Button (the actual clickable pill). 32×32 inside a 36×36 cell
  // leaves a 2px breathing ring of the band visible on each side.
  // `appearance-none bg-transparent border-0` resets the native
  // browser <button> chrome that would otherwise show as a gray
  // rectangle in light mode.
  const buttonBase =
    "mx-auto grid h-8 w-8 place-items-center rounded-full text-[13px] " +
    "appearance-none border-0 cursor-pointer transition-colors";
  let buttonExtra: string;
  if (isSelected) {
    buttonExtra = "bg-oz2-acc text-oz2-text-on-acc font-medium";
  } else if (isInRange) {
    buttonExtra = "bg-transparent text-oz2-acc-text";
  } else if (isOutside) {
    buttonExtra =
      "bg-transparent text-oz2-text-faint opacity-50 hover:bg-oz2-hover";
  } else if (isToday) {
    buttonExtra = "bg-transparent text-oz2-acc font-semibold hover:bg-oz2-hover";
  } else {
    buttonExtra = "bg-transparent text-oz2-text hover:bg-oz2-hover";
  }

  return (
    <div className={classNames("relative h-9 w-9 p-0", cellBg, cellRound)}>
      <button
        type="button"
        onClick={onClick}
        className={classNames(buttonBase, buttonExtra)}
      >
        {day.date()}
      </button>
    </div>
  );
}
