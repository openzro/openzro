"use client";

import useFetchApi from "@utils/api";
import classNames from "classnames";
import { Bot, FolderGit2, User2 } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import React from "react";
import { useGroups } from "@/contexts/GroupsProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { User } from "@/interfaces/User";

// TeamTabs — page-header sub-navigation across the three /team/*
// list screens. Sidebar exposes a single "Users & Groups" nav item
// that lands on /team/users; this segmented control lets the
// operator pivot between Users / Service Users / Groups without
// a sidebar round-trip. Used at the top of each list page right
// under the page-level header.
//
// Each tab carries a count badge (handoff TeamScreen TabBar) so the
// operator can size up the workspace at a glance. Counts are pulled
// from the same SWR endpoints the per-tab tables fetch, so the cache
// is hot whenever you're already on one of those pages — Service
// Users requires an extra request when the operator is on Users or
// Groups, but it's a single small list and SWR dedupes repeats.
//
// The /team/user (singular) detail screen is intentionally NOT
// represented here — tabs are for sibling list views, not for
// drill-down detail.

interface TeamTabDef {
  id: string;
  label: string;
  icon: React.ReactNode;
  href: string;
  /**
   * Pathname matcher. Active when current pathname equals href OR
   * starts with `${href}/` (so future sub-paths still highlight).
   */
  match: (path: string | null) => boolean;
  visible: boolean;
  count?: number;
}

export default function TeamTabs() {
  const pathname = usePathname();
  const { permission } = usePermissions();

  // Counts. Each fetch is gated on the corresponding read permission
  // so we never trigger a 403 just to render a badge for an operator
  // who can't see the underlying list anyway.
  const { data: users } = useFetchApi<User[]>(
    "/users?service_user=false",
    true,
    true,
    permission.users.read,
  );
  const { data: serviceUsers } = useFetchApi<User[]>(
    "/users?service_user=true",
    true,
    true,
    permission.users.read,
  );
  const { groups } = useGroups();

  // Iconography:
  //   Users         → User2     (single human silhouette)
  //   Service Users → Bot       (automation / API actor)
  //   Groups        → FolderGit2 (matches the legacy NoResults icon)
  const tabs: TeamTabDef[] = [
    {
      id: "users",
      label: "Users",
      icon: <User2 size={14} />,
      href: "/team/users",
      match: (p) => p === "/team/users" || p === "/team",
      visible: permission.users.read,
      count: users?.length,
    },
    {
      id: "service",
      label: "Service Users",
      icon: <Bot size={14} />,
      href: "/team/service-users",
      // /team/service-users keeps legacy paint until that body is
      // ported; the tab still navigates there so operators can reach
      // it without falling back to the URL bar.
      match: (p) => p === "/team/service-users",
      visible: permission.users.read,
      count: serviceUsers?.length,
    },
    {
      id: "groups",
      label: "Groups",
      icon: <FolderGit2 size={14} />,
      href: "/team/groups",
      match: (p) => p === "/team/groups",
      visible: permission.groups.read,
      count: groups?.length,
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
            <span
              aria-hidden
              className="inline-flex h-3.5 w-3.5 shrink-0 items-center justify-center"
            >
              {tab.icon}
            </span>
            {tab.label}
            {typeof tab.count === "number" && (
              // Count badge — handoff TabBar pattern. Active tab gets
              // the violet `acc-soft` fill; inactive tabs get the
              // sunken neutral fill so the badge reads as a chip.
              <span
                className={classNames(
                  "ml-0.5 inline-flex min-w-[20px] justify-center rounded-full px-1.5 py-0.5 font-mono text-[10.5px] font-medium tabular-nums",
                  active
                    ? "bg-oz2-acc-soft text-oz2-acc-text"
                    : "bg-oz2-bg-elev text-oz2-text-faint",
                )}
              >
                {tab.count}
              </span>
            )}
          </Link>
        );
      })}
    </nav>
  );
}
