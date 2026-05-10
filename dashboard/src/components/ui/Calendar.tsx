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
        caption_label: "text-sm font-medium",
        nav: "flex items-center gap-1 absolute right-1 top-1",
        button_previous: cn(
          buttonVariants({ variant: "outline" }),
          "h-7 w-7 bg-transparent p-0 opacity-50 hover:opacity-100",
        ),
        button_next: cn(
          buttonVariants({ variant: "outline" }),
          "h-7 w-7 bg-transparent p-0 opacity-50 hover:opacity-100",
        ),
        month_grid: "w-full border-collapse",
        weekdays: "flex",
        weekday:
          "text-neutral-500 rounded-md w-9 font-normal text-[0.8rem] dark:text-neutral-400",
        week: "flex w-full mt-2",
        day: "h-9 w-9 text-center text-sm p-0 relative focus-within:relative focus-within:z-20 [&:has([aria-selected])]:bg-neutral-100 dark:[&:has([aria-selected])]:bg-neutral-800 [&:has([aria-selected].range-end)]:rounded-r-md first:[&:has([aria-selected])]:rounded-l-md last:[&:has([aria-selected])]:rounded-r-md",
        day_button: cn("h-9 w-9 p-0 font-normal aria-selected:opacity-100"),
        range_end: "range-end rounded-r-md",
        range_start: "range-start rounded-l-md",
        selected:
          "bg-neutral-900 text-neutral-50 hover:bg-neutral-900 hover:text-neutral-50 focus:bg-neutral-900 focus:text-neutral-50 dark:bg-neutral-50 dark:text-neutral-900 dark:hover:bg-neutral-50 dark:hover:text-neutral-900 dark:focus:bg-neutral-50 dark:focus:text-neutral-900",
        today: "text-neutral-900 dark:text-red-500",
        outside:
          "outside text-neutral-500 opacity-50 aria-selected:bg-neutral-100/50 aria-selected:text-neutral-500 aria-selected:opacity-30 dark:text-neutral-400 dark:aria-selected:bg-neutral-800/50 dark:aria-selected:text-neutral-400",
        disabled: "text-neutral-500 opacity-50 dark:text-neutral-400",
        range_middle:
          "aria-selected:bg-neutral-100 aria-selected:text-neutral-900 dark:aria-selected:bg-nb-gray-800 dark:aria-selected:text-neutral-50 rounded-none",
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
