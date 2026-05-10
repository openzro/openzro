"use client";

import classNames from "classnames";
import {
  AlertOctagon,
  FolderGit2,
  KeyRound,
  Lock,
  MonitorSmartphone,
  Network,
  Shield,
  ShieldHalf,
} from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { useLoggedInUser } from "@/contexts/UsersProvider";

// SettingsTabsV2 — vertical sub-navigation across the 8 /settings/*
// sub-screens. Settings has too many tabs (8) for a horizontal
// segmented control to stay tidy, and the surface itself reads as a
// dense settings panel rather than a 2-3-tab handoff page, so we
// fall back to the legacy VerticalTabs shape — but in v2 paint:
// icon + label rows with a left accent rail on the active item.
//
// Danger Zone gates on isOwner (mirrors legacy) and gets the err
// palette so the destructive entry is obvious before the click.

interface SettingsTabDef {
  id: string;
  label: string;
  icon: React.ReactNode;
  href: string;
  match: (path: string | null) => boolean;
  visible: boolean;
  danger?: boolean;
}

export default function SettingsTabsV2() {
  const pathname = usePathname();
  const { permission } = usePermissions();
  const { isOwner } = useLoggedInUser();

  const settingsRead = permission.settings.read;

  const tabs: SettingsTabDef[] = [
    {
      id: "authentication",
      label: "Authentication",
      icon: <Shield size={14} />,
      href: "/settings/authentication",
      match: (p) => p === "/settings/authentication" || p === "/settings",
      visible: settingsRead,
    },
    {
      id: "auth-providers",
      label: "Auth Providers",
      icon: <KeyRound size={14} />,
      href: "/settings/auth-providers",
      match: (p) => p === "/settings/auth-providers",
      visible: settingsRead,
    },
    {
      id: "groups",
      label: "Groups",
      icon: <FolderGit2 size={14} />,
      href: "/settings/groups",
      match: (p) => p === "/settings/groups",
      visible: settingsRead,
    },
    {
      id: "permissions",
      label: "Permissions",
      icon: <Lock size={14} />,
      href: "/settings/permissions",
      match: (p) => p === "/settings/permissions",
      visible: settingsRead,
    },
    {
      id: "networks",
      label: "Networks",
      icon: <Network size={14} />,
      href: "/settings/networks",
      match: (p) => p === "/settings/networks",
      visible: settingsRead,
    },
    {
      id: "clients",
      label: "Clients",
      icon: <MonitorSmartphone size={14} />,
      href: "/settings/clients",
      match: (p) => p === "/settings/clients",
      visible: settingsRead,
    },
    {
      id: "device-admission",
      label: "Device Admission",
      icon: <ShieldHalf size={14} />,
      href: "/settings/device-admission",
      match: (p) => p === "/settings/device-admission",
      visible: settingsRead,
    },
    {
      id: "danger-zone",
      label: "Danger Zone",
      icon: <AlertOctagon size={14} />,
      href: "/settings/danger-zone",
      match: (p) => p === "/settings/danger-zone",
      visible: !!isOwner,
      danger: true,
    },
  ];

  const visibleTabs = tabs.filter((t) => t.visible);
  if (visibleTabs.length <= 1) return null;

  // Danger Zone is visually separated from the standard tabs with a
  // hairline divider so it reads as a distinct gravity zone, mirroring
  // how the legacy VerticalTabs pinned it to the bottom of the list.
  const standardTabs = visibleTabs.filter((t) => !t.danger);
  const dangerTabs = visibleTabs.filter((t) => t.danger);

  return (
    <nav
      role="tablist"
      aria-orientation="vertical"
      aria-label="Settings sub-navigation"
      className="flex flex-col gap-0.5"
    >
      {standardTabs.map((tab) => (
        <SettingsTabLink key={tab.href} tab={tab} pathname={pathname} />
      ))}
      {dangerTabs.length > 0 && (
        <>
          <div className="my-2 h-px bg-oz2-border-soft" aria-hidden />
          {dangerTabs.map((tab) => (
            <SettingsTabLink key={tab.href} tab={tab} pathname={pathname} />
          ))}
        </>
      )}
    </nav>
  );
}

function SettingsTabLink({
  tab,
  pathname,
}: {
  tab: SettingsTabDef;
  pathname: string | null;
}) {
  const active = tab.match(pathname);
  return (
    <Link
      href={tab.href}
      role="tab"
      aria-selected={active}
      className={classNames(
        // Each row is 32px tall, padded left by 12px + 2px rail (so
        // active and idle states share horizontal text alignment). The
        // accent rail uses a transparent border on inactive items and
        // flips to oz2-acc / oz2-err on the active one.
        "group relative inline-flex h-8 items-center gap-2.5 rounded-[6px] border-l-2 pl-3 pr-3 text-[13px] font-medium transition-colors",
        active
          ? tab.danger
            ? "border-oz2-err bg-oz2-err-bg text-oz2-err"
            : "border-oz2-acc bg-oz2-acc-soft text-oz2-acc-text"
          : tab.danger
            ? "border-transparent text-oz2-err/75 hover:border-oz2-err/40 hover:bg-oz2-err-bg/60 hover:text-oz2-err"
            : "border-transparent text-oz2-text-muted hover:bg-oz2-hover hover:text-oz2-text",
      )}
    >
      <span
        aria-hidden
        className="inline-flex h-3.5 w-3.5 shrink-0 items-center justify-center"
      >
        {tab.icon}
      </span>
      <span className="truncate">{tab.label}</span>
    </Link>
  );
}
