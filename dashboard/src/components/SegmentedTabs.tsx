import { TabContext, useTabContext } from "@components/Tabs";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@radix-ui/react-tabs";
import { cn } from "@utils/helpers";
import React from "react";

type Props = {
  value?: string;
  onChange?: (value: string) => void;
  children: React.ReactNode;
};
function SegmentedTabs({ value, onChange, children }: Props) {
  return (
    <TabContext.Provider value={value || ""}>
      <Tabs
        onValueChange={(value) => onChange && onChange(value)}
        value={value}
      >
        {children}
      </Tabs>
    </TabContext.Provider>
  );
}

function List({
  children,
  className = "",
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <TabsList
      className={cn(
        "p-1.5 rounded-t-lg flex justify-center gap-1 border border-b-0",
        "bg-neutral-100 border-neutral-200",
        "dark:bg-nb-gray-930/70 dark:border-nb-gray-900",
        className,
      )}
    >
      {children}
    </TabsList>
  );
}

function Trigger({
  children,
  value,
  disabled = false,
}: {
  children: React.ReactNode;
  value: string;
  disabled?: boolean;
}) {
  const currentValue = useTabContext();
  return (
    <TabsTrigger
      disabled={disabled}
      className={cn(
        "px-4 py-2 text-sm rounded-md w-full transition-all data-[disabled]:opacity-10",
        value == currentValue
          ? "bg-white text-neutral-900 shadow-sm dark:bg-nb-gray-900 dark:text-white dark:shadow-none"
          : disabled
          ? ""
          : "text-neutral-600 hover:bg-white/70 dark:text-nb-gray-400 dark:hover:bg-nb-gray-900/50",
      )}
      value={value}
    >
      <div className={"flex items-center w-full justify-center gap-2"}>
        {children}
      </div>
    </TabsTrigger>
  );
}

function Content({
  children,
  value,
}: {
  children: React.ReactNode;
  value: string;
}) {
  return (
    <TabsContent
      value={value}
      className={cn(
        "px-4 pt-2 pb-5 rounded-b-md mt-0 border border-t-0",
        "bg-neutral-50 border-neutral-200",
        "dark:bg-nb-gray-930/70 dark:border-nb-gray-900",
      )}
    >
      {children}
    </TabsContent>
  );
}

SegmentedTabs.List = List;
SegmentedTabs.Trigger = Trigger;
SegmentedTabs.Content = Content;

export { SegmentedTabs };
