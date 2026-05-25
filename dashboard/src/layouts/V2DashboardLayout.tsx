"use client";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@components/DropdownMenu";
import { LogOutIcon, SearchIcon, User2 } from "lucide-react";
import Image from "next/image";
import { usePathname, useRouter } from "next/navigation";
import { useTheme } from "next-themes";
import React, { useEffect, useMemo, useState } from "react";
import openzroIcon from "@/assets/openzro.svg";
import OzShell from "@/components/v2/OzShell";
import OzSidebar, { type OzSidebarSection } from "@/components/v2/OzSidebar";
import OzThemeToggle from "@/components/v2/OzThemeToggle";
import OzTopbar, {
  OzBreadcrumb,
  type OzBreadcrumbSegment,
} from "@/components/v2/OzTopbar";
import AnnouncementProvider from "@/contexts/AnnouncementProvider";
import ApplicationProvider, {
  useApplicationContext,
} from "@/contexts/ApplicationProvider";
import CountryProvider from "@/contexts/CountryProvider";
import GroupsProvider from "@/contexts/GroupsProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import UsersProvider, { useLoggedInUser } from "@/contexts/UsersProvider";
import { useLocalStorage } from "@/hooks/useLocalStorage";
import type { Permissions } from "@/interfaces/Permission";
import { GlobalSearchModal } from "@/modules/search/GlobalSearchModal";

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
    // `node` is in the dep list so consumers can pass JSX whose
    // disabled state depends on parent state (Save buttons gated on
    // hasChanges, etc). Re-registering each render is cheap — it's
    // a single setState on the slot context that React 18 batches
    // alongside the page's own render commit, no visible flicker.
  }, [ctx, node]);
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
  const [sidebarCollapsed, setSidebarCollapsed] = useLocalStorage<boolean>(
    "ozv2-sidebar-collapsed",
    false,
  );
  const [searchOpen, setSearchOpen] = useState(false);

  // Global Ctrl+K / Cmd+K shortcut for the GlobalSearchModal — matches
  // the handoff sidebar input's ⌘K kbd hint. Ignored when the operator
  // is typing into another input/textarea/contenteditable so the
  // shortcut never steals a keystroke from a focused form field.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.key === "k" || e.key === "K")) return;
      if (!(e.metaKey || e.ctrlKey)) return;
      const tag = (e.target as HTMLElement | null)?.tagName?.toLowerCase();
      const editable =
        tag === "input" ||
        tag === "textarea" ||
        (e.target as HTMLElement | null)?.isContentEditable;
      if (editable) return;
      e.preventDefault();
      setSearchOpen((prev) => !prev);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Gate the toggle on `mounted` so SSR/CSR markup match —
  // next-themes' resolvedTheme is undefined during SSR.
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);
  const currentTheme: "light" | "dark" =
    mounted && resolvedTheme === "dark" ? "dark" : "light";

  // Gate sidebar items by the operator's per-module permissions so
  // restricted / non-admin users don't see admin-only sections and
  // then hit 403 on click. Mirrors the upstream Navigation.tsx
  // pattern (per-item `visible={permission.<module>.read}`) but
  // operates on our data-driven OzSidebar sections — see
  // buildSidebarSections for the per-item mapping.
  const { permission, isRestricted } = usePermissions();
  const sections = buildSidebarSections(
    pathname,
    (href) => router.push(href),
    permission,
    isRestricted,
  );
  const breadcrumb = breadcrumbForPath(pathname);

  // Stabilize the context value — wrapping in an object literal at
  // the JSX boundary creates a new identity on every render, which
  // would re-fire useV2TopbarRight's effect every time and infinite-
  // loop the layout. setTopbarRight from useState is reference-stable
  // so memoizing with empty deps is safe.
  const topbarSlotValue = useMemo<TopbarSlotValue>(
    () => ({ setRight: setTopbarRight }),
    [],
  );

  return (
    <TopbarSlotContext.Provider value={topbarSlotValue}>
      <OzShell
        sidebarCollapsed={sidebarCollapsed}
        sidebar={
          <OzSidebar
            collapsed={sidebarCollapsed}
            brand={
              sidebarCollapsed ? (
                <Image
                  src={openzroIcon}
                  alt="openZro"
                  width={22}
                  height={22}
                  priority
                />
              ) : (
                <div className="flex items-center gap-2">
                  <Image
                    src={openzroIcon}
                    alt=""
                    width={22}
                    height={22}
                    priority
                  />
                  <span className="font-sans text-[18px] font-semibold tracking-tight text-oz2-text">
                    open<span className="font-bold text-oz2-acc">Z</span>ro
                  </span>
                </div>
              )
            }
            sections={sections}
            search={<SearchTrigger onClick={() => setSearchOpen(true)} />}
            footer={<UserFooter collapsed={sidebarCollapsed} />}
          />
        }
        topbar={
          <OzTopbar
            left={
              <div className="flex items-center gap-3">
                <SidebarTrigger
                  collapsed={sidebarCollapsed}
                  onToggle={() => setSidebarCollapsed(!sidebarCollapsed)}
                />
                <span
                  aria-hidden="true"
                  className="h-5 w-px bg-oz2-border"
                />
                {breadcrumb.length > 0 && (
                  <OzBreadcrumb segments={breadcrumb} />
                )}
              </div>
            }
            right={
              <>
                {/* Theme toggle sits left of the per-page action so the
                    layout pattern stays consistent across migrated
                    screens. UserDropdown moved to the sidebar footer
                    so the topbar focuses on page-level affordances. */}
                <OzThemeToggle
                  theme={currentTheme}
                  onToggle={() =>
                    setTheme(currentTheme === "dark" ? "light" : "dark")
                  }
                />
                {topbarRight}
              </>
            }
          />
        }
      >
        {children}
      </OzShell>
      <GlobalSearchModal open={searchOpen} setOpen={setSearchOpen} />
    </TopbarSlotContext.Provider>
  );
}

// SearchTrigger — looks like an inline input in the sidebar slot but
// is actually a button: clicking it (or Ctrl+K) opens the
// GlobalSearchModal which owns the real cmdk-style search. The kbd
// hint mirrors the handoff and helps the operator discover the
// shortcut.
function SearchTrigger({ onClick }: { onClick: () => void }) {
  const [mac, setMac] = React.useState(false);
  React.useEffect(() => {
    if (typeof navigator === "undefined") return;
    setMac(/Mac|iPod|iPhone|iPad/.test(navigator.platform));
  }, []);
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label="Search (Ctrl+K)"
      className="flex h-9 w-full items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 text-[13px] text-oz2-text-faint transition-colors hover:border-oz2-border-strong hover:bg-oz2-hover"
    >
      <SearchIcon size={14} className="shrink-0" />
      <span className="flex-1 text-left">Search…</span>
      <kbd className="inline-flex items-center gap-0.5 rounded-[5px] border border-oz2-border-soft bg-oz2-bg-sunken px-1.5 py-[2px] font-mono text-[10.5px] text-oz2-text-faint">
        {mac ? "⌘" : "Ctrl"}
        <span className="px-px">K</span>
      </kbd>
    </button>
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
  // /peer (singular) is the per-peer detail page. Crumb tail is a
  // generic "Peer" because the peer's own name shows up as the page
  // H1 inline.
  if (path === "/peer" || path.startsWith("/peer?")) {
    return [
      { label: "Workspace" },
      { label: "Peers" },
      { label: "Peer" },
    ];
  }
  if (path === "/networks" || path.startsWith("/networks/")) {
    return [{ label: "Workspace" }, { label: "Networks" }];
  }
  // /network (singular) is the per-network detail page reached from
  // the Networks list. The network's own name shows up as the page
  // H1 inline so we collapse the crumb tail to a generic "Network".
  if (path === "/network" || path.startsWith("/network?")) {
    return [
      { label: "Workspace" },
      { label: "Networks" },
      { label: "Network" },
    ];
  }
  if (path === "/network-routes" || path.startsWith("/network-routes/")) {
    return [{ label: "Workspace" }, { label: "Network Routes" }];
  }
  if (path === "/setup-keys" || path.startsWith("/setup-keys/")) {
    return [{ label: "Workspace" }, { label: "Setup Keys" }];
  }
  if (path === "/access-control" || path.startsWith("/access-control/")) {
    return [{ label: "Security" }, { label: "Access Control" }];
  }
  if (path === "/control-center" || path.startsWith("/control-center")) {
    return [{ label: "Security" }, { label: "Control Center" }];
  }
  if (path === "/posture-checks" || path.startsWith("/posture-checks/")) {
    return [{ label: "Security" }, { label: "Posture Checks" }];
  }
  // /team/users, /team/groups and /team/service-users all sit under the
  // single conceptual "Users & Groups" screen (the page H1 + the
  // TeamTabs sub-nav present the three views as siblings). The
  // breadcrumb is unified here so it stays consistent with the H1
  // regardless of which tab is active.
  if (
    path === "/team" ||
    path === "/team/users" ||
    path === "/team/groups" ||
    path === "/team/service-users"
  ) {
    return [{ label: "Identity" }, { label: "Users & Groups" }];
  }
  if (path === "/team/user" || path.startsWith("/team/user?")) {
    return [
      { label: "Identity" },
      { label: "Users & Groups" },
      { label: "User" },
    ];
  }
  // /events/* split into two top-level Identity items per the
  // handoff: Activity (audit trail) and Flow Traffic (network
  // observability). Each gets its own crumb so the topbar matches
  // the per-screen H1.
  if (path === "/events" || path === "/events/audit") {
    return [{ label: "Identity" }, { label: "Activity" }];
  }
  if (path === "/events/network-traffic") {
    return [{ label: "Identity" }, { label: "Flow Traffic" }];
  }
  // /dns/* (Nameservers + Settings + bare /dns redirect) collapses
  // under the umbrella "DNS" crumb so all sub-routes share the same
  // header crumb regardless of which DnsTabs tab is active.
  if (
    path === "/dns" ||
    path === "/dns/nameservers" ||
    path === "/dns/zones" ||
    path === "/dns/zone" ||
    path === "/dns/settings"
  ) {
    return [{ label: "Workspace" }, { label: "DNS" }];
  }
  // /integrations sub-tabs are deep-linked via ?subtab=… (Flow /
  // Activity / MDM / SCIM) but the URL path stays the same, so the
  // breadcrumb collapses to the umbrella "Integrations" crumb.
  if (path === "/integrations" || path.startsWith("/integrations/")) {
    return [{ label: "System" }, { label: "Integrations" }];
  }
  // /settings/* — each sub-route gets a specific second crumb so the
  // topbar reflects which tab is active. The bare /settings redirects
  // to /settings/authentication; the crumb match below covers the
  // redirect target so the breadcrumb is correct on the in-between
  // tick before the redirect lands.
  if (path === "/settings" || path.startsWith("/settings/")) {
    const SETTINGS_TABS: Record<string, string> = {
      "/settings/authentication": "Authentication",
      "/settings/auth-providers": "Auth Providers",
      "/settings/groups": "Groups",
      "/settings/permissions": "Permissions",
      "/settings/networks": "Networks",
      "/settings/clients": "Clients",
      "/settings/device-admission": "Device Admission",
      "/settings/danger-zone": "Danger Zone",
    };
    const sub = SETTINGS_TABS[path];
    const crumbs: OzBreadcrumbSegment[] = [
      { label: "System" },
      { label: "Settings" },
    ];
    if (sub) crumbs.push({ label: sub });
    return crumbs;
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
  // Split arrow — single trunk rising from the bottom and forking
  // into two branches with arrowheads pointing up-left and up-right.
  // Conveys "traffic distribution / route branching" for
  // /network-routes, visually distinct from the connected-mesh
  // `network` icon used for /networks (segment, not path).
  networkRoutes: navIcon(
    <>
      <path d="M12 21V13" />
      <path d="M12 13l-6-6" />
      <path d="M10 7H6v4" />
      <path d="M12 13l6-6" />
      <path d="M14 7h4v4" />
    </>,
  ),
  shield: navIcon(<path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z" />),
  // Same shield silhouette as `shield`, with a checkmark inside.
  // Differentiates Posture Checks ("device is in spec") from Access
  // Control ("who can reach what") — the two were sharing the plain
  // shield and the labels were doing all the differentiation work.
  shieldCheck: navIcon(
    <>
      <path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z" />
      <path d="m9 12 2 2 4-4" />
    </>,
  ),
  accessGraph: navIcon(
    <>
      <circle cx={5} cy={6} r={2} />
      <circle cx={19} cy={6} r={2} />
      <circle cx={12} cy={18} r={2} />
      <path d="M6.5 7.3 10.6 16M17.5 7.3 13.4 16M7 6h10" />
    </>,
  ),
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
  flowTraffic: navIcon(
    <>
      <path d="M3 7h13M16 7l-3-3m3 3-3 3" />
      <path d="M21 17H8M8 17l3-3m-3 3 3 3" />
    </>,
  ),
  integrations: navIcon(
    <>
      <path d="M9 2v4M15 2v4" />
      <rect x={4} y={6} width={16} height={6} rx={2} />
      <path d="M12 12v4" />
      <path d="M9 16h6a3 3 0 0 1 3 3v3H6v-3a3 3 0 0 1 3-3z" />
    </>,
  ),
  dns: navIcon(
    <>
      <circle cx={12} cy={12} r={9} />
      <path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18" />
    </>,
  ),
  settings: navIcon(
    <>
      <circle cx={12} cy={12} r={3} />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </>,
  ),
  docs: navIcon(
    <>
      <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20" />
      <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z" />
    </>,
  ),
};

// Builds the 4-section IA from ADR-0016. Overview is intentionally
// included even though /overview returns 404 today — the ADR explicitly
// accepts that 404 as no-worse than legacy (where the route also doesn't
// exist) until the Overview screen ships post-migration.
// itemSpec extends OzSidebarItem with a `visible` flag that
// buildSidebarSections uses to filter — kept local so the public
// OzSidebar API stays a pure data renderer.
type sidebarItemSpec = OzSidebarSection["items"][number] & {
  visible: boolean;
};

function buildSidebarSections(
  pathname: string | null,
  go: (href: string) => void,
  permission: Permissions["modules"],
  isRestricted: boolean,
): OzSidebarSection[] {
  const matches = (...prefixes: string[]) =>
    !!pathname &&
    prefixes.some((p) => pathname === p || pathname.startsWith(p + "/"));

  // Per-item visibility mirrors the per-route gates applied by each
  // page.tsx via <RestrictedAccess>. Mapping derived by grepping
  // permission.<module>.read across src/app/(v2-dashboard)/**/page.tsx
  // so the sidebar reflects exactly what each landing route will let
  // the operator see. Items with no page-level gate (Overview, Peers,
  // Control Center, Documentation) follow the upstream Navigation.tsx
  // convention: Peers and Overview are hidden for restricted users
  // (peer-only accounts whose only interactive surface is the
  // PeersBlockedView landing), the rest stay visible.
  const raw: { id: string; label: string; items: sidebarItemSpec[] }[] = [
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
          visible: !isRestricted,
        },
        {
          id: "peers",
          label: "Peers",
          icon: NAV_ICONS.peer,
          active: matches("/peers"),
          onClick: () => go("/peers"),
          visible: !isRestricted,
        },
        {
          id: "networks",
          label: "Networks",
          icon: NAV_ICONS.network,
          active: matches("/networks", "/network"),
          onClick: () => go("/networks"),
          visible: permission.networks.read,
        },
        {
          id: "network-routes",
          label: "Network Routes",
          icon: NAV_ICONS.networkRoutes,
          active: matches("/network-routes"),
          onClick: () => go("/network-routes"),
          visible: permission.routes.read,
        },
        {
          id: "dns",
          label: "DNS",
          icon: NAV_ICONS.dns,
          active: matches("/dns"),
          // Umbrella entry — visible if any of the three sub-tabs is
          // accessible. Pick the first sub-tab the operator can read
          // as the landing destination so a user with only
          // dns_zones.read or only dns.read doesn't land on
          // /dns/nameservers (a route they can't read) and bounce on
          // RestrictedAccess. DnsTabs itself hides tabs the user
          // can't open.
          onClick: () => {
            if (permission.nameservers.read) {
              go("/dns/nameservers");
            } else if (permission.dns_zones.read) {
              go("/dns/zones");
            } else {
              go("/dns/settings");
            }
          },
          visible:
            permission.nameservers.read ||
            permission.dns_zones.read ||
            permission.dns.read,
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
          active: matches("/access-control"),
          onClick: () => go("/access-control"),
          visible: permission.policies.read,
        },
        {
          id: "control-center",
          label: "Control Center",
          icon: NAV_ICONS.accessGraph,
          active: matches("/control-center"),
          onClick: () => go("/control-center"),
          // control-center/page.tsx gates on permission.settings.update
          // (admin-only), NOT policies.read — Control Center exposes
          // network-wide policy effectiveness + posture-blocked
          // diagnostics that an operator with policy read-only
          // shouldn't see. Mirror the page-level gate exactly so the
          // sidebar doesn't advertise a route the user will bounce on.
          visible: permission.settings.update,
        },
        {
          id: "posture-checks",
          label: "Posture Checks",
          icon: NAV_ICONS.shieldCheck,
          active: matches("/posture-checks"),
          onClick: () => go("/posture-checks"),
          // posture-checks/page.tsx gates on permission.policies.read.
          visible: permission.policies.read,
        },
        {
          id: "keys",
          label: "Setup Keys",
          icon: NAV_ICONS.key,
          active: matches("/setup-keys"),
          onClick: () => go("/setup-keys"),
          visible: permission.setup_keys.read,
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
          // Either tab is enough — TeamTabs hides the one the user
          // can't read. Land on /team/users when readable, otherwise
          // fall back to /team/groups so a group-only operator still
          // has a working entry point.
          onClick: () => {
            if (permission.users.read) {
              go("/team/users");
            } else {
              go("/team/groups");
            }
          },
          visible: permission.users.read || permission.groups.read,
        },
        {
          id: "activity",
          label: "Activity",
          icon: NAV_ICONS.activity,
          // Activity = audit trail only. Flow Traffic split into its
          // own item below per the handoff (TrafficScreen breadcrumb
          // is "Acme Mesh > Flow Traffic", standalone). They're
          // semantically different surfaces — admin trail vs network
          // observability — so the sidebar reflects that.
          active:
            !!pathname &&
            (pathname === "/events" || pathname === "/events/audit"),
          onClick: () => go("/events/audit"),
          visible: permission.events.read,
        },
        {
          id: "flow-traffic",
          label: "Flow Traffic",
          icon: NAV_ICONS.flowTraffic,
          active: matches("/events/network-traffic"),
          onClick: () => go("/events/network-traffic"),
          visible: permission.events.read,
        },
      ],
    },
    {
      id: "system",
      label: "System",
      items: [
        {
          id: "integrations",
          label: "Integrations",
          icon: NAV_ICONS.integrations,
          active: matches("/integrations"),
          onClick: () => go("/integrations"),
          // integrations/page.tsx gates on permission.settings.read.
          visible: permission.settings.read,
        },
        {
          id: "settings",
          label: "Settings",
          icon: NAV_ICONS.settings,
          active: matches("/settings"),
          onClick: () => go("/settings"),
          visible: permission.settings.read,
        },
        {
          id: "documentation",
          label: "Documentation",
          icon: NAV_ICONS.docs,
          active: false,
          // External — docs.openzro.io is its own site (the /docs
          // repo). Click opens a new tab, matching the legacy
          // sidebar "Documentation" affordance. Always visible.
          onClick: () => window.open("https://docs.openzro.io", "_blank"),
          visible: true,
        },
      ],
    },
  ];

  // Filter out invisible items and any section that ends up empty.
  return raw
    .map((section) => ({
      id: section.id,
      label: section.label,
      items: section.items
        .filter((it) => it.visible)
        .map(({ visible: _omit, ...rest }) => rest),
    }))
    .filter((section) => section.items.length > 0);
}

// UserFooter — sidebar bottom card matching the Claude Design handoff
// pattern (design/shell.jsx "User card"): bordered surface with a
// gradient avatar, name + role lines, and a separate ghost-style
// kebab trigger on the right. The whole card is informational; only
// the kebab opens the dropdown — same affordance the design specifies.
// SidebarTrigger — shadcn-style hamburger that toggles the icon-only
// collapsed sidebar. The aria-label flips between "Expand"/"Collapse"
// based on current state so screen-reader users hear the action they're
// about to take, not the current state.
function SidebarTrigger({
  collapsed,
  onToggle,
}: {
  collapsed: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
      aria-expanded={!collapsed}
      className="grid h-7 w-7 cursor-pointer place-items-center rounded-md text-oz2-text-muted transition-colors hover:bg-oz2-hover hover:text-oz2-text"
    >
      <svg
        viewBox="0 0 24 24"
        width={15}
        height={15}
        fill="none"
        stroke="currentColor"
        strokeWidth={1.8}
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <rect x={3} y={4} width={18} height={16} rx={2} />
        <path d="M9 4v16" />
      </svg>
    </button>
  );
}

function UserFooter({ collapsed = false }: { collapsed?: boolean }) {
  const [open, setOpen] = useState(false);
  const router = useRouter();
  const { loggedInUser, logout } = useLoggedInUser();
  const { user } = useApplicationContext();

  const display = loggedInUser?.name || loggedInUser?.email || "—";
  const role = loggedInUser?.role || "user";
  const initials = computeInitials(loggedInUser?.name || loggedInUser?.email);

  // Collapsed footer: just the gradient avatar centered, clicking it
  // opens the same dropdown as the expanded version.
  if (collapsed) {
    return (
      <DropdownMenu modal={false} open={open} onOpenChange={setOpen}>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            aria-label="Profile menu"
            className="grid h-9 w-9 cursor-pointer place-items-center rounded-full text-[12px] font-semibold leading-none text-white shadow-oz2-sm transition-transform hover:scale-105"
            style={{
              background: "linear-gradient(135deg, #f472b6, #a78bfa)",
            }}
          >
            {initials}
          </button>
        </DropdownMenuTrigger>
        <UserMenuContent
          loggedInUser={loggedInUser}
          user={user}
          onClose={() => setOpen(false)}
          logout={logout}
          router={router}
        />
      </DropdownMenu>
    );
  }

  return (
    <div className="flex items-center gap-2.5 rounded-[10px] border border-oz2-border-soft bg-oz2-surface px-2.5 py-2">
      <span
        aria-hidden="true"
        className="grid h-7 w-7 shrink-0 place-items-center rounded-full text-[12px] font-semibold leading-none text-white"
        // Pink → violet gradient from the design handoff. Inline style
        // because Tailwind doesn't expose these exact hex stops.
        style={{
          background: "linear-gradient(135deg, #f472b6, #a78bfa)",
        }}
      >
        {initials}
      </span>
      <div className="min-w-0 flex-1">
        <p className="truncate text-[13.5px] font-semibold leading-tight text-oz2-text">
          {display}
        </p>
        <p className="mt-0.5 truncate text-[12px] leading-tight text-oz2-text-muted">
          {role}
        </p>
      </div>
      <DropdownMenu modal={false} open={open} onOpenChange={setOpen}>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            aria-label="Profile menu"
            className="grid h-7 w-7 shrink-0 cursor-pointer place-items-center rounded-md border border-transparent text-oz2-text-muted transition-colors hover:bg-oz2-hover hover:text-oz2-text"
          >
            <svg
              viewBox="0 0 24 24"
              width={14}
              height={14}
              fill="none"
              stroke="currentColor"
              strokeWidth={2}
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <circle cx={5} cy={12} r={1.4} />
              <circle cx={12} cy={12} r={1.4} />
              <circle cx={19} cy={12} r={1.4} />
            </svg>
          </button>
        </DropdownMenuTrigger>
        <UserMenuContent
          loggedInUser={loggedInUser}
          user={user}
          onClose={() => setOpen(false)}
          logout={logout}
          router={router}
        />
      </DropdownMenu>
    </div>
  );
}

// Shared menu used by both expanded and collapsed UserFooter triggers.
function UserMenuContent({
  loggedInUser,
  user,
  onClose,
  logout,
  router,
}: {
  loggedInUser: ReturnType<typeof useLoggedInUser>["loggedInUser"];
  user: ReturnType<typeof useApplicationContext>["user"];
  onClose: () => void;
  logout: () => Promise<void>;
  router: ReturnType<typeof useRouter>;
}) {
  return (
    <DropdownMenuContent
      side="right"
      align="end"
      sideOffset={6}
      className="w-56"
      forceMount
    >
      <DropdownMenuLabel className="font-normal">
        <div className="flex flex-col space-y-1">
          <div className="truncate text-sm font-medium leading-none">
            {user?.name}
          </div>
          <div className="truncate text-xs leading-none text-neutral-500 dark:text-nb-gray-400">
            {user?.email}
          </div>
        </div>
      </DropdownMenuLabel>
      <DropdownMenuSeparator />
      {loggedInUser && (
        <DropdownMenuItem
          onClick={() => {
            onClose();
            router.push(`/team/user?id=${loggedInUser.id}`);
          }}
        >
          <div className="flex items-center gap-3">
            <User2 size={14} />
            Profile Settings
          </div>
        </DropdownMenuItem>
      )}
      <DropdownMenuItem onClick={() => logout()}>
        <div className="flex items-center gap-3">
          <LogOutIcon size={14} />
          Log out
        </div>
      </DropdownMenuItem>
    </DropdownMenuContent>
  );
}

function computeInitials(input?: string): string {
  if (!input) return "?";
  const parts = input.split(/[\s.@_-]+/).filter(Boolean);
  if (parts.length === 0) return "?";
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[1][0]).toUpperCase();
}
