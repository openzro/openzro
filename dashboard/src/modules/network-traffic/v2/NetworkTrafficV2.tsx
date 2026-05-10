"use client";

import useFetchApi from "@utils/api";
import dayjs from "dayjs";
import {
  ActivityIcon,
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
// Each FLOW gets one row; inside that row a vertical mini-timeline
// reads top-to-bottom with the start / policy / stop sequence (each
// event a tonal-bordered dot connected by a rail), borrowing the
// handoff TrafficScreen's start/stop/policy iconography. The right
// half of the row condenses the flow's headline data — peer pair,
// protocol, total rx/tx with a tiny rx-vs-tx ratio bar and packet
// count, plus the overall flow status pill — using the handoff's
// "make the row pull its weight" approach.
//
// Behavior preserved verbatim from the legacy NetworkTrafficTimeline:
// same SWR endpoint, same groupFlows logic, same search / peer
// filter / date range, same RestrictedAccess + PeersProvider gates.

const PAGE_SIZE = 25;

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

type FlowStatus = "active" | "ended" | "dropped";

type EventTone = "ok" | "err" | "acc";

interface DotMeta {
  icon: React.ReactNode;
  tone: EventTone;
  label: string;
}

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

function flowStatus(flow: FlowGroup): {
  status: FlowStatus;
  variant: OzPillVariants["variant"];
  label: string;
} {
  // "drop" wins regardless of order; otherwise an "end" marks the
  // flow as ended; default to active.
  const hasDrop = flow.events.some((e) => e.type === "drop");
  if (hasDrop) return { status: "dropped", variant: "err", label: "blocked" };
  const hasEnd = flow.events.some((e) => e.type === "end");
  if (hasEnd) return { status: "ended", variant: "ok", label: "ended" };
  return { status: "active", variant: "acc", label: "active" };
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
          description="Once peers start reporting, every TCP / UDP / ICMP connection shows up here. Each row is one flow with its lifecycle inside."
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
              <ol className="m-0 list-none p-0">
                {visible.map((flow, idx) => (
                  <FlowRow
                    key={flow.flowID}
                    flow={flow}
                    isLast={idx === visible.length - 1}
                  />
                ))}
              </ol>
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

// FlowRow — one flow per row. Two visually-distinct columns:
//
//   LEFT (timeline): vertical mini-timeline of the flow's lifecycle
//   events (start → optional matched policy → end | drop). Each event
//   is its own dot+icon row connected by a rail; `subsequent rows of
//   the same flow share that rail` was a request from the user, and
//   the icons follow the handoff start/stop/policy mapping.
//
//   RIGHT (summary): the handoff TrafficScreen's "row carries weight"
//   slot — peer pair (source → destination) over a small protocol /
//   port chip, then a packed traffic readout (mono total bytes, a tiny
//   rx-vs-tx ratio bar, and the packet tally) topped by the flow's
//   overall status pill. This block makes the row useful at a glance
//   without expanding into the timeline detail.
function FlowRow({ flow, isLast }: { flow: FlowGroup; isLast: boolean }) {
  const ordered = useMemo(
    () =>
      [...flow.events].sort((a, b) =>
        a.occurred_at.localeCompare(b.occurred_at),
      ),
    [flow.events],
  );

  type Step =
    | { kind: "event"; event: NetworkTrafficEvent }
    | { kind: "policy" };
  const steps: Step[] = useMemo(() => {
    const out: Step[] = [];
    for (let i = 0; i < ordered.length; i++) {
      out.push({ kind: "event", event: ordered[i] });
      // Splice the synthetic policy line in right after the start so
      // the lifecycle reads start → policy → stop.
      if (i === 0 && flow.policy && ordered.length > 1) {
        out.push({ kind: "policy" });
      }
    }
    if (flow.policy && ordered.length === 1) out.push({ kind: "policy" });
    return out;
  }, [ordered, flow.policy]);

  const status = flowStatus(flow);
  const firstEvent = ordered[0];

  return (
    <li
      className={`grid grid-cols-1 gap-6 px-5 py-4 lg:grid-cols-[minmax(0,1fr)_minmax(280px,360px)] ${
        isLast ? "" : "border-b border-oz2-border-soft"
      }`}
    >
      {/* LEFT — vertical mini-timeline */}
      <div className="relative">
        {/* Rail spans the dot column from first to last dot. */}
        <span
          aria-hidden
          className="absolute bottom-3 left-[11px] top-3 w-px bg-oz2-border-soft"
        />
        <ol className="m-0 flex list-none flex-col gap-1.5 p-0">
          {steps.map((step, i) => {
            if (step.kind === "policy") {
              return (
                <PolicyStep
                  key={`p-${i}`}
                  policy={flow.policy as Policy}
                />
              );
            }
            return (
              <EventStep
                key={`e-${step.event.event_id}`}
                event={step.event}
                flow={flow}
                showFullDate={i === 0}
              />
            );
          })}
        </ol>
      </div>

      {/* RIGHT — peer pair + traffic summary */}
      <div className="flex flex-col items-stretch gap-3">
        <div className="flex items-start justify-between gap-3">
          <span className="font-mono text-[10.5px] uppercase tracking-wider text-oz2-text-faint">
            {dayjs(flow.startedAt).format("MMM D")}
          </span>
          <OzPill variant={status.variant}>
            <span className="font-mono">{status.label}</span>
          </OzPill>
        </div>

        <div className="flex items-center gap-2">
          <PeerChip
            peer={flow.sourcePeer}
            resource={flow.sourceResource}
            ip={firstEvent?.source_ip ?? ""}
          />
          <span aria-hidden className="text-oz2-text-faint">
            →
          </span>
          <PeerChip
            peer={flow.destPeer}
            resource={flow.destResource}
            ip={firstEvent?.dest_ip ?? ""}
          />
        </div>

        <div className="flex items-center gap-2 text-[11.5px] text-oz2-text-muted">
          {firstEvent && (
            <ProtoChip event={firstEvent} />
          )}
          {flow.reportingPeer && (
            <span className="inline-flex items-center gap-1 font-mono text-[10.5px] text-oz2-text-faint">
              <ActivityIcon size={11} />
              reporter: {flow.reportingPeer.name}
            </span>
          )}
        </div>

        <TrafficSummary flow={flow} status={status.status} />
      </div>
    </li>
  );
}

const dotToneClasses: Record<EventTone, string> = {
  ok: "border-oz2-ok text-oz2-ok",
  err: "border-oz2-err text-oz2-err",
  acc: "border-oz2-acc text-oz2-acc",
};

function StepDot({ meta }: { meta: DotMeta }) {
  return (
    <span
      aria-label={meta.label}
      className={`relative z-10 grid h-6 w-6 shrink-0 place-items-center rounded-full border-2 bg-oz2-surface ${dotToneClasses[meta.tone]}`}
      // The shadow ring blends the dot into the page background
      // wherever the rail passes behind it. Uses --ozv2-bg so it
      // tracks the theme.
      style={{ boxShadow: "0 0 0 4px var(--ozv2-bg)" }}
    >
      {meta.icon}
    </span>
  );
}

function EventStep({
  event,
  flow,
  showFullDate,
}: {
  event: NetworkTrafficEvent;
  flow: FlowGroup;
  showFullDate: boolean;
}) {
  const meta = eventDot(event);
  return (
    <li className="flex items-start gap-3">
      <StepDot meta={meta} />
      <div className="min-w-0 flex-1 pt-0.5">
        <div className="text-[13px] text-oz2-text">
          {narrativeFor(event, flow)}
        </div>
        <div className="mt-0.5 flex items-center gap-1.5 font-mono text-[11px] text-oz2-text-faint tabular-nums">
          <span>
            {showFullDate
              ? dayjs(event.occurred_at).format("MMM D · HH:mm:ss")
              : dayjs(event.occurred_at).format("HH:mm:ss")}
          </span>
        </div>
      </div>
    </li>
  );
}

function PolicyStep({ policy }: { policy: Policy }) {
  const action = policy.rules?.[0]?.action;
  const blocked = action === "drop" || action === "deny";
  const meta = policyDot(blocked);
  return (
    <li className="flex items-start gap-3">
      <StepDot meta={meta} />
      <div className="min-w-0 flex-1 pt-0.5 text-[13px] text-oz2-text">
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
      </div>
    </li>
  );
}

function PeerChip({
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
    <span className="inline-flex min-w-0 items-center gap-1.5 rounded-md border border-oz2-border bg-oz2-bg-sunken px-2 py-1 text-[12px] text-oz2-text-2">
      <span className="relative grid h-4 w-4 shrink-0 place-items-center text-oz2-text-faint">
        {peer ? <OSLogo os={peer.os} /> : <GlobeIcon size={11} />}
        {peer?.country_code && (
          <span className="absolute -bottom-1 -right-1">
            <RoundedFlag country={peer.country_code} size={9} />
          </span>
        )}
      </span>
      <span className="truncate font-medium text-oz2-text">{label}</span>
      <span className="font-mono text-[10.5px] text-oz2-text-faint">{ip}</span>
    </span>
  );
}

function ProtoChip({ event }: { event: NetworkTrafficEvent }) {
  const port = portChipFor(event);
  return (
    <span className="inline-flex items-center gap-1.5 rounded-md border border-oz2-border bg-oz2-surface px-2 py-0.5 font-mono text-[11px] uppercase tracking-wide text-oz2-text-2">
      {protocolName(event.protocol)}
      {port && (
        <>
          <span className="text-oz2-text-faint">:</span>
          {port.label}
        </>
      )}
    </span>
  );
}

// TrafficSummary — handoff-flavored "row pulls its weight" block.
// Top: rx ↓ vs tx ↑ in mono with arrows, sized large enough to read
// at a glance. Middle: a 2-segment proportion bar (cyan rx, violet
// tx) so the operator can eyeball whether the flow leaned upload or
// download without parsing two byte values. Bottom: packet tally
// in mono.
function TrafficSummary({
  flow,
  status,
}: {
  flow: FlowGroup;
  status: FlowStatus;
}) {
  const { totalRx, totalTx, totalRxPkts, totalTxPkts } = flow;
  const total = totalRx + totalTx;
  const rxPct = total > 0 ? (totalRx / total) * 100 : 50;
  const txPct = total > 0 ? (totalTx / total) * 100 : 50;
  const empty = total === 0 && status === "dropped";
  return (
    <div className="rounded-oz2-input border border-oz2-border-soft bg-oz2-bg-sunken px-3 py-2.5">
      <div className="flex items-center justify-between gap-3 font-mono text-[13px] tabular-nums">
        <span className="inline-flex items-center gap-1 text-oz2-text">
          <ArrowDown size={12} className="text-oz2-acc" />
          {formatBytes(totalRx)}
        </span>
        <span className="inline-flex items-center gap-1 text-oz2-text">
          <ArrowUp size={12} className="text-oz2-acc" />
          {formatBytes(totalTx)}
        </span>
      </div>
      <div
        aria-hidden
        className="mt-1.5 flex h-1 overflow-hidden rounded-full bg-oz2-border-soft"
      >
        <span
          className="bg-oz2-acc"
          style={{ width: `${empty ? 0 : rxPct}%` }}
        />
        <span
          className="bg-oz2-acc-soft-2"
          style={{ width: `${empty ? 0 : txPct}%` }}
        />
      </div>
      <div className="mt-1.5 flex items-center justify-between font-mono text-[10.5px] text-oz2-text-faint">
        <span>{totalRxPkts + totalTxPkts} pkts</span>
        <span>
          {totalRxPkts}↓ · {totalTxPkts}↑
        </span>
      </div>
    </div>
  );
}

// ─── Helpers ports from legacy NetworkTrafficTimeline ─────────────────

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
