"use client";

import classNames from "classnames";
import { Globe, Layers, Settings2 } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";

// DnsTabs — page-header sub-navigation across the two /dns/* sub-
// screens. Sidebar exposes a single "DNS" item that lands on
// /dns/nameservers; this segmented control lets the operator pivot
// between Nameservers and DNS Settings without going back to the
// sidebar. Mirrors TeamTabs / EventsTabs in structure (active match,
// permission gate, segmented-control treatment).

interface DnsTabDef {
  id: string;
  label: string;
  icon: React.ReactNode;
  href: string;
  match: (path: string | null) => boolean;
  visible: boolean;
}

export default function DnsTabs() {
  const pathname = usePathname();
  const { permission } = usePermissions();

  const tabs: DnsTabDef[] = [
    {
      id: "nameservers",
      label: "Nameservers",
      icon: <Globe size={14} />,
      href: "/dns/nameservers",
      match: (p) => p === "/dns/nameservers" || p === "/dns",
      visible: permission.nameservers.read,
    },
    {
      id: "zones",
      label: "DNS Zones",
      icon: <Layers size={14} />,
      href: "/dns/zones",
      match: (p) => p === "/dns/zones",
      visible: permission.dns_zones.read,
    },
    {
      id: "settings",
      label: "DNS Settings",
      icon: <Settings2 size={14} />,
      href: "/dns/settings",
      match: (p) => p === "/dns/settings",
      visible: permission.dns.read,
    },
  ];

  const visibleTabs = tabs.filter((t) => t.visible);
  if (visibleTabs.length <= 1) return null;

  return (
    <nav
      role="tablist"
      aria-label="DNS sub-navigation"
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
