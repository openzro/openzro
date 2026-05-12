"use client";

import * as TabsPrimitive from "@radix-ui/react-tabs";
import classNames from "classnames";
import * as React from "react";

// OzTabs — v2 paint over Radix Tabs primitives, drop-in API
// (Root / List / Trigger / Content). Renders as the segmented pill
// control used everywhere else in the v2 dashboard (TeamTabs,
// DnsTabs, SettingsTabsV2): 34px-tall sunken track with the active
// trigger lifting onto the surface tone. Used in /peer detail and
// any other v2 page that needs in-component tab state.
//
// Legacy components/Tabs.tsx (10+ consumers across modals + posture
// checks) stays untouched.

const OzTabs = TabsPrimitive.Root;

const OzTabsList = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.List>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.List>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.List
    ref={ref}
    className={classNames(
      "inline-flex h-[34px] items-center rounded-oz2-input bg-oz2-bg-sunken p-1 text-oz2-text-muted",
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
      // `group/trigger` exposes the named group so child icons can
      // swap fill / color on the active tab via
      // `group-data-[state=active]/trigger:*` selectors — consumers
      // like RouteModal, PostureCheckModal, AccessControlModal use
      // this to tint OS / route-type / posture-section icons.
      "group/trigger inline-flex h-full items-center gap-2 whitespace-nowrap rounded-[6px] px-3 text-[13.5px] font-medium transition-colors",
      "hover:text-oz2-text",
      "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-oz2-acc/30",
      "disabled:pointer-events-none disabled:opacity-50",
      "data-[state=active]:bg-oz2-surface data-[state=active]:text-oz2-text data-[state=active]:shadow-oz2-sm",
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
