"use client";

import { useTheme } from "next-themes";
import Image from "next/image";
import { usePathname, useRouter } from "next/navigation";
import React, { useEffect, useState } from "react";
import openzroIcon from "@/assets/openzro.svg";
import UserDropdown from "@/components/ui/UserDropdown";
import OzShell from "@/components/v2/OzShell";
import OzSidebar, { type OzSidebarSection } from "@/components/v2/OzSidebar";
import OzThemeToggle from "@/components/v2/OzThemeToggle";
import OzTopbar, {
  OzBreadcrumb,
  type OzBreadcrumbSegment,
} from "@/components/v2/OzTopbar";
import AnnouncementProvider from "@/contexts/AnnouncementProvider";
import ApplicationProvider from "@/contexts/ApplicationProvider";
import CountryProvider from "@/contexts/CountryProvider";
import GroupsProvider from "@/contexts/GroupsProvider";
import UsersProvider, { useLoggedInUser } from "@/contexts/UsersProvider";

// Slot context for the v2 topbar's right side. Pages call
// useV2TopbarRight(<MyAction />) once on mount to inject a per-page
// action (e.g. "Add peer", "Save policy") that renders to the left of
// the persistent ThemeToggle + UserDropdown block. The setter from
// useState is reference-stable, so the effect dep [setRight] yields a
// run-once registration with a clean unmount.

interface TopbarSlotValue {
  setRight: (node: React.ReactNode) => void;
}

const TopbarSlotContext = React.createContext<TopbarSlotValue | null>(null);

export function useV2TopbarRight(node: React.ReactNode) {
  const ctx = React.useContext(TopbarSlotContext);
  useEffect(() => {
    if (!ctx) return;
    ctx.setRight(node);
    return () => ctx.setRight(null);
    // node is intentionally NOT a dep — pages pass static JSX once;
    // re-renders that produce a new JSX object would otherwise churn
    // the layout. If a page needs dynamic topbar content, refactor
    // the action body to read from its own context instead.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ctx]);
}

// V2DashboardLayout — Notion/Arc-flavored chrome introduced by ADR-0016.
// Composes OzShell + OzSidebar + OzTopbar around children. Wraps the
// same 5 providers as the legacy DashboardLayout (Application, Users,
// Announcement, Groups, Country) so context-dependent hooks (including
// PermissionsProvider, which UsersProvider mounts internally) keep
// working unchanged.
//
// Used by route layouts that have been migrated to v2 paint per the
// per-screen rollout in ADR-0016 §Migration phases. Routes still on
// DashboardLayout keep the legacy chrome until their migration commit
// lands.

export default function V2DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <ApplicationProvider>
      <UsersProvider>
        <AnnouncementProvider>
          <GroupsProvider>
            <CountryProvider>
              <V2DashboardChrome>{children}</V2DashboardChrome>
            </CountryProvider>
          </GroupsProvider>
        </AnnouncementProvider>
      </UsersProvider>
    </ApplicationProvider>
  );
}

function V2DashboardChrome({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const { setTheme, resolvedTheme } = useTheme();
  const [topbarRight, setTopbarRight] = useState<React.ReactNode>(null);

  // Gate the toggle on `mounted` so SSR/CSR markup match —
  // next-themes' resolvedTheme is undefined during SSR.
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);
  const currentTheme: "light" | "dark" =
    mounted && resolvedTheme === "dark" ? "dark" : "light";

  const sections = buildSidebarSections(pathname, (href) => router.push(href));
  const breadcrumb = breadcrumbForPath(pathname);

  return (
    <TopbarSlotContext.Provider value={{ setRight: setTopbarRight }}>
      <OzShell
        sidebar={
          <OzSidebar
            brand={
              <div className="flex items-center gap-2">
                <Image
                  src={openzroIcon}
                  alt=""
                  width={22}
                  height={22}
                  priority
                />
                <span className="font-sans text-[17px] font-semibold tracking-tight text-oz2-text">
                  open<span className="font-bold text-oz2-acc">Z</span>ro
                </span>
              </div>
            }
            sections={sections}
            footer={<UserFooter />}
          />
        }
        topbar={
          <OzTopbar
            left={
              breadcrumb.length > 0 ? (
                <OzBreadcrumb segments={breadcrumb} />
              ) : null
            }
            right={
              <>
                {topbarRight}
                <OzThemeToggle
                  theme={currentTheme}
                  onToggle={() =>
                    setTheme(currentTheme === "dark" ? "light" : "dark")
                  }
                />
                <UserDropdown />
              </>
            }
          />
        }
      >
        {children}
      </OzShell>
    </TopbarSlotContext.Provider>
  );
}

// Hardcoded path → breadcrumb mapping. As more routes migrate, extend
// this map alongside their PR. Routes that don't match render an
// empty breadcrumb (the topbar handles a missing left slot gracefully).
function breadcrumbForPath(path: string | null): OzBreadcrumbSegment[] {
  if (!path) return [];
  if (path === "/peers" || path.startsWith("/peers/")) {
    return [{ label: "Workspace" }, { label: "Peers" }];
  }
  return [];
}

const navIcon = (path: React.ReactNode) => (
  <svg
    viewBox="0 0 24 24"
    width={16}
    height={16}
    fill="none"
    stroke="currentColor"
    strokeWidth={1.7}
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    {path}
  </svg>
);

const NAV_ICONS = {
  home: navIcon(
    <>
      <path d="M3 11.5 12 4l9 7.5" />
      <path d="M5 10v10h14V10" />
    </>,
  ),
  peer: navIcon(
    <>
      <rect x={3} y={4} width={18} height={12} rx={2} />
      <path d="M8 20h8M12 16v4" />
    </>,
  ),
  network: navIcon(
    <>
      <circle cx={12} cy={5} r={2} />
      <circle cx={6} cy={19} r={2} />
      <circle cx={18} cy={19} r={2} />
      <path d="M12 7v3M12 10l-5 7M12 10l5 7" />
    </>,
  ),
  shield: navIcon(<path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z" />),
  key: navIcon(
    <>
      <circle cx={8} cy={15} r={4} />
      <path d="m11 12 9-9 3 3-3 3 2 2-3 3-2-2-3 3" />
    </>,
  ),
  team: navIcon(
    <>
      <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
      <circle cx={9} cy={7} r={4} />
      <path d="M22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75" />
    </>,
  ),
  activity: navIcon(<path d="M22 12h-4l-3 9L9 3l-3 9H2" />),
  settings: navIcon(
    <>
      <circle cx={12} cy={12} r={3} />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </>,
  ),
};

// Builds the 4-section IA from ADR-0016. Overview is intentionally
// included even though /overview returns 404 today — the ADR explicitly
// accepts that 404 as no-worse than legacy (where the route also doesn't
// exist) until the Overview screen ships post-migration.
function buildSidebarSections(
  pathname: string | null,
  go: (href: string) => void,
): OzSidebarSection[] {
  const matches = (...prefixes: string[]) =>
    !!pathname &&
    prefixes.some((p) => pathname === p || pathname.startsWith(p + "/"));
  return [
    {
      id: "workspace",
      label: "Workspace",
      items: [
        {
          id: "overview",
          label: "Overview",
          icon: NAV_ICONS.home,
          active: matches("/overview"),
          onClick: () => go("/overview"),
        },
        {
          id: "peers",
          label: "Peers",
          icon: NAV_ICONS.peer,
          active: matches("/peers"),
          onClick: () => go("/peers"),
        },
        {
          id: "networks",
          label: "Networks",
          icon: NAV_ICONS.network,
          active: matches("/networks", "/network", "/network-routes"),
          onClick: () => go("/networks"),
        },
      ],
    },
    {
      id: "security",
      label: "Security",
      items: [
        {
          id: "acl",
          label: "Access Control",
          icon: NAV_ICONS.shield,
          active: matches("/access-control", "/posture-checks"),
          onClick: () => go("/access-control"),
        },
        {
          id: "keys",
          label: "Setup Keys",
          icon: NAV_ICONS.key,
          active: matches("/setup-keys"),
          onClick: () => go("/setup-keys"),
        },
      ],
    },
    {
      id: "identity",
      label: "Identity",
      items: [
        {
          id: "team",
          label: "Users & Groups",
          icon: NAV_ICONS.team,
          active: matches("/team"),
          onClick: () => go("/team/users"),
        },
        {
          id: "activity",
          label: "Activity",
          icon: NAV_ICONS.activity,
          active: matches("/events"),
          onClick: () => go("/events/network-traffic"),
        },
      ],
    },
    {
      id: "system",
      label: "System",
      items: [
        {
          id: "settings",
          label: "Settings",
          icon: NAV_ICONS.settings,
          active: matches("/settings"),
          onClick: () => go("/settings"),
        },
      ],
    },
  ];
}

function UserFooter() {
  const { loggedInUser } = useLoggedInUser();
  const display = loggedInUser?.name || loggedInUser?.email || "—";
  const role = loggedInUser?.role || "user";
  return (
    <div className="flex items-center gap-2.5">
      <span className="grid h-7 w-7 place-items-center rounded-full bg-oz2-acc-soft text-[12px] font-semibold text-oz2-acc-text">
        {computeInitials(loggedInUser?.name || loggedInUser?.email)}
      </span>
      <div className="min-w-0 flex-1">
        <p className="truncate text-[12.5px] font-medium text-oz2-text">
          {display}
        </p>
        <p className="truncate text-[11px] text-oz2-text-muted">{role}</p>
      </div>
    </div>
  );
}

function computeInitials(input?: string): string {
  if (!input) return "?";
  const parts = input.split(/[\s.@_-]+/).filter(Boolean);
  if (parts.length === 0) return "?";
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[1][0]).toUpperCase();
}
