"use client";

import { useOidcUser } from "@axa-fr/react-oidc";
import { Modal, ModalTrigger } from "@components/modal/Modal";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@components/Tooltip";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import { Barcode, CpuIcon } from "lucide-react";
import React, { useEffect, useMemo, useRef, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";
import OzPill from "@/components/v2/OzPill";
import OzStatusDot from "@/components/v2/OzStatusDot";
import {
  OzTable,
  OzTableBody,
  OzTableCell,
  OzTableHead,
  OzTableHeader,
  OzTableRow,
} from "@/components/v2/OzTable";
import { useGroups } from "@/contexts/GroupsProvider";
import { useLocalStorage } from "@/hooks/useLocalStorage";
import { Peer } from "@/interfaces/Peer";
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import { OSLogo } from "@/modules/peers/PeerOSCell";
import SetupModal from "@/modules/setup-openzro-modal/SetupModal";

dayjs.extend(relativeTime);

// PeersTableV2 — phase-4.2 v2 paint over real /api/peers data.
// Page-body composition: header + stat badges + toolbar (search,
// status tabs, group filter, page size, refresh) + OzTable.
//
// Out of scope for this commit (deferred to phase 4.3):
// - Bulk select actions (header + per-row checkbox UI present, but
//   the action buttons are visual-only).
// - Refresh wired to SWR mutate (currently a 600ms spinner mock).
// - Row kebab actions (real PeerActionCell / AdmissionBypassModal).
// The Add peer button in the header is intentionally a visual stub
// pending the phase 4.3 wire-up of the legacy AddPeerButton modal.

interface Props {
  peers: Peer[] | undefined;
  isLoading: boolean;
}

type StatusFilter = "all" | "on" | "warn" | "off" | "pending";

// Idle threshold: connected=true peers whose last_seen is older than
// this fall into the v2 "Idle" status. The API exposes no native
// "idle" — this is a UI-only categorization to highlight stale peers
// that the management hasn't yet flipped to disconnected.
const IDLE_THRESHOLD_MS = 5 * 60 * 1000;

function deriveStatus(peer: Peer): "on" | "warn" | "off" {
  if (!peer.connected) return "off";
  const lastSeenMs = new Date(peer.last_seen).getTime();
  if (Number.isFinite(lastSeenMs) && Date.now() - lastSeenMs > IDLE_THRESHOLD_MS) {
    return "warn";
  }
  return "on";
}

export default function PeersTableV2({ peers, isLoading }: Props) {
  const { groups } = useGroups();
  const { mutate } = useSWRConfig();

  // Mount the real Add peer trigger into the V2 topbar's right slot.
  // AddPeerButtonV2 owns its own Modal + SetupModal; the trigger
  // renders as an OzButton primary so it inherits the v2 paint.
  useV2TopbarRight(<AddPeerButtonV2 peerCount={peers?.length ?? 0} />);

  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [groupFilter, setGroupFilter] = useState<string[]>([]);
  const [groupOpen, setGroupOpen] = useState(false);
  const [pageSize, setPageSize] = useState<number>(20);
  const [page, setPage] = useState<number>(1);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [refreshing, setRefreshing] = useState<boolean>(false);

  const all = useMemo(() => peers ?? [], [peers]);

  const counts = useMemo(() => {
    let online = 0;
    let idle = 0;
    let offline = 0;
    let pending = 0;
    for (const p of all) {
      if (p.approval_required) pending += 1;
      const s = deriveStatus(p);
      if (s === "on") online += 1;
      else if (s === "warn") idle += 1;
      else offline += 1;
    }
    return { online, idle, offline, pending, total: all.length };
  }, [all]);

  const allGroupNames = useMemo(() => {
    const names = new Set<string>();
    for (const g of groups ?? []) {
      if (g.name) names.add(g.name);
    }
    return Array.from(names).sort((a, b) => a.localeCompare(b));
  }, [groups]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return all.filter((p) => {
      const status = deriveStatus(p);
      const statusOk =
        statusFilter === "all" ||
        (statusFilter === "pending"
          ? !!p.approval_required
          : statusFilter === status);
      const peerGroups = (p.groups ?? []).map((g) => g.name).filter(Boolean);
      const searchOk =
        !q ||
        p.name?.toLowerCase().includes(q) ||
        p.ip?.includes(q) ||
        p.dns_label?.toLowerCase().includes(q) ||
        peerGroups.some((g) => g.toLowerCase().includes(q)) ||
        p.user?.email?.toLowerCase().includes(q) ||
        p.user?.name?.toLowerCase().includes(q);
      const groupOk =
        groupFilter.length === 0 ||
        groupFilter.some((g) => peerGroups.includes(g));
      return statusOk && searchOk && groupOk;
    });
  }, [all, search, statusFilter, groupFilter]);

  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
  const visiblePage = Math.min(page, totalPages);
  const pageStart = (visiblePage - 1) * pageSize;
  const paginated = filtered.slice(pageStart, pageStart + pageSize);

  const refreshClick = () => {
    setRefreshing(true);
    Promise.all([mutate("/peers"), mutate("/groups"), mutate("/users")])
      .catch(() => {
        // SWR surfaces fetch errors via its own error state; we don't
        // need to alert from the refresh button. Keep the button
        // ready for retry on transient failures.
      })
      .finally(() => setRefreshing(false));
  };

  const toggleSelected = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  return (
    <div className="space-y-6 p-8">
      <header>
        <h1 className="text-[22px] font-semibold tracking-tight">Peers</h1>
        <p className="mt-1 max-w-2xl text-[13px] text-oz2-text-muted">
          A list of all machines and devices connected to your private
          network. Use this view to manage peers. Learn more about{" "}
          <a
            href="https://docs.openzro.io/how-to/add-machines-to-your-network"
            target="_blank"
            rel="noopener noreferrer"
            className="text-oz2-acc-text underline-offset-2 hover:underline"
          >
            adding machines to your network
          </a>
          .
        </p>
      </header>

      <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-[12.5px] text-oz2-text-muted">
        <span className="inline-flex items-center gap-2">
          <OzStatusDot status="on" />
          <span className="font-medium text-oz2-text">{counts.online}</span>
          Online
        </span>
        <span className="inline-flex items-center gap-2">
          <OzStatusDot status="warn" />
          <span className="font-medium text-oz2-text">{counts.idle}</span>
          Idle
        </span>
        <span className="inline-flex items-center gap-2">
          <OzStatusDot status="off" />
          <span className="font-medium text-oz2-text">{counts.offline}</span>
          Offline
        </span>
        <span className="ml-1 inline-flex items-center gap-2 border-l border-oz2-border-soft pl-5">
          <span className="font-medium text-oz2-text">{counts.total}</span>
          Total peers
        </span>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <div className="inline-flex h-[34px] flex-1 min-w-[220px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3">
          <span className="text-oz2-text-faint">{ICONS.search}</span>
          <input
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              setPage(1);
            }}
            placeholder="Search by name, IP, user…"
            className="h-full flex-1 border-0 bg-transparent text-[13px] outline-none placeholder:text-oz2-text-faint"
          />
        </div>

        <SegmentedTabs
          value={statusFilter}
          onChange={(v) => {
            setStatusFilter(v);
            setPage(1);
          }}
          options={[
            { id: "all", label: "All", count: counts.total },
            { id: "on", label: "Online", count: counts.online },
            { id: "warn", label: "Idle", count: counts.idle },
            { id: "off", label: "Offline", count: counts.offline },
            { id: "pending", label: "Pending", count: counts.pending },
          ]}
        />

        <GroupFilter
          value={groupFilter}
          onChange={(v) => {
            setGroupFilter(v);
            setPage(1);
          }}
          allGroups={allGroupNames}
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
            {ICONS.refresh}
          </span>
        </button>
      </div>

      <OzCard flush>
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

        <OzTable>
          <OzTableHeader>
            <OzTableRow className="hover:bg-transparent">
              <OzTableHead aria-label="Select" className="w-[44px]">
                <Checkbox
                  checked={
                    paginated.length > 0 &&
                    paginated.every((p) => p.id && selected.has(p.id))
                  }
                  indeterminate={
                    paginated.some((p) => p.id && selected.has(p.id)) &&
                    !paginated.every((p) => p.id && selected.has(p.id))
                  }
                  onChange={(checked) => {
                    setSelected((prev) => {
                      const next = new Set(prev);
                      for (const p of paginated) {
                        if (!p.id) continue;
                        if (checked) next.add(p.id);
                        else next.delete(p.id);
                      }
                      return next;
                    });
                  }}
                  aria-label="Select all visible"
                />
              </OzTableHead>
              <OzTableHead>Name</OzTableHead>
              <OzTableHead>Address</OzTableHead>
              <OzTableHead>Group</OzTableHead>
              <OzTableHead>OS</OzTableHead>
              <OzTableHead>Version</OzTableHead>
              <OzTableHead>Last seen</OzTableHead>
              <OzTableHead>Notice</OzTableHead>
              <OzTableHead aria-label="Actions" className="w-[40px]" />
            </OzTableRow>
          </OzTableHeader>
          <OzTableBody>
            {paginated.map((p) => {
              const id = p.id ?? p.name;
              return (
                <OzTableRow
                  key={id}
                  data-state={selected.has(id) ? "selected" : undefined}
                >
                  <OzTableCell>
                    <Checkbox
                      checked={selected.has(id)}
                      onChange={() => toggleSelected(id)}
                      aria-label={`Select ${p.name}`}
                    />
                  </OzTableCell>
                  <OzTableCell>
                    <NameCell peer={p} />
                  </OzTableCell>
                  <OzTableCell>
                    <AddressCell peer={p} />
                  </OzTableCell>
                  <OzTableCell>
                    <GroupsCell peer={p} />
                  </OzTableCell>
                  <OzTableCell>
                    <OSCell peer={p} />
                  </OzTableCell>
                  <OzTableCell>
                    <span className="font-mono text-[11.5px] text-oz2-text-faint">
                      {p.version || "—"}
                    </span>
                  </OzTableCell>
                  <OzTableCell>
                    <LastSeenCell peer={p} />
                  </OzTableCell>
                  <OzTableCell>
                    <NoticeCell peer={p} />
                  </OzTableCell>
                  <OzTableCell>
                    <RowKebab />
                  </OzTableCell>
                </OzTableRow>
              );
            })}
            {paginated.length === 0 && (
              <OzTableRow className="hover:bg-transparent">
                <OzTableCell
                  colSpan={9}
                  className="px-[18px] py-12 text-center text-oz2-text-muted"
                >
                  {isLoading ? "Loading peers…" : "No peers match your filter."}
                </OzTableCell>
              </OzTableRow>
            )}
          </OzTableBody>
        </OzTable>

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
  );
}

// ─── Cell renderers ────────────────────────────────────────────────────────

function NameCell({ peer }: { peer: Peer }) {
  const status = deriveStatus(peer);
  // Mirror the legacy PeerNameCell fallback ladder:
  //   user.email → user.name → "user: <id>" → "—"
  // The seeded dev peers usually carry a user_id but no enriched user
  // object until UsersProvider hydrates, so the user_id fallback is
  // what the operator sees on first paint.
  const enrichedDisplay = peer.user?.email || peer.user?.name;
  const idFallback = peer.user_id ? `user: ${peer.user_id}` : null;
  const display = enrichedDisplay || idFallback || "—";
  return (
    <div className="flex min-w-0 flex-col">
      <span className="flex items-center gap-2">
        <OzStatusDot status={status} />
        <span className="truncate font-medium text-oz2-text">{peer.name}</span>
      </span>
      <span className="truncate pl-[16px] text-[11.5px] text-oz2-text-muted">
        {display}
      </span>
    </div>
  );
}

function AddressCell({ peer }: { peer: Peer }) {
  // Hover tooltip carries the network detail (Openzro IP, Public IP,
  // Domain, Region) so the dense cell only shows dns_label + IP.
  const region = [peer.city_name, peer.country_code]
    .filter(Boolean)
    .join(", ");
  return (
    <TooltipProvider>
      <Tooltip delayDuration={1}>
        <TooltipTrigger asChild>
          <div className="flex min-w-0 cursor-pointer items-center gap-3">
            <span className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-oz2-bg-sunken text-[14px] leading-none">
              {flagEmoji(peer.country_code)}
            </span>
            <div className="flex min-w-0 flex-col">
              <span className="truncate text-[13px] text-oz2-text">
                {peer.dns_label || peer.name}
              </span>
              <span className="truncate font-mono text-[11.5px] text-oz2-text-muted">
                {peer.ip}
              </span>
            </div>
          </div>
        </TooltipTrigger>
        <TooltipContent className="!p-0">
          <div className="min-w-[260px]">
            <InfoTooltipRow
              icon={ICONS.pin}
              label="Openzro IP"
              value={peer.ip || "—"}
            />
            <InfoTooltipRow
              icon={ICONS.network}
              label="Public IP"
              value={peer.connection_ip || "—"}
            />
            <InfoTooltipRow
              icon={ICONS.globe}
              label="Domain"
              value={peer.dns_label || peer.name || "—"}
              mono={false}
            />
            <InfoTooltipRow
              icon={
                <span className="text-[14px] leading-none">
                  {flagEmoji(peer.country_code)}
                </span>
              }
              label="Region"
              value={region || "—"}
              mono={false}
            />
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

function GroupsCell({ peer }: { peer: Peer }) {
  // Mirror production density: only the first chip is visible; the
  // rest collapse into a "+N" pill. Hover tooltip lists the full set.
  // Edit / add-group flow lives in the row kebab and lands in phase
  // 4.3 alongside PeerActionCell.
  const groups = (peer.groups ?? []).map((g) => g.name).filter(Boolean);
  if (groups.length === 0) {
    return <span className="text-[12.5px] text-oz2-text-faint">—</span>;
  }
  const visible = groups.slice(0, 1);
  const overflow = groups.length - visible.length;
  return (
    <TooltipProvider>
      <Tooltip delayDuration={1}>
        <TooltipTrigger asChild>
          <div className="inline-flex cursor-pointer items-center gap-1.5">
            {visible.map((g) => (
              <OzPill key={g} variant="default">
                {g}
              </OzPill>
            ))}
            {overflow > 0 && <OzPill variant="default">+{overflow}</OzPill>}
          </div>
        </TooltipTrigger>
        <TooltipContent className="!p-0">
          <div className="min-w-[200px]">
            <p className="px-3 pt-3 pb-2 font-mono text-[10.5px] uppercase tracking-widest text-oz2-text-faint">
              Assigned groups
            </p>
            <ul className="space-y-1 px-3 pb-3">
              {groups.map((g) => (
                <li
                  key={g}
                  className="flex items-center gap-2 text-[12px] text-oz2-text"
                >
                  <span className="text-oz2-text-faint">{ICONS.groupIcon}</span>
                  {g}
                </li>
              ))}
            </ul>
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

function OSCell({ peer }: { peer: Peer }) {
  // Icon-only with hover tooltip for OS + serial. Mirrors the legacy
  // PeerOSCell behaviour: row stays dense, full label lives in the
  // tooltip so operators don't lose accessibility to the OS string.
  return (
    <TooltipProvider>
      <Tooltip delayDuration={1}>
        <TooltipTrigger asChild>
          <span className="grid h-7 w-7 cursor-pointer place-items-center rounded-md text-oz2-text-2 transition-colors hover:bg-oz2-hover">
            <OSLogo os={peer.os} />
          </span>
        </TooltipTrigger>
        <TooltipContent className="!p-0">
          <div className="min-w-[200px]">
            <InfoTooltipRow
              icon={<CpuIcon size={14} />}
              label="OS"
              value={peer.os || "—"}
            />
            {peer.serial_number && peer.serial_number !== "" && (
              <InfoTooltipRow
                icon={<Barcode size={14} />}
                label="Serial number"
                value={peer.serial_number}
              />
            )}
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

function InfoTooltipRow({
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
    <div className="flex items-center gap-2 border-b border-oz2-border-soft px-3 py-2 text-[12.5px] last:border-b-0">
      <span className="text-oz2-text-faint">{icon}</span>
      <span className="text-oz2-text-faint">{label}</span>
      <span
        className={
          (mono ? "font-mono text-[11.5px] " : "text-[12px] ") +
          "ml-auto truncate text-oz2-text"
        }
      >
        {value}
      </span>
    </div>
  );
}

function LastSeenCell({ peer }: { peer: Peer }) {
  if (peer.connected) {
    return <span className="whitespace-nowrap text-oz2-text-muted">just now</span>;
  }
  const date = peer.last_seen ? dayjs(peer.last_seen) : null;
  const neverSeen = !date || date.isBefore(dayjs().subtract(2000, "years"));
  return (
    <span className="whitespace-nowrap text-oz2-text-muted">
      {neverSeen ? "never" : date.fromNow()}
    </span>
  );
}

function NoticeCell({ peer }: { peer: Peer }) {
  if (peer.login_expired) {
    return (
      <OzPill variant="err">
        <span className="opacity-80">{ICONS.alert}</span>
        Login required
      </OzPill>
    );
  }
  if (peer.approval_required) {
    return (
      <OzPill variant="warn">
        <span className="opacity-80">{ICONS.clock}</span>
        Approval pending
      </OzPill>
    );
  }
  if (!peer.login_expiration_enabled) {
    return (
      <OzPill variant="default">
        <span className="opacity-70">{ICONS.hourglass}</span>
        Expiration disabled
      </OzPill>
    );
  }
  return null;
}

function RowKebab() {
  // Phase 4.3 swaps this for the real PeerActionCell wired to
  // permissions + AdmissionBypassModal.
  return (
    <button
      type="button"
      aria-label="Row actions"
      disabled
      title="Actions wire-up lands in phase 4.3"
      className="grid h-7 w-7 place-items-center rounded-oz2-input text-oz2-text-faint opacity-60"
    >
      {ICONS.more}
    </button>
  );
}

// AddPeerButtonV2 — v2 paint over the legacy AddPeerButton modal flow.
// Mirrors components/ui/AddPeerButton: same first-run / onboarding /
// SetupModal wiring, just renders OzButton as the trigger so it
// inherits the v2 paint inside the topbar slot.
function AddPeerButtonV2({ peerCount }: { peerCount: number }) {
  const { oidcUser: user } = useOidcUser();

  const [hasOnboardingFormCompleted] = useLocalStorage(
    "openzro-onboarding-modal",
    false,
  );
  const [isFirstRun, setIsFirstRun] = useLocalStorage<boolean>(
    "openzro-first-run",
    peerCount === 0,
  );
  const [open, setOpen] = useState(
    !hasOnboardingFormCompleted
      ? process.env.APP_ENV !== "test"
        ? false
        : isFirstRun
      : isFirstRun,
  );

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    setIsFirstRun(false);
  };

  return (
    <Modal open={open} onOpenChange={handleOpenChange}>
      <ModalTrigger asChild>
        <OzButton variant="primary" type="button">
          <span className="inline-flex h-3.5 w-3.5 items-center justify-center">
            {ICONS.plus}
          </span>
          Add peer
        </OzButton>
      </ModalTrigger>
      <SetupModal user={user} />
    </Modal>
  );
}

// ─── Filter and helper components ─────────────────────────────────────────

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
    <div
      role="tablist"
      className="inline-flex h-[34px] items-center rounded-oz2-input bg-oz2-bg-sunken p-1 text-oz2-text-muted"
    >
      {options.map((opt) => {
        const active = opt.id === value;
        return (
          <button
            key={opt.id}
            type="button"
            role="tab"
            aria-selected={active}
            onClick={() => onChange(opt.id)}
            className={
              "inline-flex h-full items-center gap-1.5 whitespace-nowrap rounded-[6px] px-3 text-[12.5px] font-medium transition-colors " +
              (active
                ? "bg-oz2-surface text-oz2-text shadow-oz2-sm"
                : "hover:text-oz2-text")
            }
          >
            {opt.label}
            {typeof opt.count === "number" && (
              <span className="font-mono text-[10.5px] text-oz2-text-faint">
                {opt.count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}

function GroupFilter({
  value,
  onChange,
  allGroups,
  open,
  onOpenChange,
}: {
  value: string[];
  onChange: (next: string[]) => void;
  allGroups: string[];
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
          "inline-flex h-[34px] items-center gap-1.5 rounded-oz2-input border px-3 text-[13px] font-medium transition-colors " +
          (value.length > 0
            ? "border-transparent bg-oz2-acc-soft text-oz2-acc-text"
            : "border-oz2-border bg-oz2-surface text-oz2-text-2 hover:bg-oz2-hover")
        }
      >
        <span className="text-oz2-text-faint">Group:</span>
        {label}
        <span className="text-oz2-text-faint">{ICONS.chevDown}</span>
      </button>
      {open && (
        <div className="absolute right-0 top-full z-30 mt-2 w-[220px] overflow-hidden rounded-oz2-input border border-oz2-border bg-oz2-bg-elev shadow-oz2-md">
          <p className="border-b border-oz2-border-soft px-3 py-2 font-mono text-[10.5px] uppercase tracking-widest text-oz2-text-faint">
            Filter by group
          </p>
          <ul className="max-h-[260px] overflow-y-auto py-1">
            {allGroups.length === 0 && (
              <li className="px-3 py-3 text-[12px] text-oz2-text-faint">
                No groups yet
              </li>
            )}
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

function PageSizeCombobox({
  value,
  onChange,
}: {
  value: number;
  onChange: (next: number) => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const choices = [10, 20, 50, 100];

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
        <span className="text-oz2-text-faint">Rows:</span>
        <span className="font-mono">{value}</span>
        <span className="text-oz2-text-faint">{ICONS.chevDown}</span>
      </button>
      {open && (
        <div className="absolute right-0 top-full z-30 mt-1 min-w-[110px] overflow-hidden rounded-oz2-input border border-oz2-border bg-oz2-bg-elev shadow-oz2-md">
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
                    (c === value ? "text-oz2-acc-text" : "text-oz2-text")
                  }
                >
                  <span className="font-mono">{c}</span>
                  <span className="text-oz2-text-faint">rows</span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

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
        <span className="rotate-90">{ICONS.chevDown}</span>
      </PagerBtn>
      <span className="px-2 font-mono text-[12px] tabular-nums text-oz2-text-muted">
        {page} / {totalPages}
      </span>
      <PagerBtn
        disabled={!canNext}
        onClick={() => onChange(page + 1)}
        aria-label="Next page"
      >
        <span className="-rotate-90">{ICONS.chevDown}</span>
      </PagerBtn>
    </div>
  );
}

function PagerBtn({
  disabled,
  onClick,
  children,
  ...props
}: {
  disabled?: boolean;
  onClick: () => void;
  children: React.ReactNode;
} & Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, "onClick">) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={
        "grid h-7 w-7 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 transition-colors " +
        (disabled
          ? "opacity-40"
          : "hover:border-oz2-border-strong hover:bg-oz2-hover")
      }
      {...props}
    >
      {children}
    </button>
  );
}

function Checkbox({
  checked,
  indeterminate,
  onChange,
  ...props
}: {
  checked: boolean;
  indeterminate?: boolean;
  onChange: (checked: boolean) => void;
} & Omit<
  React.InputHTMLAttributes<HTMLInputElement>,
  "checked" | "onChange" | "type"
>) {
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
        {indeterminate && (
          <svg
            width={10}
            height={2}
            viewBox="0 0 10 2"
            fill="none"
          >
            <rect width={10} height={2} fill="currentColor" />
          </svg>
        )}
      </span>
    </label>
  );
}

// ─── Icons + helpers ─────────────────────────────────────────────────────

function flagEmoji(country: string | null | undefined): string {
  if (!country || country.length !== 2) return "🌐";
  const codePoints = country
    .toUpperCase()
    .split("")
    .map((c) => 127397 + c.charCodeAt(0));
  return String.fromCodePoint(...codePoints);
}

const baseIcon = (path: React.ReactNode) => (
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

const ICONS = {
  search: baseIcon(
    <>
      <circle cx={11} cy={11} r={7} />
      <path d="m20 20-3.5-3.5" />
    </>,
  ),
  pin: baseIcon(
    <>
      <path d="M12 13c2 0 4-2 4-4 0-2-1.5-4-4-4S8 7 8 9c0 2 2 4 4 4z" />
      <path d="M12 13v8" />
    </>,
  ),
  network: baseIcon(
    <>
      <circle cx={12} cy={5} r={2} />
      <circle cx={6} cy={19} r={2} />
      <circle cx={18} cy={19} r={2} />
      <path d="M12 7v3M12 10l-5 7M12 10l5 7" />
    </>,
  ),
  globe: baseIcon(
    <>
      <circle cx={12} cy={12} r={9} />
      <path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18" />
    </>,
  ),
  groupIcon: baseIcon(
    <>
      <circle cx={12} cy={8} r={4} />
      <path d="M4 21a8 8 0 0 1 16 0" />
    </>,
  ),
  chevDown: baseIcon(<path d="m6 9 6 6 6-6" />),
  refresh: baseIcon(
    <>
      <path d="M21 12a9 9 0 1 1-3.5-7.1" />
      <path d="M21 4v5h-5" />
    </>,
  ),
  alert: baseIcon(
    <>
      <path d="M12 9v4M12 17h.01" />
      <path d="M10.3 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
    </>,
  ),
  clock: baseIcon(
    <>
      <circle cx={12} cy={12} r={9} />
      <path d="M12 7v5l3 2" />
    </>,
  ),
  hourglass: baseIcon(
    <>
      <path d="M5 22h14M5 2h14M17 22v-4.17a2 2 0 0 0-.59-1.42L12 12l-4.41 4.41A2 2 0 0 0 7 17.83V22M7 2v4.17c0 .53.21 1.04.59 1.42L12 12l4.41-4.41A2 2 0 0 0 17 6.17V2" />
    </>,
  ),
  more: baseIcon(
    <>
      <circle cx={5} cy={12} r={1.4} />
      <circle cx={12} cy={12} r={1.4} />
      <circle cx={19} cy={12} r={1.4} />
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
