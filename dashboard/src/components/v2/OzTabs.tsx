"use client";

import * as TabsPrimitive from "@radix-ui/react-tabs";
import classNames from "classnames";
import * as React from "react";

// OzTabs — v2 paint over Radix Tabs primitives. Drop-in API
// (Root / List / Trigger / Content) with v2 tokens applied: the bar
// uses a hairline bottom-border separator and each trigger gets a
// 2px accent underline when active, mirroring the handoff
// PeerDetailScreen tab visual.
//
// Legacy components/Tabs.tsx (10+ consumers) stays untouched. v2
// pages use this primitive directly.

const OzTabs = TabsPrimitive.Root;

const OzTabsList = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.List>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.List>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.List
    ref={ref}
    className={classNames(
      "inline-flex items-center gap-1 border-b border-oz2-border-soft",
      className,
    )}
    {...props}
  />
));
OzTabsList.displayName = TabsPrimitive.List.displayName;

const OzTabsTrigger = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Trigger
    ref={ref}
    className={classNames(
      "-mb-px inline-flex items-center gap-2 whitespace-nowrap border-b-2 border-transparent px-3.5 py-2.5 text-[13px] font-medium text-oz2-text-muted transition-colors",
      "hover:text-oz2-text",
      "focus-visible:outline-none focus-visible:text-oz2-text",
      "disabled:pointer-events-none disabled:opacity-50",
      "data-[state=active]:border-oz2-acc data-[state=active]:text-oz2-text data-[state=active]:font-semibold",
      className,
    )}
    {...props}
  />
));
OzTabsTrigger.displayName = TabsPrimitive.Trigger.displayName;

const OzTabsContent = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Content
    ref={ref}
    className={classNames(
      "focus-visible:outline-none",
      className,
    )}
    {...props}
  />
));
OzTabsContent.displayName = TabsPrimitive.Content.displayName;

export { OzTabs, OzTabsContent, OzTabsList, OzTabsTrigger };
