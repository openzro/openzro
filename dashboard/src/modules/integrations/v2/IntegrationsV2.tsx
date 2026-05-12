"use client";

import classNames from "classnames";
import {
  CableIcon,
  RadioTowerIcon,
  ShieldCheckIcon,
  UsersIcon,
} from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import React, { useEffect, useState } from "react";
import ActivityExportersSectionV2 from "@/modules/integrations/v2/sections/ActivityExportersSectionV2";
import FlowExportsSectionV2 from "@/modules/integrations/v2/sections/FlowExportsSectionV2";
import MDMProvidersSectionV2 from "@/modules/integrations/v2/sections/MDMProvidersSectionV2";
import SCIMSetupSectionV2 from "@/modules/integrations/v2/sections/SCIMSetupSectionV2";

// IntegrationsV2 — phase-5.17 v2 chrome over the legacy
// IntegrationsPage's four sub-sections (Flow Exports / Activity
// Streamer / MDM-EDR Providers / SCIM Setup). The body sections
// themselves are reused unchanged from the legacy module — they
// own their own SWR fetches, modals and per-section tables, and
// behave identically. Only the page chrome around them flips:
//
//   - legacy: PageContainer + Breadcrumbs + h1 + Paragraph +
//     vertical-rail sub-nav with the active section to the right
//   - v2:     V2DashboardLayout (already provides chrome) + h1
//     "Integrations" + handoff-flavored subtitle + a vertical
//     220px sub-nav on the left matching SettingsTabsV2, with
//     the active section rendered to the right
//
// Sub-tab state still deep-links via ?subtab=… so refresh /
// share-link keeps the operator on the right section, matching
// the legacy behavior.

type SubTab = {
  value: string;
  label: string;
  icon: React.ReactNode;
};

const SUB_TABS: SubTab[] = [
  { value: "flow", label: "Flow Exports", icon: <CableIcon size={14} /> },
  {
    value: "activity",
    label: "Activity Streamer",
    icon: <RadioTowerIcon size={14} />,
  },
  { value: "mdm", label: "MDM / EDR", icon: <ShieldCheckIcon size={14} /> },
  {
    value: "idp-sync",
    label: "Identity Provider Sync",
    icon: <UsersIcon size={14} />,
  },
];

const DEFAULT_SUB_TAB = "flow";

function isValidSubTab(value: string | null): value is string {
  return typeof value === "string" && SUB_TABS.some((t) => t.value === value);
}

export default function IntegrationsV2() {
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();

  // Same pattern the legacy page used: read ?subtab= once on mount,
  // track locally, and reconcile on browser back/forward via the
  // effect below — keeps URL and state in lockstep without racing.
  const initial = isValidSubTab(params.get("subtab"))
    ? (params.get("subtab") as string)
    : DEFAULT_SUB_TAB;
  const [active, setActive] = useState<string>(initial);

  useEffect(() => {
    const fromURL = params.get("subtab");
    if (isValidSubTab(fromURL) && fromURL !== active) {
      setActive(fromURL);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params]);

  const select = (value: string) => {
    setActive(value);
    router.push(`${pathname}?subtab=${value}`, { scroll: false });
  };

  return (
    <div className="space-y-6 p-8">
      <header>
        <h1 className="text-[24px] font-semibold tracking-tight">
          Integrations
        </h1>
        <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
          External destinations and providers — SIEM streaming + cold archive
          for traffic events, MDM/EDR vendors for posture compliance, and SCIM
          2.0 for user provisioning from your IdP.
        </p>
      </header>

      {/* Two-column layout: vertical sub-nav on the left (220px), the
          active section on the right. Mirrors SettingsPageShell so the
          two settings-adjacent surfaces read the same. */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[220px_minmax(0,1fr)] lg:gap-8">
        <aside className="lg:sticky lg:top-6 lg:self-start">
          <nav
            role="tablist"
            aria-orientation="vertical"
            aria-label="Integrations sub-navigation"
            className="flex flex-col gap-0.5"
          >
            {SUB_TABS.map((tab) => {
              const on = tab.value === active;
              return (
                <button
                  key={tab.value}
                  type="button"
                  role="tab"
                  aria-selected={on}
                  onClick={() => select(tab.value)}
                  className={classNames(
                    // Same row shape SettingsTabsV2 uses: 32px tall,
                    // 2px left rail flips to oz2-acc on the active
                    // item so horizontal text alignment stays steady.
                    "group relative inline-flex h-8 items-center gap-2.5 rounded-[6px] border-l-2 pl-3 pr-3 text-left text-[13px] font-medium transition-colors",
                    on
                      ? "border-oz2-acc bg-oz2-acc-soft text-oz2-acc-text"
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
                </button>
              );
            })}
          </nav>
        </aside>

        <div className="min-w-0">
          {active === "flow" && <FlowExportsSectionV2 />}
          {active === "activity" && <ActivityExportersSectionV2 />}
          {active === "mdm" && <MDMProvidersSectionV2 />}
          {active === "idp-sync" && <SCIMSetupSectionV2 />}
        </div>
      </div>
    </div>
  );
}
