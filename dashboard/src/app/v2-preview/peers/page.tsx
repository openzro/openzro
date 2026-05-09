"use client";

import { useEffect, useMemo, useRef, useState } from "react";

import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";
import OzPill from "@/components/v2/OzPill";
import OzShell from "@/components/v2/OzShell";
import OzSidebar, { type OzSidebarSection } from "@/components/v2/OzSidebar";
import OzStatusDot from "@/components/v2/OzStatusDot";
import OzThemeToggle from "@/components/v2/OzThemeToggle";
import OzTopbar, { OzBreadcrumb } from "@/components/v2/OzTopbar";

// ─── Mock peer data ────────────────────────────────────────────────────────
// Realistic shape (status, OS, country, groups, IP) so the table
// exercises every cell type. Replace with real PeersProvider data
// when this design migrates to /peers.

interface MockPeer {
  id: string;
  name: string;
  dnsLabel: string;
  ip: string;
  publicIp: string;
  os: string;
  osVersion: string;
  serial?: string;
  groups: string[];
  country: string; // ISO 2-letter
  region: string;
  lastSeen: string;
  version: string;
  status: "on" | "warn" | "off";
}

const peers: MockPeer[] = [
  {
    id: "1",
    name: "kleber-laptop",
    dnsLabel: "kleber-laptop.cora.zero.mesh",
    ip: "100.80.1.42",
    publicIp: "201.17.42.18",
    os: "macOS",
    osVersion: "15.3",
    serial: "C02XL0AAJG5L",
    groups: ["developers", "all"],
    country: "BR",
    region: "São Paulo, BR",
    lastSeen: "Just now",
    version: "0.53.1-alpha.50",
    status: "on",
  },
  {
    id: "2",
    name: "andre-mbp",
    dnsLabel: "andre-mbp.cora.zero.mesh",
    ip: "100.80.1.51",
    publicIp: "201.17.42.32",
    os: "macOS",
    osVersion: "14.7",
    serial: "C02WK0AAJG5L",
    groups: ["developers", "all"],
    country: "BR",
    region: "São Paulo, BR",
    lastSeen: "2 min ago",
    version: "0.53.1-alpha.49",
    status: "on",
  },
  {
    id: "3",
    name: "routing-peer-br-1",
    dnsLabel: "routing-peer-br-1.cora.zero.mesh",
    ip: "100.80.2.10",
    publicIp: "34.95.120.4",
    os: "Rocky Linux",
    osVersion: "9.5",
    groups: ["routing-peers-br", "production", "all"],
    country: "BR",
    region: "São Paulo, BR",
    lastSeen: "Just now",
    version: "0.53.1-alpha.50",
    status: "on",
  },
  {
    id: "4",
    name: "routing-peer-br-2",
    dnsLabel: "routing-peer-br-2.cora.zero.mesh",
    ip: "100.80.2.11",
    publicIp: "34.95.120.5",
    os: "Rocky Linux",
    osVersion: "9.5",
    groups: ["routing-peers-br", "production", "all"],
    country: "BR",
    region: "São Paulo, BR",
    lastSeen: "Just now",
    version: "0.53.1-alpha.50",
    status: "on",
  },
  {
    id: "5",
    name: "matera-prod-jumphost",
    dnsLabel: "matera-jumphost.cora.zero.mesh",
    ip: "100.80.3.5",
    publicIp: "52.169.72.18",
    os: "Ubuntu",
    osVersion: "24.04",
    groups: ["matera-jumphosts", "production", "all"],
    country: "US",
    region: "Iowa, US",
    lastSeen: "12 min ago",
    version: "0.53.1-alpha.48",
    status: "warn",
  },
  {
    id: "6",
    name: "ci-runner-01",
    dnsLabel: "ci-runner-01.cora.zero.mesh",
    ip: "100.80.4.1",
    publicIp: "35.224.18.9",
    os: "Ubuntu",
    osVersion: "24.04",
    groups: ["ci", "all"],
    country: "US",
    region: "Iowa, US",
    lastSeen: "1h ago",
    version: "0.53.1-alpha.47",
    status: "off",
  },
  {
    id: "7",
    name: "ana-workstation",
    dnsLabel: "ana-workstation.cora.zero.mesh",
    ip: "100.80.1.18",
    publicIp: "201.17.42.71",
    os: "Windows",
    osVersion: "11 23H2",
    serial: "WIN-AN12-3456",
    groups: ["developers", "designers", "all"],
    country: "BR",
    region: "São Paulo, BR",
    lastSeen: "Just now",
    version: "0.53.1-alpha.50",
    status: "on",
  },
  {
    id: "8",
    name: "old-vpn-gateway",
    dnsLabel: "old-vpn-gateway.cora.zero.mesh",
    ip: "100.80.5.99",
    publicIp: "85.214.132.18",
    os: "Debian",
    osVersion: "11",
    groups: ["deprecated", "all"],
    country: "DE",
    region: "Frankfurt, DE",
    lastSeen: "4d ago",
    version: "0.53.1-alpha.21",
    status: "off",
  },
  {
    id: "9",
    name: "felipe-laptop",
    dnsLabel: "felipe-laptop.cora.zero.mesh",
    ip: "100.80.1.77",
    publicIp: "201.17.42.94",
    os: "macOS",
    osVersion: "15.2",
    serial: "C02ZL0AAJG5L",
    groups: ["developers", "all"],
    country: "BR",
    region: "Rio de Janeiro, BR",
    lastSeen: "Just now",
    version: "0.53.1-alpha.50",
    status: "on",
  },
];

// All distinct groups (for the Groups filter dropdown)
const allGroups = Array.from(
  new Set(peers.flatMap((p) => p.groups)),
).sort((a, b) => a.localeCompare(b));

// ISO 2-letter → flag emoji using regional indicator letters
function flagEmoji(country: string): string {
  if (!country || country.length !== 2) return "🌐";
  const codePoints = country
    .toUpperCase()
    .split("")
    .map((c) => 127397 + c.charCodeAt(0));
  return String.fromCodePoint(...codePoints);
}

// ─── Sidebar config ────────────────────────────────────────────────────────

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
  chevDown: ico(<path d="m6 9 6 6 6-6" />),
  pin: ico(
    <>
      <path d="M12 13c2 0 4-2 4-4 0-2-1.5-4-4-4S8 7 8 9c0 2 2 4 4 4z" />
      <path d="M12 13v8" />
    </>,
  ),
  globe: ico(
    <>
      <circle cx={12} cy={12} r={9} />
      <path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18" />
    </>,
  ),
  cpu: ico(
    <>
      <rect x={4} y={4} width={16} height={16} rx={2} />
      <rect x={9} y={9} width={6} height={6} />
      <path d="M9 1v3M15 1v3M9 20v3M15 20v3M20 9h3M20 14h3M1 9h3M1 14h3" />
    </>,
  ),
  barcode: ico(
    <>
      <path d="M3 5v14M7 5v14M11 5v14M14 5v14M18 5v14M21 5v14" />
    </>,
  ),
  groupIcon: ico(
    <>
      <circle cx={12} cy={8} r={4} />
      <path d="M4 21a8 8 0 0 1 16 0" />
    </>,
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
  const [groupFilter, setGroupFilter] = useState<string[]>([]);
  const [groupOpen, setGroupOpen] = useState(false);

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
        p.dnsLabel.toLowerCase().includes(q) ||
        p.groups.some((g) => g.toLowerCase().includes(q));
      const groupOk =
        groupFilter.length === 0 ||
        groupFilter.some((g) => p.groups.includes(g));
      return statusOk && searchOk && groupOk;
    });
  }, [search, statusFilter, groupFilter]);

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
                placeholder="Search by name, DNS, IP, group…"
                className="h-full flex-1 border-0 bg-transparent text-[13px] outline-none placeholder:text-oz2-text-faint"
              />
            </div>

            {/* Group dropdown */}
            <GroupFilter
              value={groupFilter}
              onChange={setGroupFilter}
              open={groupOpen}
              onOpenChange={setGroupOpen}
            />

            {/* Status pills */}
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
                <Th>Address</Th>
                <Th>OS</Th>
                <Th>Groups</Th>
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
                    <AddressCell peer={p} />
                  </Td>
                  <Td>
                    <OSCell peer={p} />
                  </Td>
                  <Td>
                    <GroupsCell peer={p} />
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
                    colSpan={5}
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

// ─── Cells ─────────────────────────────────────────────────────────────────

function AddressCell({ peer }: { peer: MockPeer }) {
  return (
    <Tip
      content={
        <div className="w-[280px]">
          <TipRow icon={icons.pin} label="Openzro IP" value={peer.ip} />
          <TipRow
            icon={icons.network}
            label="Public IP"
            value={peer.publicIp}
          />
          <TipRow
            icon={icons.globe}
            label="Domain"
            value={peer.dnsLabel}
            mono={false}
          />
          <TipRow
            icon={
              <span className="text-[16px] leading-none">
                {flagEmoji(peer.country)}
              </span>
            }
            label="Region"
            value={peer.region}
            mono={false}
          />
        </div>
      }
    >
      <div className="flex items-center gap-3">
        <span className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-oz2-bg-sunken text-[18px] leading-none">
          {flagEmoji(peer.country)}
        </span>
        <div className="flex min-w-0 flex-col">
          <OzStatusDotInline status={peer.status} name={peer.name} />
          <span className="font-mono text-[11.5px] text-oz2-text-faint">
            {peer.ip}
          </span>
        </div>
      </div>
    </Tip>
  );
}

function OzStatusDotInline({
  status,
  name,
}: {
  status: "on" | "warn" | "off";
  name: string;
}) {
  return (
    <span className="flex items-center gap-2">
      <OzStatusDot status={status} />
      <span className="truncate font-medium text-oz2-text">{name}</span>
    </span>
  );
}

function OSCell({ peer }: { peer: MockPeer }) {
  return (
    <Tip
      content={
        <div className="w-[240px]">
          <TipRow
            icon={icons.cpu}
            label="OS"
            value={`${peer.os} ${peer.osVersion}`}
            mono={false}
          />
          {peer.serial && (
            <TipRow
              icon={icons.barcode}
              label="Serial Number"
              value={peer.serial}
            />
          )}
        </div>
      }
    >
      <span className="inline-flex items-center gap-1.5">
        <span className="text-oz2-text-2">{peer.os}</span>
        <span className="text-oz2-text-faint">{peer.osVersion}</span>
      </span>
    </Tip>
  );
}

function GroupsCell({ peer }: { peer: MockPeer }) {
  const visible = peer.groups.slice(0, 2);
  const overflow = peer.groups.length - visible.length;
  return (
    <Tip
      content={
        <div className="w-[200px]">
          <p className="mb-2 px-3 pt-3 font-mono text-[10.5px] uppercase tracking-widest text-oz2-text-faint">
            Assigned groups
          </p>
          <ul className="space-y-1 px-3 pb-3">
            {peer.groups.map((g) => (
              <li
                key={g}
                className="flex items-center gap-2 text-[12px] text-oz2-text"
              >
                <span className="text-oz2-text-faint">{icons.groupIcon}</span>
                {g}
              </li>
            ))}
          </ul>
        </div>
      }
    >
      <div className="flex items-center gap-1.5">
        {visible.map((g) => (
          <OzPill key={g} variant="default">
            {g}
          </OzPill>
        ))}
        {overflow > 0 && (
          <OzPill variant="default">+{overflow}</OzPill>
        )}
      </div>
    </Tip>
  );
}

// ─── Tooltip helper (lightweight, hover-driven) ────────────────────────────
// Inline implementation rather than wiring the project's Radix Tooltip
// — this is a preview surface and the simpler box keeps the page
// dependency-free. When the migration touches /peers the real
// components import @components/Tooltip / FullTooltip directly.

function Tip({
  children,
  content,
}: {
  children: React.ReactNode;
  content: React.ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  return (
    <span
      className="relative inline-flex"
      onMouseEnter={() => {
        if (closeTimer.current) clearTimeout(closeTimer.current);
        setOpen(true);
      }}
      onMouseLeave={() => {
        closeTimer.current = setTimeout(() => setOpen(false), 80);
      }}
    >
      {children}
      {open && (
        <span
          role="tooltip"
          className="absolute left-0 top-full z-30 mt-2 overflow-hidden rounded-oz2-input border border-oz2-border bg-oz2-bg-elev shadow-oz2-md"
        >
          {content}
        </span>
      )}
    </span>
  );
}

function TipRow({
  icon,
  label,
  value,
  mono = true,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="flex items-center justify-between gap-3 border-b border-oz2-border-soft px-3 py-2 text-[12px] last:border-b-0">
      <span className="flex items-center gap-2 text-oz2-text-muted">
        <span className="text-oz2-text-faint">{icon}</span>
        {label}
      </span>
      <span
        className={
          (mono ? "font-mono text-[11.5px] " : "text-[12px] ") +
          "text-oz2-text"
        }
      >
        {value}
      </span>
    </div>
  );
}

// ─── Group filter dropdown ─────────────────────────────────────────────────
// Click button → opens panel listing all groups with checkboxes;
// closes on outside click. Same UX as the existing
// GroupFilterSelector but with v2 paint.

function GroupFilter({
  value,
  onChange,
  open,
  onOpenChange,
}: {
  value: string[];
  onChange: (next: string[]) => void;
  open: boolean;
  onOpenChange: (next: boolean) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) onOpenChange(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open, onOpenChange]);

  const toggle = (g: string) => {
    onChange(value.includes(g) ? value.filter((x) => x !== g) : [...value, g]);
  };

  const label =
    value.length === 0
      ? "All groups"
      : value.length === 1
        ? value[0]
        : `${value.length} selected`;

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => onOpenChange(!open)}
        className={
          "inline-flex h-[34px] items-center gap-2 rounded-oz2-input border px-3 text-[13px] font-medium transition-colors " +
          (value.length > 0
            ? "border-transparent bg-oz2-acc-soft text-oz2-acc-text"
            : "border-oz2-border bg-oz2-surface text-oz2-text-2 hover:bg-oz2-hover")
        }
      >
        <span className="text-oz2-text-faint">{icons.groupIcon}</span>
        {label}
        <span className="text-oz2-text-faint">{icons.chevDown}</span>
      </button>
      {open && (
        <div className="absolute left-0 top-full z-30 mt-2 w-[220px] overflow-hidden rounded-oz2-input border border-oz2-border bg-oz2-bg-elev shadow-oz2-md">
          <p className="border-b border-oz2-border-soft px-3 py-2 font-mono text-[10.5px] uppercase tracking-widest text-oz2-text-faint">
            Filter by group
          </p>
          <ul className="max-h-[260px] overflow-y-auto py-1">
            {allGroups.map((g) => {
              const checked = value.includes(g);
              return (
                <li key={g}>
                  <button
                    type="button"
                    onClick={() => toggle(g)}
                    className="flex w-full items-center gap-2 px-3 py-2 text-left text-[12.5px] hover:bg-oz2-hover"
                  >
                    <span
                      className={
                        "grid h-4 w-4 shrink-0 place-items-center rounded border " +
                        (checked
                          ? "border-transparent bg-oz2-acc text-oz2-text-on-acc"
                          : "border-oz2-border bg-oz2-surface")
                      }
                    >
                      {checked && (
                        <svg
                          width={10}
                          height={10}
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth={3}
                          strokeLinecap="round"
                          strokeLinejoin="round"
                        >
                          <path d="m5 12 5 5L20 7" />
                        </svg>
                      )}
                    </span>
                    <span className="flex-1 text-oz2-text">{g}</span>
                  </button>
                </li>
              );
            })}
          </ul>
          {value.length > 0 && (
            <div className="border-t border-oz2-border-soft p-2">
              <button
                type="button"
                onClick={() => onChange([])}
                className="w-full rounded-oz2-input px-3 py-1.5 text-left text-[12px] text-oz2-text-muted hover:bg-oz2-hover hover:text-oz2-text"
              >
                Clear selection
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ─── KPI + table primitives ────────────────────────────────────────────────

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
  return <td className="px-[18px] py-3 align-middle">{children}</td>;
}
