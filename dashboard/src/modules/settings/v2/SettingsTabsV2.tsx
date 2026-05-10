"use client";

import classNames from "classnames";
import {
  AlertOctagon,
  FolderGit2,
  KeyRound,
  Lock,
  MonitorSmartphone,
  Network,
  ShieldHalf,
  Shield,
} from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { useLoggedInUser } from "@/contexts/UsersProvider";

// SettingsTabsV2 — page-header sub-navigation across the 8 /settings/*
// sub-screens introduced by ADR-0016 phase 5. Sidebar exposes a single
// "Settings" item that lands on /settings/authentication; this
// segmented control swaps between the 8 sections without going back to
// the sidebar. Mirrors DnsTabs / TeamTabs / EventsTabs in structure
// (active match, permission gate, visible filter, segmented-control
// treatment) — Settings just has more tabs, so the bar wraps onto
// multiple rows when the viewport is narrow.
//
// Danger Zone is owner-only, matching the legacy gating in
// /modules/settings/page.tsx.

interface SettingsTabDef {
  id: string;
  label: string;
  icon: React.ReactNode;
  href: string;
  match: (path: string | null) => boolean;
  visible: boolean;
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
    },
  ];

  const visibleTabs = tabs.filter((t) => t.visible);
  if (visibleTabs.length <= 1) return null;

  return (
    <nav
      role="tablist"
      aria-label="Settings sub-navigation"
      className="inline-flex max-w-full flex-wrap items-center gap-1 rounded-oz2-input bg-oz2-bg-sunken p-1 text-oz2-text-muted"
    >
      {visibleTabs.map((tab) => {
        const active = tab.match(pathname);
        const isDanger = tab.id === "danger-zone";
        return (
          <Link
            key={tab.href}
            href={tab.href}
            role="tab"
            aria-selected={active}
            className={classNames(
              "inline-flex h-[28px] items-center gap-2 whitespace-nowrap rounded-[6px] px-3 text-[13px] font-medium transition-colors",
              active
                ? isDanger
                  ? "bg-oz2-err-bg text-oz2-err shadow-oz2-sm"
                  : "bg-oz2-surface text-oz2-text shadow-oz2-sm"
                : isDanger
                  ? "text-oz2-err/80 hover:text-oz2-err"
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
