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
import { OSLogo } from "@/modules/peers/PeerOSCell";

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
  userName: string;
  userEmail: string;
  connection: "p2p" | "relay";
  lastSeen: string;
  version: string;
  status: "on" | "warn" | "off";
  // Operational notices — render as pills before the kebab. Most
  // severe wins when multiple apply; loginRequired > approvalPending
  // > expirationDisabled.
  loginRequired?: boolean;
  approvalPending?: boolean;
  expirationDisabled?: boolean;
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
    userName: "Kleber Rocha",
    userEmail: "klinux@gmail.com",
    connection: "p2p",
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
    userName: "André Souza",
    userEmail: "andre@cora.com.br",
    connection: "p2p",
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
    userName: "platform-engineering",
    userEmail: "platform@cora.com.br",
    connection: "p2p",
    lastSeen: "Just now",
    version: "0.53.1-alpha.50",
    status: "on",
    expirationDisabled: true,
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
    userName: "platform-engineering",
    userEmail: "platform@cora.com.br",
    connection: "p2p",
    lastSeen: "Just now",
    version: "0.53.1-alpha.50",
    status: "on",
    expirationDisabled: true,
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
    userName: "matera-svc",
    userEmail: "matera-integration@cora.com.br",
    connection: "relay",
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
    userName: "ci-bot",
    userEmail: "ci@cora.com.br",
    connection: "relay",
    lastSeen: "1h ago",
    version: "0.53.1-alpha.47",
    status: "off",
    loginRequired: true,
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
    userName: "Ana Pereira",
    userEmail: "ana@cora.com.br",
    connection: "p2p",
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
    userName: "(removed user)",
    userEmail: "—",
    connection: "relay",
    lastSeen: "4d ago",
    version: "0.53.1-alpha.21",
    status: "off",
    approvalPending: true,
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
    userName: "Felipe Lima",
    userEmail: "felipe@cora.com.br",
    connection: "p2p",
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
  more: ico(
    <>
      <circle cx={5} cy={12} r={1.4} />
      <circle cx={12} cy={12} r={1.4} />
      <circle cx={19} cy={12} r={1.4} />
    </>,
  ),
  copy: ico(
    <>
      <rect x={9} y={9} width={13} height={13} rx={2} />
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
    </>,
  ),
  block: ico(
    <>
      <circle cx={12} cy={12} r={9} />
      <path d="m5.6 5.6 12.8 12.8" />
    </>,
  ),
  trash: ico(
    <>
      <path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M6 6v14a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2V6" />
    </>,
  ),
  arrow: ico(<path d="m6 9 6 6 6-6" />),
  refresh: ico(
    <>
      <path d="M21 12a9 9 0 1 1-3.5-7.1" />
      <path d="M21 4v5h-5" />
    </>,
  ),
  alert: ico(
    <>
      <path d="M12 9v4M12 17h.01" />
      <path d="M10.3 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
    </>,
  ),
  clock: ico(
    <>
      <circle cx={12} cy={12} r={9} />
      <path d="M12 7v5l3 2" />
    </>,
  ),
  hourglass: ico(
    <>
      <path d="M5 22h14M5 2h14M17 22v-4.17a2 2 0 0 0-.59-1.42L12 12l-4.41 4.41A2 2 0 0 0 7 17.83V22M7 2v4.17c0 .53.21 1.04.59 1.42L12 12l4.41-4.41A2 2 0 0 0 17 6.17V2" />
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
  const [pageSize, setPageSize] = useState<number>(10);
  const [page, setPage] = useState<number>(1);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [refreshing, setRefreshing] = useState<boolean>(false);

  const toggleSelected = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const refreshClick = () => {
    setRefreshing(true);
    setTimeout(() => setRefreshing(false), 600);
  };

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

  // Pagination derived state
  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
  const visiblePage = Math.min(page, totalPages);
  const pageStart = (visiblePage - 1) * pageSize;
  const paginated = filtered.slice(pageStart, pageStart + pageSize);

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
              <OzButton variant="primary">
                <span className="inline-flex h-3.5 w-3.5 items-center justify-center">
                  {icons.plus}
                </span>
                Add peer
              </OzButton>
            </>
          }
        />
      }
    >
      <div className="space-y-6 p-8">
        {/* Page title row */}
        <header>
          <h1 className="text-[22px] font-semibold tracking-tight">Peers</h1>
          <p className="mt-1 text-[13px] text-oz2-text-muted">
            All machines and devices connected to your private mesh.
          </p>
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

        {/* Status tabs (segmented) — primary axis filter */}
        <SegmentedTabs
          value={statusFilter}
          onChange={(v) => {
            setStatusFilter(v);
            setPage(1);
          }}
          options={[
            {
              id: "all",
              label: "All peers",
              count: peers.length,
            },
            {
              id: "on",
              label: "Online",
              count: counts.online,
            },
            {
              id: "warn",
              label: "Idle",
              count: counts.idle,
            },
            {
              id: "off",
              label: "Disconnected",
              count: counts.offline,
            },
          ]}
        />

        {/* Toolbar + Table card */}
        <OzCard flush>
          <div className="flex flex-wrap items-center gap-3 border-b border-oz2-border-soft px-[18px] py-3">
            <div className="inline-flex h-[34px] flex-1 min-w-[220px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3">
              <span className="text-oz2-text-faint">{icons.search}</span>
              <input
                value={search}
                onChange={(e) => {
                  setSearch(e.target.value);
                  setPage(1);
                }}
                placeholder="Search by name, DNS, IP, group…"
                className="h-full flex-1 border-0 bg-transparent text-[13px] outline-none placeholder:text-oz2-text-faint"
              />
            </div>

            <GroupFilter
              value={groupFilter}
              onChange={(v) => {
                setGroupFilter(v);
                setPage(1);
              }}
              open={groupOpen}
              onOpenChange={setGroupOpen}
            />

            <PageSizeCombobox value={pageSize} onChange={setPageSize} />

            <button
              type="button"
              onClick={refreshClick}
              aria-label="Refresh peers"
              className="grid h-[34px] w-[34px] place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 hover:border-oz2-border-strong hover:bg-oz2-hover"
            >
              <span className={refreshing ? "animate-spin text-oz2-acc" : ""}>
                {icons.refresh}
              </span>
            </button>
          </div>

          {/* Bulk-action band — appears when any row is selected */}
          {selected.size > 0 && (
            <div className="flex items-center justify-between gap-3 border-b border-oz2-border-soft bg-oz2-acc-soft px-[18px] py-2.5 text-[12.5px]">
              <span className="font-medium text-oz2-acc-text">
                {selected.size} {selected.size === 1 ? "peer" : "peers"} selected
              </span>
              <div className="flex items-center gap-2">
                <OzButton variant="default">Add to group</OzButton>
                <OzButton variant="default">Block</OzButton>
                <OzButton variant="default" className="text-oz2-err">
                  Delete
                </OzButton>
                <button
                  type="button"
                  onClick={() => setSelected(new Set())}
                  className="rounded-oz2-input px-2 py-1 text-[12px] text-oz2-text-muted hover:bg-oz2-hover hover:text-oz2-text"
                >
                  Clear
                </button>
              </div>
            </div>
          )}

          {/* Table */}
          <table className="w-full text-[13px]">
            <thead>
              <tr className="bg-oz2-bg-sunken text-left">
                <Th aria-label="Select" className="w-[44px]">
                  <OzCheckbox
                    checked={
                      paginated.length > 0 &&
                      paginated.every((p) => selected.has(p.id))
                    }
                    indeterminate={
                      paginated.some((p) => selected.has(p.id)) &&
                      !paginated.every((p) => selected.has(p.id))
                    }
                    onChange={(checked) => {
                      setSelected((prev) => {
                        const next = new Set(prev);
                        if (checked) {
                          paginated.forEach((p) => next.add(p.id));
                        } else {
                          paginated.forEach((p) => next.delete(p.id));
                        }
                        return next;
                      });
                    }}
                    aria-label="Select all visible"
                  />
                </Th>
                <Th>Name</Th>
                <Th>Address</Th>
                <Th>Group</Th>
                <Th>OS</Th>
                <Th>Version</Th>
                <Th>Connection</Th>
                <Th>Last seen</Th>
                <Th>Notice</Th>
                <Th aria-label="Actions" className="w-[40px]">{""}</Th>
              </tr>
            </thead>
            <tbody>
              {paginated.map((p) => (
                <tr
                  key={p.id}
                  className={
                    "group border-t border-oz2-border-soft transition-colors " +
                    (selected.has(p.id) ? "bg-oz2-acc-soft/40" : "hover:bg-oz2-hover")
                  }
                >
                  <Td>
                    <OzCheckbox
                      checked={selected.has(p.id)}
                      onChange={() => toggleSelected(p.id)}
                      aria-label={`Select ${p.name}`}
                    />
                  </Td>
                  <Td>
                    <NameCell peer={p} />
                  </Td>
                  <Td>
                    <AddressCell peer={p} />
                  </Td>
                  <Td>
                    <GroupsCell peer={p} />
                  </Td>
                  <Td>
                    <OSCell peer={p} />
                  </Td>
                  <Td>
                    <span className="font-mono text-[11.5px] text-oz2-text-faint">
                      {p.version}
                    </span>
                  </Td>
                  <Td>
                    <ConnectionPill connection={p.connection} />
                  </Td>
                  <Td>
                    <span className="whitespace-nowrap text-oz2-text-muted">
                      {p.lastSeen}
                    </span>
                  </Td>
                  <Td>
                    <NoticeCell peer={p} />
                  </Td>
                  <Td>
                    <RowKebab peer={p} />
                  </Td>
                </tr>
              ))}
              {paginated.length === 0 && (
                <tr>
                  <td
                    colSpan={10}
                    className="px-[18px] py-12 text-center text-oz2-text-muted"
                  >
                    No peers match your filter.
                  </td>
                </tr>
              )}
            </tbody>
          </table>

          {/* Pagination footer */}
          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-oz2-border-soft bg-oz2-bg-sunken px-[18px] py-3 text-[12.5px]">
            <span className="text-oz2-text-muted">
              {filtered.length === 0
                ? "0 peers"
                : `Showing ${pageStart + 1}–${Math.min(
                    pageStart + pageSize,
                    filtered.length,
                  )} of ${filtered.length}`}
            </span>
            <Pager
              page={visiblePage}
              totalPages={totalPages}
              onChange={setPage}
            />
          </div>
        </OzCard>
      </div>
    </OzShell>
  );
}

// ─── Cells ─────────────────────────────────────────────────────────────────

function NameCell({ peer }: { peer: MockPeer }) {
  // Mirror production PeerNameCell: peer name on top with the
  // online dot, user email/name below in a lighter weight. Two
  // lines is enough — anything else (dns_label, public IP, region)
  // belongs in the Address cell next to it.
  const displayUser = peer.userEmail && peer.userEmail !== "—"
    ? peer.userEmail
    : peer.userName;
  return (
    <div className="flex min-w-0 flex-col">
      <span className="flex items-center gap-2">
        <OzStatusDot status={peer.status} />
        <span className="truncate font-medium text-oz2-text">
          {peer.name}
        </span>
      </span>
      <span className="truncate pl-[16px] text-[11.5px] text-oz2-text-muted">
        {displayUser}
      </span>
    </div>
  );
}

function AddressCell({ peer }: { peer: MockPeer }) {
  // Mirror production PeerAddressCell: country flag halo on the
  // left, dns_label on top, openZro IP below in mono. Tooltip
  // carries the network info (public IP, region) so the cell stays
  // dense.
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
      <div className="flex min-w-0 items-center gap-3">
        <span className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-oz2-bg-sunken text-[14px] leading-none">
          {flagEmoji(peer.country)}
        </span>
        <div className="flex min-w-0 flex-col">
          <span className="truncate text-[13px] text-oz2-text">
            {peer.dnsLabel}
          </span>
          <span className="truncate font-mono text-[11.5px] text-oz2-text-muted">
            {peer.ip}
          </span>
        </div>
      </div>
    </Tip>
  );
}

function ConnectionPill({ connection }: { connection: "p2p" | "relay" }) {
  return (
    <OzPill variant={connection === "p2p" ? "ok" : "default"}>
      {connection === "p2p" ? "P2P" : "Relay"}
    </OzPill>
  );
}

// Render up to one notice pill per peer (most-severe wins). Order:
// loginRequired > approvalPending > expirationDisabled. Empty cell
// when no notice applies — most peers fall here, so the column reads
// as "exception space" not "always populated".
function NoticeCell({ peer }: { peer: MockPeer }) {
  if (peer.loginRequired) {
    return (
      <OzPill variant="err">
        <span className="opacity-80">{icons.alert}</span>
        Login required
      </OzPill>
    );
  }
  if (peer.approvalPending) {
    return (
      <OzPill variant="warn">
        <span className="opacity-80">{icons.clock}</span>
        Approval pending
      </OzPill>
    );
  }
  if (peer.expirationDisabled) {
    return (
      <OzPill variant="default">
        <span className="opacity-70">{icons.hourglass}</span>
        Expiration disabled
      </OzPill>
    );
  }
  return null;
}

// Tri-state checkbox: checked, unchecked, indeterminate. The
// `indeterminate` visual is owned via a horizontal bar instead of
// the check glyph (matching shadcn / Radix pattern). Keyboard +
// space-to-toggle work via the underlying input.
function OzCheckbox({
  checked,
  indeterminate,
  onChange,
  ...props
}: {
  checked: boolean;
  indeterminate?: boolean;
  onChange: (checked: boolean) => void;
} & Omit<React.InputHTMLAttributes<HTMLInputElement>, "checked" | "onChange" | "type">) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = !!indeterminate && !checked;
  }, [indeterminate, checked]);

  const showFill = checked || indeterminate;

  return (
    <label className="inline-flex h-4 w-4 shrink-0 cursor-pointer items-center justify-center">
      <input
        ref={ref}
        type="checkbox"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        className="peer sr-only"
        {...props}
      />
      <span
        aria-hidden="true"
        className={
          "grid h-4 w-4 place-items-center rounded border transition-colors " +
          (showFill
            ? "border-transparent bg-oz2-acc text-oz2-text-on-acc"
            : "border-oz2-border bg-oz2-surface peer-hover:border-oz2-border-strong") +
          " peer-focus-visible:ring-2 peer-focus-visible:ring-oz2-acc peer-focus-visible:ring-offset-2 peer-focus-visible:ring-offset-oz2-bg"
        }
      >
        {checked && !indeterminate && (
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
        {indeterminate && !checked && (
          <span className="h-[2px] w-[8px] rounded-full bg-oz2-text-on-acc" />
        )}
      </span>
    </label>
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
      <span
        className="inline-flex h-6 w-6 items-center justify-center grayscale brightness-[100%] contrast-[40%]"
        aria-label={`${peer.os} ${peer.osVersion}`}
      >
        <OSLogo os={peer.os} />
      </span>
    </Tip>
  );
}

function GroupsCell({ peer }: { peer: MockPeer }) {
  // Mirror the production /peers behaviour — only one chip visible,
  // overflow rolls into a "+N" badge (full list is in the tooltip).
  const visible = peer.groups.slice(0, 1);
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

function Th({
  children,
  className,
  ...props
}: React.ThHTMLAttributes<HTMLTableCellElement> & {
  children: React.ReactNode;
}) {
  return (
    <th
      {...props}
      className={
        "whitespace-nowrap px-[14px] py-[11px] font-mono text-[10.5px] font-semibold uppercase tracking-widest text-oz2-text-muted " +
        (className ?? "")
      }
    >
      {children}
    </th>
  );
}

function Td({ children }: { children: React.ReactNode }) {
  return (
    <td className="px-[14px] py-[13px] align-middle">{children}</td>
  );
}

// ─── SegmentedTabs ─────────────────────────────────────────────────────────
// Border-soft underline across the strip; active tab gets a 2px acc
// underline + heavy text. Each tab includes a count badge (mono).

function SegmentedTabs<T extends string>({
  value,
  onChange,
  options,
}: {
  value: T;
  onChange: (next: T) => void;
  options: { id: T; label: string; count?: number }[];
}) {
  return (
    <div className="flex gap-6 border-b border-oz2-border-soft">
      {options.map((opt) => {
        const active = opt.id === value;
        return (
          <button
            key={opt.id}
            type="button"
            onClick={() => onChange(opt.id)}
            className={
              "relative -mb-px inline-flex h-9 items-center gap-2 border-b-2 px-1 text-[13px] font-medium transition-colors " +
              (active
                ? "border-oz2-acc text-oz2-text"
                : "border-transparent text-oz2-text-muted hover:text-oz2-text")
            }
          >
            {opt.label}
            {typeof opt.count === "number" && (
              <span
                className={
                  "rounded-full px-1.5 py-px font-mono text-[10.5px] font-semibold " +
                  (active
                    ? "bg-oz2-acc-soft text-oz2-acc-text"
                    : "bg-oz2-bg-soft text-oz2-text-faint")
                }
              >
                {opt.count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}

// ─── PageSizeCombobox ──────────────────────────────────────────────────────
// Native-feeling combobox: button shows current size, click opens
// listbox of options. Closes on outside click.

function PageSizeCombobox({
  value,
  onChange,
}: {
  value: number;
  onChange: (next: number) => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const choices = [5, 10, 25, 50];

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="inline-flex h-[34px] items-center gap-1.5 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 text-[13px] font-medium text-oz2-text-2 hover:bg-oz2-hover hover:border-oz2-border-strong"
      >
        <span className="font-mono">{value}</span>
        <span className="text-oz2-text-faint">/ page</span>
        <span className="text-oz2-text-faint">{icons.chevDown}</span>
      </button>
      {open && (
        <div className="absolute left-0 top-full z-30 mt-1 min-w-[110px] overflow-hidden rounded-oz2-input border border-oz2-border bg-oz2-bg-elev shadow-oz2-md">
          <ul className="py-1">
            {choices.map((c) => (
              <li key={c}>
                <button
                  type="button"
                  onClick={() => {
                    onChange(c);
                    setOpen(false);
                  }}
                  className={
                    "flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left text-[12.5px] hover:bg-oz2-hover " +
                    (c === value
                      ? "text-oz2-acc-text"
                      : "text-oz2-text")
                  }
                >
                  <span className="font-mono">{c}</span>
                  <span className="text-oz2-text-faint">/ page</span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

// ─── Pager (prev/next + page numbers) ──────────────────────────────────────

function Pager({
  page,
  totalPages,
  onChange,
}: {
  page: number;
  totalPages: number;
  onChange: (next: number) => void;
}) {
  const canPrev = page > 1;
  const canNext = page < totalPages;
  return (
    <div className="flex items-center gap-1">
      <PagerBtn
        disabled={!canPrev}
        onClick={() => onChange(page - 1)}
        aria-label="Previous page"
      >
        <span className="rotate-90">{icons.chevDown}</span>
      </PagerBtn>
      <span className="px-2 font-mono text-[12px] tabular-nums text-oz2-text-muted">
        {page} / {totalPages}
      </span>
      <PagerBtn
        disabled={!canNext}
        onClick={() => onChange(page + 1)}
        aria-label="Next page"
      >
        <span className="-rotate-90">{icons.chevDown}</span>
      </PagerBtn>
    </div>
  );
}

function PagerBtn({
  children,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      {...props}
      className={
        "grid h-7 w-7 place-items-center rounded-md border border-oz2-border bg-oz2-surface text-oz2-text-2 transition-colors " +
        "hover:border-oz2-border-strong hover:bg-oz2-hover " +
        "disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:border-oz2-border disabled:hover:bg-oz2-surface"
      }
    >
      {children}
    </button>
  );
}

// ─── RowKebab ──────────────────────────────────────────────────────────────
// Per-row action menu: View details / Copy IP / Block / Delete.
// Action handlers no-op in preview — real /peers wires them to the
// existing PeerActionCell logic.

function RowKebab({ peer }: { peer: MockPeer }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  const items: { id: string; label: string; icon: React.ReactNode; danger?: boolean }[] = [
    { id: "view", label: "View details", icon: icons.peer },
    { id: "copy", label: "Copy IP", icon: icons.copy },
    { id: "block", label: "Block peer", icon: icons.block },
    { id: "delete", label: "Delete peer", icon: icons.trash, danger: true },
  ];

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          setOpen(!open);
        }}
        aria-label={`Actions for ${peer.name}`}
        className={
          "grid h-7 w-7 place-items-center rounded-md border text-oz2-text-2 transition-all " +
          (open
            ? "border-oz2-border-strong bg-oz2-hover"
            : "border-transparent opacity-0 group-hover:opacity-100 hover:border-oz2-border-strong hover:bg-oz2-hover")
        }
      >
        {icons.more}
      </button>
      {open && (
        <div className="absolute right-0 top-full z-30 mt-1 w-[180px] overflow-hidden rounded-oz2-input border border-oz2-border bg-oz2-bg-elev shadow-oz2-md">
          <ul className="py-1">
            {items.map((it) => (
              <li key={it.id}>
                <button
                  type="button"
                  onClick={() => setOpen(false)}
                  className={
                    "flex w-full items-center gap-2 px-3 py-2 text-left text-[12.5px] " +
                    (it.danger
                      ? "text-oz2-err hover:bg-oz2-err-bg"
                      : "text-oz2-text hover:bg-oz2-hover")
                  }
                >
                  <span
                    className={
                      it.danger ? "text-oz2-err" : "text-oz2-text-faint"
                    }
                  >
                    {it.icon}
                  </span>
                  {it.label}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
