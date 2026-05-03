import { cn } from "@utils/helpers";
import React from "react";
import { useApplicationContext } from "@/contexts/ApplicationProvider";

type Props = {
  children: React.ReactNode;
  className?: string;
};
export default function PageContainer({
  children,
  className,
}: Readonly<Props>) {
  const { isNavigationCollapsed } = useApplicationContext();
  return (
    <div
      className={cn(
        className,
        // bg-nb-gray DEFAULT resolves via globals.css `--nb-gray-DEFAULT`
        // → `#0F0A1F` (deep violet ink) on dark, `#F1F1F4` (cool white)
        // on light. The light-mode value reads as a violet tint on some
        // calibrations; pin pure white for the page surface in light
        // mode and keep the brand ink only on dark.
        "relative flex-auto overflow-auto bg-white dark:bg-nb-gray z-1 focus:outline-none",
        isNavigationCollapsed && "md:pl-[70px]",
      )}
    >
      {children}
    </div>
  );
}
