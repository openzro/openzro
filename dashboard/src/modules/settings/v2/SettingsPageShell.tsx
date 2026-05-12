"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import * as Tabs from "@radix-ui/react-tabs";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { useLoggedInUser } from "@/contexts/UsersProvider";
import SettingsTabsV2 from "@/modules/settings/v2/SettingsTabsV2";

// SettingsPageShell — shared chrome for the 8 /settings/* sub-pages.
// Renders the page H1 + sub paragraph, the SettingsTabsV2 segmented
// sub-nav, and wraps the legacy tab body in a Radix Tabs.Root with
// the matching value (each legacy tab body lives inside a
// <Tabs.Content value="X">). The Tabs.Root provides the Radix context
// the body's Tabs.Content needs to render without modifying the body
// itself — phase 5 keeps the legacy tab implementations untouched
// pending per-tab v2 paint commits.
//
// Permission gating mirrors the legacy /settings page: read on
// settings.read for all tabs; isOwner additionally for danger-zone.

interface Props {
  /** Radix Tabs value matching the legacy tab body. */
  value: string;
  /** Slug used to detect Danger Zone (extra isOwner gate). */
  page?: string;
  children: React.ReactNode;
}

export default function SettingsPageShell({ value, page, children }: Props) {
  const { permission } = usePermissions();
  const { isOwner } = useLoggedInUser();

  const isDangerZone = page === "danger-zone";
  const hasAccess = isDangerZone ? !!isOwner : permission.settings.read;

  return (
    <div className="space-y-6 p-8">
      <header>
        <h1 className="text-[24px] font-semibold tracking-tight">Settings</h1>
        <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
          Workspace-wide configuration. Authentication, identity providers,
          group defaults, role permissions, network behavior, client defaults,
          and the irreversible operations behind the Danger Zone.
        </p>
      </header>

      {/* Two-column layout: vertical sub-nav on the left (220px),
          tab content on the right. The nav becomes sticky on tall
          viewports so it stays in reach while the content scrolls. */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[220px_minmax(0,1fr)] lg:gap-8">
        <aside className="lg:sticky lg:top-6 lg:self-start">
          <SettingsTabsV2 />
        </aside>
        <RestrictedAccess page="Settings" hasAccess={hasAccess}>
          <Tabs.Root value={value}>{children}</Tabs.Root>
        </RestrictedAccess>
      </div>
    </div>
  );
}
