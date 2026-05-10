"use client";

import classNames from "classnames";
import { Bot, FolderGit2, User2 } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";

// TeamTabs — page-header sub-navigation across the three /team/*
// list screens. Sidebar exposes a single "Users & Groups" nav item
// that lands on /team/users; this segmented control lets the
// operator pivot between Users / Service Users / Groups without
// a sidebar round-trip. Used at the top of each list page right
// under the page-level header.
//
// The /team/user (singular) detail screen is intentionally NOT
// represented here — tabs are for sibling list views, not for
// drill-down detail.

interface TeamTabDef {
  label: string;
  icon: React.ReactNode;
  href: string;
  /**
   * Pathname matcher. Active when current pathname equals href OR
   * starts with `${href}/` (so future sub-paths still highlight).
   */
  match: (path: string | null) => boolean;
  visible: boolean;
}

export default function TeamTabs() {
  const pathname = usePathname();
  const { permission } = usePermissions();

  // Iconography:
  //   Users         → User2     (single human silhouette)
  //   Service Users → Bot       (automation / API actor)
  //   Groups        → FolderGit2 (matches the legacy NoResults icon)
  const tabs: TeamTabDef[] = [
    {
      label: "Users",
      icon: <User2 size={14} />,
      href: "/team/users",
      match: (p) => p === "/team/users" || p === "/team",
      visible: permission.users.read,
    },
    {
      label: "Service Users",
      icon: <Bot size={14} />,
      href: "/team/service-users",
      // /team/service-users keeps legacy paint until that body is
      // ported; the tab still navigates there so operators can reach
      // it without falling back to the URL bar.
      match: (p) => p === "/team/service-users",
      visible: permission.users.read,
    },
    {
      label: "Groups",
      icon: <FolderGit2 size={14} />,
      href: "/team/groups",
      match: (p) => p === "/team/groups",
      visible: permission.groups.read,
    },
  ];

  const visibleTabs = tabs.filter((t) => t.visible);
  if (visibleTabs.length <= 1) return null;

  return (
    <nav
      role="tablist"
      aria-label="Identity sub-navigation"
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
            <span aria-hidden className="inline-flex h-3.5 w-3.5 shrink-0 items-center justify-center">
              {tab.icon}
            </span>
            {tab.label}
          </Link>
        );
      })}
    </nav>
  );
}
