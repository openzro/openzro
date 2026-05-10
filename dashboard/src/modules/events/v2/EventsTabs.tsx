"use client";

import classNames from "classnames";
import { LogsIcon, NetworkIcon } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";

// EventsTabs — page-header sub-navigation across the two /events/*
// surfaces. Sidebar exposes a single "Activity" nav item that lands
// on /events/network-traffic; this segmented control lets the
// operator pivot between Audit Events / Network Traffic without a
// sidebar round-trip. Mirrors TeamTabs in structure (active match,
// permission gate, segmented-control treatment).
//
// Counts are deliberately omitted for now — both endpoints would
// require an extra HTTP request just for the badge, and Network
// Traffic in particular paginates server-side so a "total" is
// expensive. Revisit if the user explicitly asks for badges.

interface EventsTabDef {
  id: string;
  label: string;
  icon: React.ReactNode;
  href: string;
  match: (path: string | null) => boolean;
  visible: boolean;
}

export default function EventsTabs() {
  const pathname = usePathname();
  const { permission } = usePermissions();

  const tabs: EventsTabDef[] = [
    {
      id: "audit",
      label: "Audit Events",
      icon: <LogsIcon size={14} />,
      href: "/events/audit",
      match: (p) => p === "/events/audit" || p === "/events",
      visible: permission.events.read,
    },
    {
      id: "network-traffic",
      label: "Network Traffic",
      icon: <NetworkIcon size={14} />,
      href: "/events/network-traffic",
      match: (p) => p === "/events/network-traffic",
      visible: permission.events.read,
    },
  ];

  const visibleTabs = tabs.filter((t) => t.visible);
  if (visibleTabs.length <= 1) return null;

  return (
    <nav
      role="tablist"
      aria-label="Activity sub-navigation"
      className="inline-flex h-[34px] items-center rounded-oz2-input bg-oz2-bg-sunken p-1 text-oz2-text-muted"
    >
      {visibleTabs.map((tab) => {
        const active = tab.match(pathname);
        return (
          <Link
            key={tab.href}
            href={tab.href}
            role="tab"
            aria-selected={active}
            className={classNames(
              "inline-flex h-full items-center gap-2 whitespace-nowrap rounded-[6px] px-3 text-[13.5px] font-medium transition-colors",
              active
                ? "bg-oz2-surface text-oz2-text shadow-oz2-sm"
                : "hover:text-oz2-text",
            )}
          >
            <span
              aria-hidden
              className="inline-flex h-3.5 w-3.5 shrink-0 items-center justify-center"
            >
              {tab.icon}
            </span>
            {tab.label}
          </Link>
        );
      })}
    </nav>
  );
}
