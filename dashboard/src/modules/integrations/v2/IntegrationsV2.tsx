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
//     "Integrations" + handoff-flavored subtitle + horizontal
//     segmented tabs (DnsTabs-style) + the active section below
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

      <nav
        role="tablist"
        aria-label="Integrations sub-navigation"
        className="inline-flex h-[34px] items-center rounded-oz2-input bg-oz2-bg-sunken p-1 text-oz2-text-muted"
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
                "inline-flex h-full items-center gap-2 whitespace-nowrap rounded-[6px] px-3 text-[13.5px] font-medium transition-colors",
                on
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
            </button>
          );
        })}
      </nav>

      <div className="min-w-0">
        {active === "flow" && <FlowExportsSectionV2 />}
        {active === "activity" && <ActivityExportersSectionV2 />}
        {active === "mdm" && <MDMProvidersSectionV2 />}
        {active === "idp-sync" && <SCIMSetupSectionV2 />}
      </div>
    </div>
  );
}
