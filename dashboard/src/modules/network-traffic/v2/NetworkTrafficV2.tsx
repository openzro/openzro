"use client";

import useFetchApi from "@utils/api";
import dayjs from "dayjs";
import {
  ArrowDown,
  ArrowUp,
  Ban,
  GlobeIcon,
  Play,
  ShieldCheckIcon,
  Square,
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
// Table-shaped grid: one row per event, with same-flow events sharing
// a continuous rail in the Event column. Subsequent events of a flow
// elide the repeated Source / Protocol / Destination / Traffic /
// Status cells so the operator visually anchors to the first event of
// each flow. Rail terminates at the LAST event of each flow — no edge
// dangles past stop or drop.
//
// Each event's dot uses the audit-style tonal border + lucide icon:
// Play (start, ok), Square (stop, ok), Ban (drop, err), ShieldCheck
// (policy allow, acc), Ban (policy block, err).
//
// Behavior preserved verbatim from the legacy NetworkTrafficTimeline.

const PAGE_SIZE = 25;

// Grid template echoes the legacy table layout — wide Event column
// for the timeline narrative + four right-side columns plus Status.
const GRID_COLS =
  "minmax(0, 4fr) minmax(0, 2fr) minmax(0, 1.4fr) minmax(0, 2fr) minmax(0, 1.6fr) auto";

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

type EventTone = "ok" | "err" | "acc";

interface DotMeta {
  icon: React.ReactNode;
  tone: EventTone;
  label: string;
}

type Row =
  | {
      kind: "event";
      event: NetworkTrafficEvent;
      group: FlowGroup;
      isFirstInFlow: boolean;
      isLastInFlow: boolean;
    }
  | {
      kind: "policy";
      group: FlowGroup;
      isFirstInFlow: false;
      isLastInFlow: boolean;
    };

function eventDot(e: NetworkTrafficEvent): DotMeta {
  switch (e.type) {
    case "start":
      return { icon: <Play size={11} />, tone: "ok", label: "Start" };
    case "end":
      return { icon: <Square size={11} />, tone: "ok", label: "Stop" };
    case "drop":
      return { icon: <Ban size={11} />, tone: "err", label: "Drop" };
    default:
      return { icon: <Play size={11} />, tone: "ok", label: "Event" };
  }
}

function policyDot(blocked: boolean): DotMeta {
  return blocked
    ? { icon: <Ban size={11} />, tone: "err", label: "Block" }
    : { icon: <ShieldCheckIcon size={11} />, tone: "acc", label: "Policy" };
}

function isPolicyBlocked(policy: Policy | undefined): boolean {
  if (!policy) return false;
  const action = policy.rules?.[0]?.action;
  return action === "drop" || action === "deny";
}

function flowStatusPill(flow: FlowGroup): {
  variant: OzPillVariants["variant"];
  label: string;
  icon: React.ReactNode;
} {
  const hasDrop = flow.events.some((e) => e.type === "drop");
  const blocked = hasDrop || isPolicyBlocked(flow.policy);
  if (blocked) {
    return { variant: "err", label: "Blocked", icon: <Ban size={11} /> };
  }
  const hasEnd = flow.events.some((e) => e.type === "end");
  if (hasEnd) {
    return { variant: "ok", label: "Ended", icon: <Square size={11} /> };
  }
  return { variant: "acc", label: "Active", icon: <Play size={11} /> };
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

  const visibleGroups = useMemo(
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
          description="Once peers start reporting, every TCP / UDP / ICMP connection shows up here. Each flow's events stitch together via a shared timeline rail."
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

          {visibleGroups.length === 0 ? (
            <OzCard className="px-6 py-10 text-center text-[13.5px] text-oz2-text-muted">
              {isLoading
                ? "Loading network traffic…"
                : "No flows match your filters."}
            </OzCard>
          ) : (
            <OzCard flush>
              {/* Header gets its own grid so its column widths are
                  authoritative for the table. */}
              <div
                role="rowgroup"
                className="grid"
                style={{ gridTemplateColumns: GRID_COLS }}
              >
                <HeaderRow />
              </div>
              {/* Each flow renders as a separate sub-grid (same
                  template) wrapped in a relative container that
                  hosts ONE continuous rail across all the flow's
                  events. This mirrors AuditTimelineV2's pattern of
                  putting the rail on the parent <ol> rather than
                  per-row, and avoids the boundary cuts a per-cell
                  rail produced. */}
              {visibleGroups.map((flow, fi) => (
                <FlowBlock
                  key={flow.flowID}
                  flow={flow}
                  isLastFlow={fi === visibleGroups.length - 1}
                />
              ))}
            </OzCard>
          )}

          {filteredGroups.length > visibleGroups.length && (
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
        Event
      </div>
      <div role="columnheader" className={cell}>
        Source
      </div>
      <div role="columnheader" className={cell}>
        Protocol & Port
      </div>
      <div role="columnheader" className={cell}>
        Destination
      </div>
      <div role="columnheader" className={`${cell} justify-end`}>
        Traffic
      </div>
      <div role="columnheader" className={`${cell} justify-end pr-4`}>
        Status
      </div>
    </>
  );
}

// FlowBlock wraps the events of one flow in a sub-grid so the rail
// can sit absolutely above ALL of them in one piece — no per-cell
// rail breaks. The sub-grid uses the same gridTemplateColumns as the
// header, so column widths line up despite the nested grids.
function FlowBlock({
  flow,
  isLastFlow,
}: {
  flow: FlowGroup;
  isLastFlow: boolean;
}) {
  const rows = useMemo(() => flattenSingleFlow(flow), [flow]);
  const hasMultipleSteps = rows.length > 1;
  return (
    <div
      role="rowgroup"
      className={`relative grid ${
        isLastFlow ? "" : "border-b border-oz2-border-soft"
      }`}
      style={{ gridTemplateColumns: GRID_COLS }}
    >
      {/* Continuous rail: one element per flow. Aligned to the dot
          column's vertical center across all event rows. The dot's
          opaque bg-oz2-surface (z-10) covers the rail in its 24px
          disc, so the rail visually "passes through" each step's
          dot without breaks. Only renders when the flow has more
          than one step (otherwise there's nothing to connect). */}
      {hasMultipleSteps && (
        <span
          aria-hidden
          // left:28px = px-4 (16) + half of dot column (12) = dot center
          // top/bottom: ~24-30px so the rail starts inside the first
          //   dot and ends inside the last (the dot's surface fill
          //   masks the overshoot in its disc).
          className="pointer-events-none absolute left-[28px] top-7 bottom-7 z-0 w-[2px] -translate-x-1/2 bg-oz2-border-strong"
        />
      )}
      {rows.map((r, i) => (
        <RowCells key={rowKey(r, i)} row={r} />
      ))}
    </div>
  );
}

// flattenSingleFlow expands ONE FlowGroup into its rows. Same logic
// as the prior flattenRows but operating on a single flow.
function flattenSingleFlow(g: FlowGroup): Row[] {
  const out: Row[] = [];
  const events = [...g.events].sort((a, b) =>
    a.occurred_at.localeCompare(b.occurred_at),
  );
  const totalSteps = events.length + (g.policy ? 1 : 0);
  let stepIdx = 0;
  for (let i = 0; i < events.length; i++) {
    const e = events[i];
    out.push({
      kind: "event",
      event: e,
      group: g,
      isFirstInFlow: stepIdx === 0,
      isLastInFlow: stepIdx === totalSteps - 1,
    });
    stepIdx++;
    if (i === 0 && g.policy && events.length > 1) {
      out.push({
        kind: "policy",
        group: g,
        isFirstInFlow: false,
        isLastInFlow: stepIdx === totalSteps - 1,
      });
      stepIdx++;
    }
  }
  if (g.policy && events.length === 1) {
    out.push({
      kind: "policy",
      group: g,
      isFirstInFlow: false,
      isLastInFlow: true,
    });
  }
  return out;
}

function RowCells({ row }: { row: Row }) {
  // The first event in a flow carries the repeated context (peer pair,
  // protocol, traffic totals, status). Later events / synthetic policy
  // rows leave those cells blank so the row visually attaches to the
  // flow's anchor row.
  const isFirst = row.kind === "event" && row.isFirstInFlow;

  // Uniform padding + min-height so every row in a flow has the
  // same vertical real estate regardless of content density. Without
  // this, a 1-line policy step + 2-line event steps produced
  // different row heights, which slid the dot's vertical center
  // around and made the rail's start→policy gap visibly larger
  // than the policy→end gap.
  const cellCls = "px-4 py-2.5 min-h-[44px]";
  const firstEvent = row.group.events[0];
  const status = flowStatusPill(row.group);

  return (
    <>
      <div role="cell" className={cellCls}>
        <EventCell row={row} />
      </div>
      <div role="cell" className={cellCls}>
        {isFirst && (
          <PeerCell
            peer={row.group.sourcePeer}
            resource={row.group.sourceResource}
            ip={firstEvent.source_ip}
          />
        )}
      </div>
      <div role="cell" className={cellCls}>
        {isFirst && <ProtoCell event={firstEvent} />}
      </div>
      <div role="cell" className={cellCls}>
        {isFirst && (
          <PeerCell
            peer={row.group.destPeer}
            resource={row.group.destResource}
            ip={firstEvent.dest_ip}
          />
        )}
      </div>
      <div role="cell" className={`${cellCls} flex items-center justify-end`}>
        {isFirst && <TrafficCell flow={row.group} />}
      </div>
      <div
        role="cell"
        className={`${cellCls} flex items-center justify-end pr-4`}
      >
        {isFirst && (
          // Inline-flex wrapper around the pill keeps it from
          // stretching to fill the cell height — flex children default
          // to align-items:stretch otherwise.
          <span className="inline-flex">
            <OzPill variant={status.variant}>
              <span className="inline-flex items-center gap-1.5">
                <span aria-hidden>{status.icon}</span>
                {status.label}
              </span>
            </OzPill>
          </span>
        )}
      </div>
    </>
  );
}

const dotToneClasses: Record<EventTone, string> = {
  ok: "border-oz2-ok text-oz2-ok",
  err: "border-oz2-err text-oz2-err",
  acc: "border-oz2-acc text-oz2-acc",
};

// EventCell renders the dot column + the event's own narrative line.
// The rail itself lives on the FlowBlock parent (one continuous
// element across all events of the flow), mirroring how
// AuditTimelineV2 hangs its rail off the <ol> rather than per-row.
// Each EventCell only renders the dot — its solid bg-oz2-surface
// fill (z-10) masks the rail in its 24px disc.
function EventCell({ row }: { row: Row }) {
  const meta =
    row.kind === "event"
      ? eventDot(row.event)
      : policyDot(isPolicyBlocked(row.group.policy));

  const time =
    row.kind === "event"
      ? dayjs(row.event.occurred_at).format("MMM D · HH:mm:ss")
      : null;

  return (
    <div className="flex items-stretch gap-3">
      <div className="relative flex w-6 shrink-0 flex-col items-center justify-center">
        <span
          aria-label={meta.label}
          className={`relative z-10 grid h-6 w-6 shrink-0 place-items-center rounded-full border-2 bg-oz2-surface ${dotToneClasses[meta.tone]}`}
        >
          {meta.icon}
        </span>
      </div>
      <div className="flex min-w-0 flex-1 flex-col justify-center gap-0.5">
        {row.kind === "event" ? (
          <>
            <span className="text-[13px] text-oz2-text">
              {narrativeFor(row.event, row.group)}
            </span>
            <span className="font-mono text-[11px] tabular-nums text-oz2-text-faint">
              {time}
            </span>
          </>
        ) : (
          <PolicyContent policy={row.group.policy as Policy} />
        )}
      </div>
    </div>
  );
}

function PolicyContent({ policy }: { policy: Policy }) {
  const blocked = isPolicyBlocked(policy);
  return (
    <span className="text-[13px] text-oz2-text">
      Policy{" "}
      <a
        href="/access-control"
        className="font-medium text-oz2-acc-text underline-offset-2 hover:underline"
      >
        {policy.name}
      </a>{" "}
      <span className="text-oz2-text-muted">
        {blocked ? "blocked the connection" : "allowed the connection"}
      </span>
    </span>
  );
}

function PeerCell({
  peer,
  resource,
  ip,
}: {
  peer?: Peer;
  resource?: NetworkResource;
  ip: string;
}) {
  const fallback = resource?.address ?? (peer ? undefined : "external");
  const label = peer?.name ?? fallback ?? ip;
  return (
    <div className="flex min-w-0 items-center gap-2">
      <span className="relative grid h-7 w-7 shrink-0 place-items-center rounded-md border border-oz2-border bg-oz2-bg-sunken">
        <span className="grid h-4 w-4 place-items-center text-oz2-text-faint">
          {peer ? <OSLogo os={peer.os} /> : <GlobeIcon size={12} />}
        </span>
        {peer?.country_code && (
          <span className="absolute -bottom-1 -right-1">
            <RoundedFlag country={peer.country_code} size={11} />
          </span>
        )}
      </span>
      <div className="flex min-w-0 flex-col">
        <span className="truncate text-[13px] text-oz2-text">{label}</span>
        <span className="font-mono text-[11px] text-oz2-text-faint">{ip}</span>
      </div>
    </div>
  );
}

function ProtoCell({ event }: { event: NetworkTrafficEvent }) {
  const port = portChipFor(event);
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="inline-flex items-center gap-1 rounded-md border border-oz2-border bg-oz2-bg-sunken px-2 py-0.5 font-mono text-[11px] uppercase tracking-wide text-oz2-text">
        {protocolName(event.protocol)}
      </span>
      {port && (
        <span className="inline-flex items-center rounded-md border border-oz2-border-soft bg-oz2-surface px-2 py-0.5 font-mono text-[11px] uppercase tracking-wide text-oz2-text-2">
          {port.label}
        </span>
      )}
    </div>
  );
}

// TrafficCell — handoff-flavored "row carries weight" block: mono
// totals on top, a 2-segment rx-vs-tx ratio bar below, with the
// packet tally beneath. Rendered right-aligned per the table grid.
function TrafficCell({ flow }: { flow: FlowGroup }) {
  const total = flow.totalRx + flow.totalTx;
  const rxPct = total > 0 ? (flow.totalRx / total) * 100 : 0;
  const txPct = total > 0 ? (flow.totalTx / total) * 100 : 0;
  return (
    <div className="flex w-full flex-col items-end gap-1">
      <div className="flex items-center gap-3 font-mono text-[12px] tabular-nums text-oz2-text">
        <span className="inline-flex items-center gap-1">
          <ArrowDown size={11} className="text-oz2-acc" />
          {formatBytes(flow.totalRx)}
        </span>
        <span className="inline-flex items-center gap-1">
          <ArrowUp size={11} className="text-oz2-acc" />
          {formatBytes(flow.totalTx)}
        </span>
      </div>
      {total > 0 && (
        <div
          aria-hidden
          className="flex h-1 w-[120px] overflow-hidden rounded-full bg-oz2-border-soft"
        >
          <span className="bg-oz2-acc" style={{ width: `${rxPct}%` }} />
          <span
            className="bg-oz2-acc-soft-2"
            style={{ width: `${txPct}%` }}
          />
        </div>
      )}
      <span className="font-mono text-[10.5px] text-oz2-text-faint">
        {flow.totalRxPkts + flow.totalTxPkts} pkts
      </span>
    </div>
  );
}

// ─── Helpers ─────────────────────────────────────────────────────────

function rowKey(r: Row, idx: number): string {
  if (r.kind === "event") return `${r.group.flowID}:e:${r.event.event_id}`;
  return `${r.group.flowID}:p:${idx}`;
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

function narrativeFor(e: NetworkTrafficEvent, g: FlowGroup): string {
  const dest = g.destPeer?.name ?? g.destResource?.address ?? e.dest_ip;
  const src = g.sourcePeer?.name ?? g.sourceResource?.address ?? e.source_ip;
  switch (e.type) {
    case "start":
      if (e.direction === "egress") {
        return `${src} requested P2P connection to ${dest}`;
      }
      return `${dest} received P2P connection from ${src}`;
    case "end":
      if (e.direction === "egress") {
        return `${src} stopped P2P connection to ${dest}`;
      }
      return `${dest} stopped P2P connection from ${src}`;
    case "drop":
      return `${dest} blocked connection from ${src}`;
    default:
      return `Event between ${dest} and ${src}`;
  }
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
