"use client";

import { useEffect, useState } from "react";

import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";
import OzPill from "@/components/v2/OzPill";
import OzShell from "@/components/v2/OzShell";
import OzSidebar, { type OzSidebarSection } from "@/components/v2/OzSidebar";
import OzStatusDot from "@/components/v2/OzStatusDot";
import OzThemeToggle from "@/components/v2/OzThemeToggle";
import OzTopbar, { OzBreadcrumb } from "@/components/v2/OzTopbar";

// Internal preview page for the v2 dashboard redesign — operators
// don't navigate here; it's a dev surface to validate the v2 shell +
// primitives end-to-end against the design handoff.
//
// Reach via /v2-preview when running `npm run dev`. Not linked from
// anywhere on purpose — when the migration is far enough along that
// the shell is adopted, this page will be deleted.

const ico = (path: React.ReactNode) => (
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

const icons = {
  home: ico(
    <>
      <path d="M3 11.5 12 4l9 7.5" />
      <path d="M5 10v10h14V10" />
    </>,
  ),
  peer: ico(
    <>
      <rect x={3} y={4} width={18} height={12} rx={2} />
      <path d="M8 20h8M12 16v4" />
    </>,
  ),
  network: ico(
    <>
      <circle cx={12} cy={5} r={2} />
      <circle cx={6} cy={19} r={2} />
      <circle cx={18} cy={19} r={2} />
      <path d="M12 7v3M12 10l-5 7M12 10l5 7" />
    </>,
  ),
  shield: ico(<path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z" />),
  key: ico(
    <>
      <circle cx={8} cy={15} r={4} />
      <path d="m11 12 9-9 3 3-3 3 2 2-3 3-2-2-3 3" />
    </>,
  ),
  team: ico(
    <>
      <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
      <circle cx={9} cy={7} r={4} />
      <path d="M22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75" />
    </>,
  ),
  activity: ico(<path d="M22 12h-4l-3 9L9 3l-3 9H2" />),
  settings: ico(
    <>
      <circle cx={12} cy={12} r={3} />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </>,
  ),
};

const sections: OzSidebarSection[] = [
  {
    id: "workspace",
    label: "Workspace",
    items: [
      { id: "overview", label: "Overview", icon: icons.home, active: true },
      { id: "peers", label: "Peers", icon: icons.peer },
      { id: "networks", label: "Networks", icon: icons.network },
    ],
  },
  {
    id: "security",
    label: "Security",
    items: [
      { id: "acl", label: "Access Control", icon: icons.shield },
      { id: "keys", label: "Setup Keys", icon: icons.key },
    ],
  },
  {
    id: "identity",
    label: "Identity",
    items: [
      { id: "team", label: "Users & Groups", icon: icons.team },
      { id: "activity", label: "Activity", icon: icons.activity },
    ],
  },
  {
    id: "system",
    label: "System",
    items: [{ id: "settings", label: "Settings", icon: icons.settings }],
  },
];

export default function V2PreviewPage() {
  const [theme, setTheme] = useState<"light" | "dark">("light");

  useEffect(() => {
    const root = document.documentElement;
    if (theme === "dark") {
      root.classList.add("dark");
    } else {
      root.classList.remove("dark");
    }
  }, [theme]);

  return (
    <OzShell
      sidebar={
        <OzSidebar
          brand={
            <span className="font-sans text-[17px] font-semibold tracking-tight text-oz2-text">
              open<span className="font-bold text-oz2-acc">Z</span>ro
            </span>
          }
          sections={sections}
          footer={
            <div className="flex items-center gap-2.5">
              <span className="grid h-7 w-7 place-items-center rounded-full bg-oz2-acc-soft text-[12px] font-semibold text-oz2-acc-text">
                KR
              </span>
              <div className="min-w-0 flex-1">
                <p className="truncate text-[12.5px] font-medium text-oz2-text">
                  Alice Lin
                </p>
                <p className="truncate text-[11px] text-oz2-text-muted">
                  prod-admin
                </p>
              </div>
            </div>
          }
        />
      }
      topbar={
        <OzTopbar
          left={
            <OzBreadcrumb
              segments={[
                { label: "Workspace" },
                { label: "Preview · v2 Primitives" },
              ]}
            />
          }
          right={
            <>
              <OzThemeToggle
                theme={theme}
                onToggle={() =>
                  setTheme(theme === "dark" ? "light" : "dark")
                }
              />
              <span className="grid h-7 w-7 place-items-center rounded-full bg-oz2-acc-soft text-[11px] font-semibold text-oz2-acc-text">
                KR
              </span>
            </>
          }
        />
      }
    >
      <div className="mx-auto max-w-5xl space-y-8 p-8">
        <header className="space-y-2">
          <p className="font-mono text-[11px] uppercase tracking-widest text-oz2-text-faint">
            internal · dashboard redesign
          </p>
          <h1 className="text-[22px] font-semibold tracking-tight">
            v2 Shell + Primitives Preview
          </h1>
          <p className="text-[13px] text-oz2-text-muted">
            Sidebar, topbar, theme toggle and the four primitives rendered
            against the warm-paper / dark-violet tokens.
          </p>
        </header>

        <OzCard>
          <h2 className="mb-4 font-mono text-[11px] uppercase tracking-widest text-oz2-text-faint">
            OzButton
          </h2>
          <div className="flex flex-wrap items-center gap-3">
            <OzButton variant="default">Default</OzButton>
            <OzButton variant="primary">Primary</OzButton>
            <OzButton variant="ghost">Ghost</OzButton>
            <OzButton variant="default" disabled>
              Disabled
            </OzButton>
            <OzButton variant="primary" disabled>
              Disabled primary
            </OzButton>
          </div>
        </OzCard>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-4">
          {[
            { key: "Peers online", value: "128", sub: "last 5 min" },
            { key: "Active sessions", value: "42", sub: "across 8 networks" },
            { key: "Throughput 24h", value: "1.4 TB", sub: "+12% vs prev" },
            { key: "Compliant peers", value: "94%", sub: "120/128" },
          ].map((kpi) => (
            <OzCard key={kpi.key}>
              <p className="mb-1 font-mono text-[11px] uppercase tracking-widest text-oz2-text-faint">
                {kpi.key}
              </p>
              <p className="text-[22px] font-semibold tracking-tight">
                {kpi.value}
              </p>
              <p className="text-[11.5px] text-oz2-text-muted">{kpi.sub}</p>
            </OzCard>
          ))}
        </div>

        <OzCard>
          <h2 className="mb-4 font-mono text-[11px] uppercase tracking-widest text-oz2-text-faint">
            OzPill + OzStatusDot
          </h2>
          <div className="space-y-4">
            <div className="flex flex-wrap items-center gap-2">
              <OzPill variant="default">default</OzPill>
              <OzPill variant="acc">accent</OzPill>
              <OzPill variant="ok">ok</OzPill>
              <OzPill variant="warn">warning</OzPill>
              <OzPill variant="err">error</OzPill>
            </div>
            <div className="space-y-2 text-[13px]">
              <div className="flex items-center gap-3">
                <OzStatusDot status="on" />
                <span>Online · with halo</span>
              </div>
              <div className="flex items-center gap-3">
                <OzStatusDot status="warn" />
                <span>Degraded · with halo</span>
              </div>
              <div className="flex items-center gap-3">
                <OzStatusDot status="off" />
                <span className="text-oz2-text-muted">
                  Offline · no halo
                </span>
              </div>
            </div>
          </div>
        </OzCard>

        <OzCard>
          <h2 className="mb-4 font-mono text-[11px] uppercase tracking-widest text-oz2-text-faint">
            Surface + text spectrum
          </h2>
          <div className="grid grid-cols-3 gap-3 text-[12px]">
            <div className="rounded-oz2-input border border-oz2-border-soft bg-oz2-bg p-3">
              bg
            </div>
            <div className="rounded-oz2-input border border-oz2-border-soft bg-oz2-bg-elev p-3">
              bg-elev
            </div>
            <div className="rounded-oz2-input border border-oz2-border-soft bg-oz2-bg-soft p-3">
              bg-soft
            </div>
            <div className="rounded-oz2-input border border-oz2-border-soft bg-oz2-bg-sunken p-3">
              bg-sunken
            </div>
            <div className="rounded-oz2-input border border-oz2-border-soft bg-oz2-surface p-3">
              surface
            </div>
            <div className="rounded-oz2-input border border-oz2-border-soft bg-oz2-surface-2 p-3">
              surface-2
            </div>
          </div>
          <div className="mt-4 grid grid-cols-2 gap-3 text-[12px]">
            <div className="text-oz2-text">text — primary ink</div>
            <div className="text-oz2-text-2">text-2 — secondary</div>
            <div className="text-oz2-text-muted">text-muted — tertiary</div>
            <div className="text-oz2-text-faint">text-faint — hints</div>
          </div>
        </OzCard>
      </div>
    </OzShell>
  );
}
