import { TabContext, useTabContext } from "@components/Tabs";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@radix-ui/react-tabs";
import { cn } from "@utils/helpers";
import React from "react";

// SegmentedTabs — v2 paint. The shape is preserved: <SegmentedTabs>,
// <SegmentedTabs.List>, <SegmentedTabs.Trigger>, <SegmentedTabs.Content>.
// Only the tokens change: nb-gray legacy classes swap for the oz2-*
// palette so this matches the rest of the v2 chrome (handoff Forms.html
// segmented control + screens-1 peer-tab card).

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
        "flex justify-center gap-1 rounded-t-oz2-card border border-b-0 border-oz2-border bg-oz2-bg-sunken p-1.5",
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
  const active = value === currentValue;
  return (
    <TabsTrigger
      disabled={disabled}
      className={cn(
        "w-full rounded-oz2-input px-4 py-2 text-[13px] font-medium transition-colors",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-oz2-acc/40",
        "data-[disabled]:opacity-40",
        active
          ? "bg-oz2-surface text-oz2-text shadow-oz2-sm"
          : disabled
            ? "text-oz2-text-faint"
            : "text-oz2-text-muted hover:bg-oz2-surface/60 hover:text-oz2-text",
      )}
      value={value}
    >
      <div className={"flex w-full items-center justify-center gap-2"}>
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
        "mt-0 rounded-b-oz2-card border border-t-0 border-oz2-border bg-oz2-bg-sunken/60 px-4 pb-5 pt-2",
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
