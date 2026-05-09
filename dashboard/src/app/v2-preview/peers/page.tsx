"use client";

import { useEffect, useMemo, useState } from "react";

import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";
import OzPill from "@/components/v2/OzPill";
import OzShell from "@/components/v2/OzShell";
import OzSidebar, { type OzSidebarSection } from "@/components/v2/OzSidebar";
import OzStatusDot from "@/components/v2/OzStatusDot";
import OzThemeToggle from "@/components/v2/OzThemeToggle";
import OzTopbar, { OzBreadcrumb } from "@/components/v2/OzTopbar";

// ─── Mock peer data ────────────────────────────────────────────────────────
// Realistic shape (status, OS, group, IP) so the table exercises every
// cell type. Replace with real PeersProvider data when this design
// migrates to /peers.
//
// Status mix: 6 online, 1 idle (warn), 2 disconnected — exercises all
// three OzStatusDot states.

interface MockPeer {
  id: string;
  name: string;
  ip: string;
  os: string;
  osVersion: string;
  group: string;
  lastSeen: string;
  version: string;
  status: "on" | "warn" | "off";
}

const peers: MockPeer[] = [
  {
    id: "1",
    name: "kleber-laptop",
    ip: "100.80.1.42",
    os: "macOS",
    osVersion: "15.3",
    group: "developers",
    lastSeen: "Just now",
    version: "0.53.1-alpha.50",
    status: "on",
  },
  {
    id: "2",
    name: "andre-mbp",
    ip: "100.80.1.51",
    os: "macOS",
    osVersion: "14.7",
    group: "developers",
    lastSeen: "2 min ago",
    version: "0.53.1-alpha.49",
    status: "on",
  },
  {
    id: "3",
    name: "routing-peer-br-1",
    ip: "100.80.2.10",
    os: "Rocky Linux",
    osVersion: "9.5",
    group: "routing-peers-br",
    lastSeen: "Just now",
    version: "0.53.1-alpha.50",
    status: "on",
  },
  {
    id: "4",
    name: "routing-peer-br-2",
    ip: "100.80.2.11",
    os: "Rocky Linux",
    osVersion: "9.5",
    group: "routing-peers-br",
    lastSeen: "Just now",
    version: "0.53.1-alpha.50",
    status: "on",
  },
  {
    id: "5",
    name: "matera-prod-jumphost",
    ip: "100.80.3.5",
    os: "Ubuntu",
    osVersion: "24.04",
    group: "matera-jumphosts",
    lastSeen: "12 min ago",
    version: "0.53.1-alpha.48",
    status: "warn",
  },
  {
    id: "6",
    name: "ci-runner-01",
    ip: "100.80.4.1",
    os: "Ubuntu",
    osVersion: "24.04",
    group: "ci",
    lastSeen: "1h ago",
    version: "0.53.1-alpha.47",
    status: "off",
  },
  {
    id: "7",
    name: "ana-workstation",
    ip: "100.80.1.18",
    os: "Windows",
    osVersion: "11 23H2",
    group: "developers",
    lastSeen: "Just now",
    version: "0.53.1-alpha.50",
    status: "on",
  },
  {
    id: "8",
    name: "old-vpn-gateway",
    ip: "100.80.5.99",
    os: "Debian",
    osVersion: "11",
    group: "deprecated",
    lastSeen: "4d ago",
    version: "0.53.1-alpha.21",
    status: "off",
  },
  {
    id: "9",
    name: "felipe-laptop",
    ip: "100.80.1.77",
    os: "macOS",
    osVersion: "15.2",
    group: "developers",
    lastSeen: "Just now",
    version: "0.53.1-alpha.50",
    status: "on",
  },
];

// ─── Sidebar config (peers active here) ────────────────────────────────────

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
  search: ico(
    <>
      <circle cx={11} cy={11} r={7} />
      <path d="m20 20-3.5-3.5" />
    </>,
  ),
  plus: (
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
      <path d="M12 5v14M5 12h14" />
    </svg>
  ),
};

const sections: OzSidebarSection[] = [
  {
    id: "workspace",
    label: "Workspace",
    items: [
      { id: "overview", label: "Overview", icon: icons.home },
      { id: "peers", label: "Peers", icon: icons.peer, active: true },
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

// ─── Page ───────────────────────────────────────────────────────────────────

export default function V2PeersPreview() {
  const [theme, setTheme] = useState<"light" | "dark">("light");
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<
    "all" | "on" | "warn" | "off"
  >("all");

  useEffect(() => {
    const root = document.documentElement;
    if (theme === "dark") root.classList.add("dark");
    else root.classList.remove("dark");
  }, [theme]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return peers.filter((p) => {
      const statusOk = statusFilter === "all" || p.status === statusFilter;
      const searchOk =
        !q ||
        p.name.toLowerCase().includes(q) ||
        p.ip.includes(q) ||
        p.group.toLowerCase().includes(q);
      return statusOk && searchOk;
    });
  }, [search, statusFilter]);

  const counts = useMemo(
    () => ({
      online: peers.filter((p) => p.status === "on").length,
      idle: peers.filter((p) => p.status === "warn").length,
      offline: peers.filter((p) => p.status === "off").length,
      total: peers.length,
    }),
    [],
  );

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
                  Kleber Rocha
                </p>
                <p className="truncate text-[11px] text-oz2-text-muted">
                  cora-admin
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
              segments={[{ label: "Workspace" }, { label: "Peers" }]}
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
      <div className="space-y-6 p-8">
        {/* Page title row */}
        <header className="flex items-center justify-between gap-4">
          <div>
            <h1 className="text-[22px] font-semibold tracking-tight">Peers</h1>
            <p className="mt-1 text-[13px] text-oz2-text-muted">
              All machines and devices connected to your private mesh.
            </p>
          </div>
          <OzButton variant="primary">
            <span className="inline-flex h-3.5 w-3.5 items-center justify-center">
              {icons.plus}
            </span>
            Add peer
          </OzButton>
        </header>

        {/* KPI band */}
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <KpiCard label="Online" value={String(counts.online)} dot="on" />
          <KpiCard label="Idle" value={String(counts.idle)} dot="warn" />
          <KpiCard
            label="Disconnected"
            value={String(counts.offline)}
            dot="off"
          />
          <KpiCard label="Total peers" value={String(counts.total)} />
        </div>

        {/* Toolbar */}
        <OzCard flush>
          <div className="flex flex-wrap items-center gap-3 border-b border-oz2-border-soft px-[18px] py-3">
            <div className="inline-flex h-[34px] flex-1 min-w-[220px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3">
              <span className="text-oz2-text-faint">{icons.search}</span>
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search by name, IP, group…"
                className="h-full flex-1 border-0 bg-transparent text-[13px] outline-none placeholder:text-oz2-text-faint"
              />
            </div>
            <div className="flex items-center gap-1.5">
              {(
                [
                  { id: "all", label: "All" },
                  { id: "on", label: "Online" },
                  { id: "warn", label: "Idle" },
                  { id: "off", label: "Disconnected" },
                ] as const
              ).map((f) => (
                <button
                  key={f.id}
                  type="button"
                  onClick={() => setStatusFilter(f.id)}
                  className={
                    "inline-flex h-[28px] items-center rounded-full border px-3 text-[12px] font-medium transition-colors " +
                    (statusFilter === f.id
                      ? "border-transparent bg-oz2-acc-soft text-oz2-acc-text"
                      : "border-oz2-border bg-oz2-surface text-oz2-text-2 hover:bg-oz2-hover")
                  }
                >
                  {f.label}
                </button>
              ))}
            </div>
          </div>

          {/* Table */}
          <table className="w-full text-[13px]">
            <thead>
              <tr className="text-left">
                <Th>Name</Th>
                <Th>IP address</Th>
                <Th>OS</Th>
                <Th>Group</Th>
                <Th>Last seen</Th>
                <Th>Version</Th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((p) => (
                <tr
                  key={p.id}
                  className="group border-t border-oz2-border-soft transition-colors hover:bg-oz2-hover"
                >
                  <Td>
                    <div className="flex items-center gap-2.5">
                      <OzStatusDot status={p.status} />
                      <span className="font-medium text-oz2-text">
                        {p.name}
                      </span>
                    </div>
                  </Td>
                  <Td>
                    <span className="font-mono text-[12px] text-oz2-text-2">
                      {p.ip}
                    </span>
                  </Td>
                  <Td>
                    <span className="text-oz2-text-2">{p.os}</span>
                    <span className="ml-1.5 text-oz2-text-faint">
                      {p.osVersion}
                    </span>
                  </Td>
                  <Td>
                    <OzPill variant="default">{p.group}</OzPill>
                  </Td>
                  <Td>
                    <span className="text-oz2-text-muted">{p.lastSeen}</span>
                  </Td>
                  <Td>
                    <span className="font-mono text-[11.5px] text-oz2-text-faint">
                      {p.version}
                    </span>
                  </Td>
                </tr>
              ))}
              {filtered.length === 0 && (
                <tr>
                  <td
                    colSpan={6}
                    className="px-[18px] py-12 text-center text-oz2-text-muted"
                  >
                    No peers match your filter.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </OzCard>
      </div>
    </OzShell>
  );
}

// Local helpers — kept inline rather than extracted into v2/ until
// the same pattern shows up on a second screen and we can see what
// the right abstraction is.

function KpiCard({
  label,
  value,
  dot,
}: {
  label: string;
  value: string;
  dot?: "on" | "warn" | "off";
}) {
  return (
    <OzCard>
      <div className="mb-1 flex items-center gap-2">
        {dot && <OzStatusDot status={dot} />}
        <p className="font-mono text-[11px] uppercase tracking-widest text-oz2-text-faint">
          {label}
        </p>
      </div>
      <p className="text-[22px] font-semibold tracking-tight">{value}</p>
    </OzCard>
  );
}

function Th({ children }: { children: React.ReactNode }) {
  return (
    <th className="px-[18px] py-3 font-mono text-[10.5px] font-semibold uppercase tracking-widest text-oz2-text-faint">
      {children}
    </th>
  );
}

function Td({ children }: { children: React.ReactNode }) {
  return <td className="px-[18px] py-3">{children}</td>;
}
