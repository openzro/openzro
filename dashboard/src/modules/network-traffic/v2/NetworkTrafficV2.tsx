"use client";

import useFetchApi from "@utils/api";
import dayjs from "dayjs";
import {
  Ban,
  CheckCircle2,
  GlobeIcon,
  MoreHorizontal,
  ShieldCheckIcon,
} from "lucide-react";
import React, { useEffect, useMemo, useRef, useState } from "react";
import { DateRange } from "react-day-picker";
import RoundedFlag from "@/assets/countries/RoundedFlag";
import OzCard from "@/components/v2/OzCard";
import OzEmptyState from "@/components/v2/OzEmptyState";
import OzPill, { type OzPillVariants } from "@/components/v2/OzPill";
import { usePeers } from "@/contexts/PeersProvider";
import { NetworkResource } from "@/interfaces/Network";
import {
  NetworkTrafficEvent,
  NetworkTrafficEventsResponse,
} from "@/interfaces/NetworkTrafficEvent";
import { Peer } from "@/interfaces/Peer";
import { Policy } from "@/interfaces/Policy";
import DateRangePickerV2 from "@/modules/activity/v2/DateRangePickerV2";
import EventsTabs from "@/modules/events/v2/EventsTabs";
import PeerFilterV2, {
  type PeerFilterOption,
} from "@/modules/network-traffic/v2/PeerFilterV2";
import { OSLogo } from "@/modules/peers/PeerOSCell";

// NetworkTrafficV2 — phase-5.12 v2 paint over /network-traffic-events.
// Pixel-aligned with the handoff TrafficScreen (extracted screens
// module of openzro-dashboard.bundle.html → TrafficScreen + FlowRow):
// one flow per row, the row's Path cell stitches Source → Policy →
// Destination as connected pill nodes with arrowed segments. Status,
// time, and size each have their own column so a list of flows
// scans cleanly.
//
// Path color tracks the flow's status:
//   allowed → solid violet line + acc-tinted policy pill
//   blocked → dashed red line + err-tinted policy pill ("— no match —")
//
// Behavior preserved verbatim from the legacy NetworkTrafficTimeline
// (groupFlows, search, peer filter, date range, refresh, perms).

const PAGE_SIZE = 25;

// Column widths echo the handoff: tight time + status + size, with
// the rest of the row going to the path cell.
const GRID_COLS =
  "92px minmax(0, 1fr) 130px 110px 32px";

type FlowGroup = {
  flowID: string;
  events: NetworkTrafficEvent[];
  reportingPeer?: Peer;
  sourcePeer?: Peer;
  destPeer?: Peer;
  sourceResource?: NetworkResource;
  destResource?: NetworkResource;
  policy?: Policy;
  totalRx: number;
  totalTx: number;
  totalRxPkts: number;
  totalTxPkts: number;
  latestAt: string;
  startedAt: string;
};

type FlowStatus = "allowed" | "blocked";

interface StatusMeta {
  status: FlowStatus;
  variant: OzPillVariants["variant"];
  icon: React.ReactNode;
  label: string;
  // Path arrow color (solid for allowed, dashed pattern for blocked).
  arrowColor: string;
  // Policy node paint.
  policyBg: string;
  policyBorder: string;
  policyText: string;
  policyIcon: React.ReactNode;
}

function statusFor(flow: FlowGroup): StatusMeta {
  const action = flow.policy?.rules?.[0]?.action;
  const policyBlocks = action === "drop" || action === "deny";
  const hasDrop = flow.events.some((e) => e.type === "drop");
  const blocked = hasDrop || policyBlocks;
  if (blocked) {
    return {
      status: "blocked",
      variant: "err",
      icon: <Ban size={11} />,
      label: "Blocked",
      arrowColor: "var(--ozv2-err)",
      policyBg: "var(--ozv2-err-bg)",
      policyBorder:
        "color-mix(in oklab, var(--ozv2-err) 35%, var(--ozv2-border))",
      policyText: "var(--ozv2-err)",
      policyIcon: <Ban size={11} />,
    };
  }
  return {
    status: "allowed",
    variant: "ok",
    icon: <CheckCircle2 size={11} />,
    label: "Allowed",
    arrowColor: "var(--ozv2-acc)",
    policyBg:
      "color-mix(in oklab, var(--ozv2-ok) 12%, var(--ozv2-surface))",
    policyBorder:
      "color-mix(in oklab, var(--ozv2-ok) 35%, var(--ozv2-border))",
    policyText: "var(--ozv2-ok)",
    policyIcon: <ShieldCheckIcon size={11} />,
  };
}

export default function NetworkTrafficV2() {
  const { peers } = usePeers();
  const { data: policies } = useFetchApi<Policy[]>("/policies");
  const { data: resources } = useFetchApi<NetworkResource[]>(
    "/networks/resources",
  );

  const [search, setSearch] = useState("");
  const [range, setRange] = useState<DateRange | undefined>();
  const [peerFilter, setPeerFilter] = useState<string | undefined>();
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [refreshing, setRefreshing] = useState(false);

  useEffect(() => {
    setPageSize(PAGE_SIZE);
  }, [search, range, peerFilter]);

  const queryUrl = useMemo(() => {
    const params = new URLSearchParams();
    if (range?.from) params.set("since", range.from.toISOString());
    if (range?.to) params.set("until", range.to.toISOString());
    const qs = params.toString();
    return qs ? `/network-traffic-events?${qs}` : "/network-traffic-events";
  }, [range]);

  const {
    data,
    isLoading,
    mutate: mutateFlows,
  } = useFetchApi<NetworkTrafficEventsResponse>(queryUrl);

  const groups = useMemo(
    () =>
      groupFlows(
        data?.events ?? [],
        peers ?? [],
        policies ?? [],
        resources ?? [],
      ),
    [data, peers, policies, resources],
  );

  const filteredGroups = useMemo(() => {
    const q = search.trim().toLowerCase();
    return groups.filter((g) => {
      if (peerFilter) {
        if (
          g.sourcePeer?.id !== peerFilter &&
          g.destPeer?.id !== peerFilter &&
          g.reportingPeer?.id !== peerFilter
        ) {
          return false;
        }
      }
      if (q && !matchesSearch(g, q)) return false;
      return true;
    });
  }, [groups, search, peerFilter]);

  const visible = useMemo(
    () => filteredGroups.slice(0, pageSize),
    [filteredGroups, pageSize],
  );

  const peerOptions = useMemo<PeerFilterOption[]>(
    () =>
      (peers ?? [])
        .filter((p) => p.id)
        .map((p) => ({
          id: p.id as string,
          name: p.name,
          hostname: p.hostname,
          ip: p.ip,
          os: p.os,
          countryCode: p.country_code,
        }))
        .sort((a, b) => a.name.localeCompare(b.name)),
    [peers],
  );

  const onRefresh = () => {
    setRefreshing(true);
    mutateFlows().finally(() => setRefreshing(false));
  };

  const isColdStart = !isLoading && groups.length === 0;

  return (
    <div className="space-y-6 p-8">
      <header>
        <h1 className="text-[24px] font-semibold tracking-tight">Activity</h1>
        <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
          Per-flow records reported by your peers — connection starts, ends,
          and drops. Useful for forensics, capacity planning, and validating
          that your access policies match traffic in the wild.
        </p>
      </header>

      <EventsTabs />

      {isColdStart ? (
        <OzEmptyState
          title="No network traffic events yet"
          description="Once peers start reporting, every TCP / UDP / ICMP connection shows up here. Each row stitches the source → policy → destination path."
          learnMore={
            <>
              Learn more about{" "}
              <a
                href="https://docs.openzro.io/how-to/network-traffic-events"
                target="_blank"
                rel="noopener noreferrer"
                className="font-medium text-oz2-acc-text underline-offset-2 hover:underline"
              >
                Network Traffic Events
              </a>
              .
            </>
          }
        />
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-2.5">
            <div className="inline-flex h-8 w-[280px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-2.5">
              <span className="text-oz2-text-faint">{ICONS.search}</span>
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search by peer, IP or policy…"
                className="h-full flex-1 border-0 bg-transparent text-[12.5px] outline-none placeholder:text-oz2-text-faint"
              />
            </div>

            <DateRangePickerV2 value={range} onChange={setRange} />

            <PeerFilterV2
              value={peerFilter}
              options={peerOptions}
              onChange={setPeerFilter}
            />

            <PageSizeCombobox value={pageSize} onChange={setPageSize} />

            <button
              type="button"
              onClick={onRefresh}
              aria-label="Refresh network traffic"
              className="grid h-8 w-8 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 hover:border-oz2-border-strong hover:bg-oz2-hover"
            >
              <span className={refreshing ? "animate-spin text-oz2-acc" : ""}>
                {ICONS.refresh}
              </span>
            </button>

            <span className="ml-auto font-mono text-[11px] uppercase tracking-[0.04em] text-oz2-text-faint">
              {filteredGroups.length} flow
              {filteredGroups.length === 1 ? "" : "s"}
            </span>
          </div>

          {visible.length === 0 ? (
            <OzCard className="px-6 py-10 text-center text-[13.5px] text-oz2-text-muted">
              {isLoading
                ? "Loading network traffic…"
                : "No flows match your filters."}
            </OzCard>
          ) : (
            <OzCard flush>
              <div
                role="table"
                aria-label="Network traffic events"
                className="grid items-stretch"
                style={{ gridTemplateColumns: GRID_COLS }}
              >
                <HeaderRow />
                {visible.map((flow, i) => (
                  <FlowRow
                    key={flow.flowID}
                    flow={flow}
                    isLast={i === visible.length - 1}
                  />
                ))}
              </div>
            </OzCard>
          )}

          {filteredGroups.length > visible.length && (
            <div className="flex justify-center pt-2">
              <button
                type="button"
                onClick={() => setPageSize((n) => n + PAGE_SIZE)}
                className="inline-flex h-8 items-center rounded-oz2-input bg-transparent px-4 text-[13px] font-medium text-oz2-text-2 transition-colors hover:bg-oz2-hover hover:text-oz2-text"
              >
                Load earlier flows
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
}

function HeaderRow() {
  const cell =
    "flex h-10 items-center bg-oz2-bg-sunken px-4 font-mono text-[10.5px] font-medium uppercase tracking-[0.06em] text-oz2-text-muted border-b border-oz2-border-soft";
  return (
    <>
      <div role="columnheader" className={cell}>
        Time
      </div>
      <div role="columnheader" className={cell}>
        Path · Source → Policy → Destination
      </div>
      <div role="columnheader" className={`${cell} justify-end`}>
        Size
      </div>
      <div role="columnheader" className={cell}>
        Status
      </div>
      <div role="columnheader" className={cell} aria-label="Actions" />
    </>
  );
}

// FlowRow — one flow per row. Five cells: Time, Path (the headline
// FlowNode mini-timeline), Size, Status, kebab.
function FlowRow({ flow, isLast }: { flow: FlowGroup; isLast: boolean }) {
  const meta = statusFor(flow);
  const firstEvent = flow.events[0];
  const time = dayjs(flow.startedAt).format("HH:mm:ss");

  const cell = `flex items-center px-4 py-3 ${
    isLast ? "" : "border-b border-oz2-border-soft"
  }`;

  return (
    <>
      <div role="cell" className={cell}>
        <span className="font-mono text-[12px] tabular-nums text-oz2-text-muted">
          {time}
        </span>
      </div>

      <div role="cell" className={cell}>
        <PathCell flow={flow} firstEvent={firstEvent} meta={meta} />
      </div>

      <div role="cell" className={`${cell} justify-end`}>
        <SizeCell flow={flow} meta={meta} />
      </div>

      <div role="cell" className={cell}>
        <OzPill variant={meta.variant}>
          <span className="inline-flex items-center gap-1.5">
            <span aria-hidden>{meta.icon}</span>
            {meta.label}
          </span>
        </OzPill>
      </div>

      <div role="cell" className={cell}>
        <button
          type="button"
          aria-label="Flow actions"
          className="grid h-7 w-7 place-items-center rounded-oz2-input text-oz2-text-faint transition-colors hover:bg-oz2-hover hover:text-oz2-text"
        >
          <MoreHorizontal size={14} />
        </button>
      </div>
    </>
  );
}

// PathCell — Source pill → arrow segment → Policy pill → arrow
// segment → Destination pill. Each pill renders OS-icon avatar +
// stacked label/sublabel; the policy pill is tonally tinted by
// status (green for allowed, red for blocked) with a "— no match —"
// fallback when we don't know which policy fired (a drop event
// without a rule_id).
function PathCell({
  flow,
  firstEvent,
  meta,
}: {
  flow: FlowGroup;
  firstEvent: NetworkTrafficEvent | undefined;
  meta: StatusMeta;
}) {
  const policyName =
    flow.policy?.name ?? (meta.status === "blocked" ? "— no match —" : "—");
  const protoLabel = firstEvent ? protoCellLabel(firstEvent) : "—";
  return (
    <div className="flex min-w-0 items-center gap-0">
      <PeerNode
        peer={flow.sourcePeer}
        resource={flow.sourceResource}
        ip={firstEvent?.source_ip ?? ""}
        sublabel={
          flow.sourcePeer?.dns_label ||
          flow.sourcePeer?.hostname ||
          flow.sourceResource?.type
        }
      />
      <PathSegment color={meta.arrowColor} dashed={meta.status === "blocked"} />
      <PolicyNode meta={meta} name={policyName} sublabel={protoLabel} />
      <PathSegment color={meta.arrowColor} dashed={meta.status === "blocked"} />
      <PeerNode
        peer={flow.destPeer}
        resource={flow.destResource}
        ip={firstEvent?.dest_ip ?? ""}
        sublabel={
          flow.destPeer?.dns_label ||
          flow.destPeer?.hostname ||
          flow.destResource?.type
        }
      />
    </div>
  );
}

// PeerNode — rounded pill with OS-icon avatar on the left and a
// 2-line label / sublabel block. Uses OSLogo + RoundedFlag from the
// peer module so the iconography matches the rest of the v2
// dashboard. Falls through to the bare IP when neither a peer nor a
// resource is known.
function PeerNode({
  peer,
  resource,
  ip,
  sublabel,
}: {
  peer?: Peer;
  resource?: NetworkResource;
  ip: string;
  sublabel?: string;
}) {
  const fallback = resource?.address ?? (peer ? undefined : "external");
  const label = peer?.name ?? fallback ?? ip;
  const sub = sublabel ?? (peer ? undefined : ip);
  return (
    <span className="inline-flex max-w-[200px] min-w-0 shrink-0 items-center gap-2 rounded-full border border-oz2-border bg-oz2-surface py-1 pl-1 pr-3 shadow-oz2-sm">
      <span className="relative grid h-7 w-7 shrink-0 place-items-center rounded-full bg-oz2-bg-sunken text-oz2-text-faint">
        <span className="grid h-4 w-4 place-items-center">
          {peer ? <OSLogo os={peer.os} /> : <GlobeIcon size={12} />}
        </span>
        {peer?.country_code && (
          <span className="absolute -bottom-1 -right-1">
            <RoundedFlag country={peer.country_code} size={11} />
          </span>
        )}
      </span>
      <span className="flex min-w-0 flex-col leading-tight">
        <span className="truncate text-[12.5px] font-semibold text-oz2-text">
          {label}
        </span>
        {sub && (
          <span className="truncate text-[10.5px] text-oz2-text-muted">
            {sub}
          </span>
        )}
      </span>
    </span>
  );
}

// PolicyNode — middle pill of the path. Background + border + icon
// switch on flow status (acc-tinted ok for allowed, err-tinted for
// blocked). Two-line label so the policy name and protocol/port
// stack like the handoff.
function PolicyNode({
  meta,
  name,
  sublabel,
}: {
  meta: StatusMeta;
  name: string;
  sublabel: string;
}) {
  return (
    <span
      className="inline-flex max-w-[220px] min-w-0 shrink-0 items-center gap-2 rounded-full py-1 pl-1 pr-3"
      style={{
        background: meta.policyBg,
        border: `1px solid ${meta.policyBorder}`,
      }}
    >
      <span
        className="grid h-6 w-6 shrink-0 place-items-center rounded-full text-oz2-text-on-acc"
        style={{ background: meta.policyText }}
      >
        {meta.policyIcon}
      </span>
      <span className="flex min-w-0 flex-col leading-tight">
        <span
          className="truncate text-[12.5px] font-semibold"
          style={{ color: meta.policyText }}
        >
          {name}
        </span>
        <span className="truncate font-mono text-[10.5px] text-oz2-text-muted">
          {sublabel}
        </span>
      </span>
    </span>
  );
}

// PathSegment — horizontal connector between path nodes. Solid line
// + chevron tip for allowed; dashed pattern for blocked. Sized so
// the line carries weight at the row's reading distance and the
// arrow tip is unambiguously visible.
function PathSegment({
  color,
  dashed,
}: {
  color: string;
  dashed: boolean;
}) {
  return (
    <span className="relative inline-block h-5 w-9 shrink-0">
      <span
        aria-hidden
        className="absolute left-0 right-[10px] top-1/2 h-[3px] -translate-y-1/2 rounded-sm"
        style={
          dashed
            ? {
                background: `repeating-linear-gradient(90deg, ${color} 0 6px, transparent 6px 11px)`,
              }
            : { background: color }
        }
      />
      <span
        aria-hidden
        className="absolute right-0 top-1/2 -translate-y-1/2"
        style={{
          width: 0,
          height: 0,
          borderTop: "5px solid transparent",
          borderBottom: "5px solid transparent",
          borderLeft: `8px solid ${color}`,
        }}
      />
    </span>
  );
}

// SizeCell — total bytes mono on top, a tiny event-by-event
// sparkline below (each bar = one event's tx+rx bytes), and pkts ·
// approx duration in mono at the bottom. Sparkline color tracks
// flow status so the readout reads at a glance.
function SizeCell({ flow, meta }: { flow: FlowGroup; meta: StatusMeta }) {
  const total = flow.totalRx + flow.totalTx;
  const pkts = flow.totalRxPkts + flow.totalTxPkts;
  // Synthesize a 6-bar sparkline from the event stream. Each bar's
  // height is proportional to that event's byte total. For flows
  // with fewer than 6 events, we left-pad with zero-height bars so
  // the sparkline always occupies the same width.
  const bars = useMemo(() => {
    const sorted = [...flow.events].sort((a, b) =>
      a.occurred_at.localeCompare(b.occurred_at),
    );
    const series = sorted.map((e) => e.rx_bytes + e.tx_bytes);
    const max = Math.max(...series, 1);
    const target = 8;
    const padded =
      series.length < target
        ? Array(target - series.length)
            .fill(0)
            .concat(series)
        : series.slice(-target);
    return padded.map((v) => (v / max) * 100);
  }, [flow.events]);

  // Approximate flow duration. When start ≠ latest, the window
  // gives a rough wall-clock; otherwise show the dash.
  const durationMs = useMemo(() => {
    const s = dayjs(flow.startedAt).valueOf();
    const e = dayjs(flow.latestAt).valueOf();
    return Math.max(0, e - s);
  }, [flow.startedAt, flow.latestAt]);

  return (
    <div className="flex flex-col items-end gap-1">
      <span className="font-mono text-[13px] font-medium tabular-nums text-oz2-text">
        {formatBytes(total)}
      </span>
      <div
        aria-hidden
        className="flex h-3 items-end gap-[2px]"
      >
        {bars.map((h, i) => (
          <span
            key={i}
            className="w-[3px] rounded-[1px]"
            style={{
              height: `${Math.max(15, h)}%`,
              background: meta.arrowColor,
              opacity: 0.35 + (h / 100) * 0.55,
            }}
          />
        ))}
      </div>
      <span className="font-mono text-[10.5px] text-oz2-text-muted">
        {formatPkts(pkts)} · {formatDuration(durationMs)}
      </span>
    </div>
  );
}

// ─── Helpers ─────────────────────────────────────────────────────────

function protoCellLabel(e: NetworkTrafficEvent): string {
  const port = portChipFor(e);
  const p = protocolName(e.protocol).toUpperCase();
  return port ? `${p}:${port.label}` : p;
}

function groupFlows(
  events: NetworkTrafficEvent[],
  peers: Peer[],
  policies: Policy[],
  resources: NetworkResource[],
): FlowGroup[] {
  const byID = new Map<string, Peer>();
  const byIP = new Map<string, Peer>();
  for (const p of peers) {
    if (p.id) byID.set(p.id, p);
    if (p.ip) byIP.set(p.ip, p);
  }
  const policyByID = new Map<string, Policy>();
  for (const p of policies) {
    if (p.id) policyByID.set(p.id, p);
  }
  const resourceByID = new Map<string, NetworkResource>();
  for (const r of resources) {
    if (r.id) resourceByID.set(r.id, r);
  }

  const grouped = new Map<string, FlowGroup>();
  for (const e of events) {
    const reporting = byID.get(e.peer_id);
    let source = byIP.get(e.source_ip);
    let dest = byIP.get(e.dest_ip);
    if (!source && reporting && e.direction === "egress") source = reporting;
    if (!dest && reporting && e.direction === "ingress") dest = reporting;
    const sourceResource = e.source_resource_id
      ? resourceByID.get(e.source_resource_id)
      : undefined;
    const destResource = e.dest_resource_id
      ? resourceByID.get(e.dest_resource_id)
      : undefined;

    const key = e.flow_id || e.event_id;
    let g = grouped.get(key);
    if (!g) {
      g = {
        flowID: key,
        events: [],
        reportingPeer: reporting,
        sourcePeer: source,
        destPeer: dest,
        sourceResource,
        destResource,
        policy: e.rule_id ? policyByID.get(e.rule_id) : undefined,
        totalRx: 0,
        totalTx: 0,
        totalRxPkts: 0,
        totalTxPkts: 0,
        latestAt: e.occurred_at,
        startedAt: e.occurred_at,
      };
      grouped.set(key, g);
    } else {
      g.sourcePeer = g.sourcePeer ?? source;
      g.destPeer = g.destPeer ?? dest;
      g.reportingPeer = g.reportingPeer ?? reporting;
      g.sourceResource = g.sourceResource ?? sourceResource;
      g.destResource = g.destResource ?? destResource;
    }
    g.events.push(e);
    g.totalRx += e.rx_bytes;
    g.totalTx += e.tx_bytes;
    g.totalRxPkts += e.rx_packets;
    g.totalTxPkts += e.tx_packets;
    if (e.occurred_at > g.latestAt) g.latestAt = e.occurred_at;
    if (e.occurred_at < g.startedAt) g.startedAt = e.occurred_at;
    if (!g.policy && e.rule_id) g.policy = policyByID.get(e.rule_id);
  }

  return Array.from(grouped.values()).sort((a, b) =>
    b.latestAt.localeCompare(a.latestAt),
  );
}

function matchesSearch(g: FlowGroup, q: string): boolean {
  const haystack = [
    g.sourcePeer?.name,
    g.sourcePeer?.hostname,
    g.destPeer?.name,
    g.destPeer?.hostname,
    g.sourceResource?.name,
    g.sourceResource?.address,
    g.destResource?.name,
    g.destResource?.address,
    g.reportingPeer?.name,
    g.policy?.name,
    g.events[0]?.source_ip,
    g.events[0]?.dest_ip,
  ];
  return haystack.some((v) => !!v && v.toLowerCase().includes(q));
}

function portChipFor(e: NetworkTrafficEvent): { label: string } | null {
  if (e.protocol === 6 || e.protocol === 17) {
    if (e.dest_port) return { label: String(e.dest_port) };
    return null;
  }
  if (e.protocol === 1 || e.protocol === 58) {
    const t = e.icmp_type;
    if (t === undefined) return null;
    if (t === 8 || t === 128) return { label: "Echo" };
    if (t === 0 || t === 129) return { label: "Reply" };
    return { label: `type ${t}` };
  }
  return null;
}

function protocolName(protocol: number): string {
  switch (protocol) {
    case 1:
      return "icmp";
    case 6:
      return "tcp";
    case 17:
      return "udp";
    case 58:
      return "icmpv6";
    case 132:
      return "sctp";
    default:
      return `p${protocol}`;
  }
}

function formatBytes(b: number): string {
  if (b < 1024) return `${b} B`;
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`;
  if (b < 1024 * 1024 * 1024) return `${(b / 1024 / 1024).toFixed(1)} MB`;
  return `${(b / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

function formatPkts(n: number): string {
  if (n < 1000) return `${n}`;
  if (n < 10_000) return `${(n / 1000).toFixed(1)}k`;
  if (n < 1_000_000) return `${Math.round(n / 1000)}k`;
  return `${(n / 1_000_000).toFixed(1)}m`;
}

function formatDuration(ms: number): string {
  if (ms <= 0) return "—";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  if (ms < 3_600_000) return `${Math.round(ms / 60_000)}m`;
  return `${Math.round(ms / 3_600_000)}h`;
}

// ─── PageSizeCombobox + icons (same shape as AuditTimelineV2) ──────────

function PageSizeCombobox({
  value,
  onChange,
}: {
  value: number;
  onChange: (next: number) => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const choices = [10, 25, 50, 100, 1000];

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
        className="inline-flex h-8 items-center gap-1.5 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 text-[13px] font-medium text-oz2-text-2 hover:bg-oz2-hover hover:border-oz2-border-strong"
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
                    "flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left text-[13.5px] hover:bg-oz2-hover " +
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
  chevDown: baseIcon(<path d="m6 9 6 6 6-6" />),
  refresh: baseIcon(
    <>
      <path d="M21 12a9 9 0 1 1-3.5-7.1" />
      <path d="M21 4v5h-5" />
    </>,
  ),
};
