"use client";

import "react-day-picker/dist/style.css";
import { buttonVariants } from "@components/ui/CalendarButton";
import { cn } from "@utils/helpers";
import { ChevronLeft, ChevronRight } from "lucide-react";
import * as React from "react";
import { DayPicker } from "react-day-picker";

export type CalendarProps = React.ComponentProps<typeof DayPicker>;

// Calendar — wraps react-day-picker v9 with the openZro brand
// classes. v9 renamed most className slots vs v8 (caption →
// month_caption, head_row → weekdays, day_selected → selected,
// nav_button_* → previous/next button, IconLeft/IconRight →
// Chevron component). The override map below targets the v9 names
// only — the v8 keys it replaced silently no-op'd in upgrades.
function Calendar({
  className,
  classNames,
  showOutsideDays = true,
  ...props
}: CalendarProps) {
  return (
    <DayPicker
      showOutsideDays={showOutsideDays}
      className={cn("p-3", className)}
      classNames={{
        months: "flex flex-col sm:flex-row gap-4",
        month: "flex flex-col gap-4",
        month_caption: "flex justify-center pt-1 relative items-center",
        caption_label: "text-sm font-medium text-oz2-text",
        nav: "flex items-center gap-1 absolute right-1 top-1",
        button_previous: cn(
          buttonVariants({ variant: "outline" }),
          "h-7 w-7 bg-transparent p-0 opacity-60 hover:opacity-100",
        ),
        button_next: cn(
          buttonVariants({ variant: "outline" }),
          "h-7 w-7 bg-transparent p-0 opacity-60 hover:opacity-100",
        ),
        month_grid: "w-full border-collapse",
        weekdays: "flex",
        weekday:
          "text-oz2-text-faint rounded-md w-9 font-normal text-[0.8rem]",
        week: "flex w-full mt-2",
        // Range-band uses oz2-acc-soft so the violet tint stays
        // legible in both light and dark mode (the legacy neutral-100
        // band looked black in light mode and disappeared in dark).
        day: "h-9 w-9 text-center text-sm p-0 relative focus-within:relative focus-within:z-20 [&:has([aria-selected])]:bg-oz2-acc-soft [&:has([aria-selected].range-end)]:rounded-r-md first:[&:has([aria-selected])]:rounded-l-md last:[&:has([aria-selected])]:rounded-r-md",
        day_button: cn("h-9 w-9 p-0 font-normal aria-selected:opacity-100"),
        range_end: "range-end rounded-r-md",
        range_start: "range-start rounded-l-md",
        // Endpoints get the solid violet pill (oz2-acc). Hover/focus
        // stay on the same fill so the pill doesn't flash.
        selected:
          "bg-oz2-acc text-oz2-text-on-acc hover:bg-oz2-acc hover:text-oz2-text-on-acc focus:bg-oz2-acc focus:text-oz2-text-on-acc",
        today:
          "text-oz2-acc font-semibold aria-selected:text-oz2-text-on-acc",
        outside:
          "outside text-oz2-text-faint opacity-50 aria-selected:bg-oz2-acc-soft/50 aria-selected:text-oz2-text-2 aria-selected:opacity-40",
        disabled: "text-oz2-text-faint opacity-40",
        range_middle:
          "aria-selected:bg-oz2-acc-soft aria-selected:text-oz2-acc-text rounded-none",
        hidden: "invisible",
        ...classNames,
      }}
      components={{
        // v9 collapsed IconLeft / IconRight into a single Chevron
        // component that receives the orientation as a prop.
        Chevron: ({ orientation, ...rest }) =>
          orientation === "left" ? (
            <ChevronLeft className="h-4 w-4" {...rest} />
          ) : (
            <ChevronRight className="h-4 w-4" {...rest} />
          ),
      }}
      {...props}
    />
  );
}
Calendar.displayName = "Calendar";

export { Calendar };
