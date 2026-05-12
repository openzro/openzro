"use client";

import useFetchApi from "@utils/api";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import {
  ArrowUpRightIcon,
  KeyRoundIcon,
  PlusCircleIcon,
  ServerIcon,
  ShieldCheckIcon,
  Users2Icon,
} from "lucide-react";
import Link from "next/link";
import React, { useMemo } from "react";
import OzCard from "@/components/v2/OzCard";
import { useLoggedInUser } from "@/contexts/UsersProvider";
import { ActivityEvent } from "@/interfaces/ActivityEvent";
import { Network } from "@/interfaces/Network";
import { Peer } from "@/interfaces/Peer";
import { Policy } from "@/interfaces/Policy";
import ActivityTypeIcon from "@/modules/activity/ActivityTypeIcon";

dayjs.extend(relativeTime);

// /overview — v2 dashboard landing. Scoped to APIs already shipping
// (/peers, /networks, /policies, /events/audit, /users/current).
// Throughput/latency cards from the handoff prototype are intentionally
// not rendered yet — we don't expose a metrics API today and faking
// numbers would mislead operators. Re-introduce them when an aggregate
// metrics endpoint exists.

export default function OverviewPage() {
  const { loggedInUser } = useLoggedInUser();

  const { data: peers } = useFetchApi<Peer[]>("/peers");
  const { data: networks } = useFetchApi<Network[]>("/networks");
  const { data: policies } = useFetchApi<Policy[]>("/policies");
  const { data: events } = useFetchApi<ActivityEvent[]>("/events/audit");

  const stats = useMemo(() => {
    const total = peers?.length ?? 0;
    const online = peers?.filter((p) => p.connected).length ?? 0;
    const pending =
      peers?.filter((p) => p.approval_required === true).length ?? 0;
    const networksCount = networks?.length ?? 0;
    const activePolicies = policies?.filter((p) => p.enabled).length ?? 0;
    return { total, online, pending, networksCount, activePolicies };
  }, [peers, networks, policies]);

  const recentEvents = useMemo(() => {
    if (!events) return undefined;
    return [...events]
      .sort((a, b) => (a.timestamp < b.timestamp ? 1 : -1))
      .slice(0, 6);
  }, [events]);

  const firstName = useMemo(() => {
    const raw = loggedInUser?.name?.trim();
    if (!raw) return null;
    const first = raw.split(/\s+/)[0];
    return first || null;
  }, [loggedInUser]);

  return (
    <div className="space-y-6 px-8 py-8">
      <header className="space-y-1">
        <div className="flex items-center gap-2 font-mono text-[10.5px] uppercase tracking-[0.06em] text-oz2-acc-text">
          <span className="inline-block h-1.5 w-1.5 rounded-full bg-oz2-acc" />
          Workspace
        </div>
        <h1 className="text-[26px] font-semibold tracking-tight text-oz2-text">
          {firstName ? `Welcome back, ${firstName}` : "Welcome back"}
        </h1>
        <p className="max-w-2xl text-[13.5px] leading-[1.55] text-oz2-text-muted">
          {stats.total > 0
            ? `Your mesh has ${stats.online} of ${stats.total} ${
                stats.total === 1 ? "peer" : "peers"
              } online${
                stats.pending > 0
                  ? ` — ${stats.pending} pending approval.`
                  : "."
              }`
            : "No peers yet. Generate a setup key and add your first device to get started."}
        </p>
      </header>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          icon={<Users2Icon size={14} />}
          label="Online peers"
          value={stats.online}
          sub={`of ${stats.total} total`}
        />
        <StatCard
          icon={<ServerIcon size={14} />}
          label="Networks"
          value={stats.networksCount}
          sub={
            stats.networksCount === 1 ? "1 configured" : `${stats.networksCount} configured`
          }
        />
        <StatCard
          icon={<ShieldCheckIcon size={14} />}
          label="Active policies"
          value={stats.activePolicies}
          sub={
            policies && policies.length !== stats.activePolicies
              ? `${policies.length - stats.activePolicies} disabled`
              : "all enabled"
          }
        />
        <StatCard
          icon={<KeyRoundIcon size={14} />}
          label="Pending peers"
          value={stats.pending}
          sub={
            stats.pending === 0
              ? "no approvals waiting"
              : "awaiting admin approval"
          }
          tone={stats.pending > 0 ? "warn" : "muted"}
        />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[1.6fr_1fr]">
        <RecentActivityCard events={recentEvents} />
        <QuickActionsCard />
      </div>
    </div>
  );
}

function StatCard({
  icon,
  label,
  value,
  sub,
  tone = "muted",
}: {
  icon: React.ReactNode;
  label: string;
  value: number;
  sub: string;
  tone?: "muted" | "warn";
}) {
  return (
    <OzCard className="!p-[18px]">
      <div className="flex items-center gap-2 text-[12.5px] font-medium text-oz2-text-muted">
        <span className="text-oz2-text-faint">{icon}</span>
        {label}
      </div>
      <div className="mt-3 flex items-baseline gap-1.5">
        <span className="text-[30px] font-semibold tracking-tight text-oz2-text">
          {value}
        </span>
      </div>
      <div className="mt-1.5 text-[12px] text-oz2-text-faint">
        {tone === "warn" && value > 0 ? (
          <span className="inline-flex items-center gap-1.5">
            <span className="inline-block h-1.5 w-1.5 rounded-full bg-oz2-warn" />
            {sub}
          </span>
        ) : (
          sub
        )}
      </div>
    </OzCard>
  );
}

function RecentActivityCard({
  events,
}: {
  events: ActivityEvent[] | undefined;
}) {
  return (
    <OzCard flush>
      <div className="flex items-center justify-between border-b border-oz2-border-soft px-[18px] py-3.5">
        <div>
          <div className="text-[14px] font-semibold text-oz2-text">
            Recent activity
          </div>
          <div className="mt-0.5 text-[12px] text-oz2-text-muted">
            Latest events from your workspace audit log
          </div>
        </div>
        <Link
          href="/events/audit"
          className="inline-flex items-center gap-1 text-[12px] text-oz2-acc-text underline-offset-2 hover:underline"
        >
          View all
          <ArrowUpRightIcon size={12} />
        </Link>
      </div>
      {events === undefined ? (
        <div className="px-[18px] py-6 text-[13px] text-oz2-text-muted">
          Loading activity…
        </div>
      ) : events.length === 0 ? (
        <div className="px-[18px] py-6 text-[13px] text-oz2-text-muted">
          No activity recorded yet.
        </div>
      ) : (
        <ul>
          {events.map((event, i) => (
            <li
              key={event.id}
              className={
                "flex items-start gap-3 px-[18px] py-3 " +
                (i === 0 ? "" : "border-t border-oz2-border-soft")
              }
            >
              <span className="mt-0.5 grid h-7 w-7 shrink-0 place-items-center rounded-[8px] bg-oz2-bg-sunken text-oz2-text-2">
                <ActivityTypeIcon
                  code={event.activity_code}
                  size={14}
                  className="!h-3.5 !w-3.5 text-oz2-text-2"
                />
              </span>
              <div className="min-w-0 flex-1">
                <div className="truncate text-[13px] text-oz2-text">
                  <span className="font-medium">
                    {event.initiator_name || event.initiator_email || "system"}
                  </span>{" "}
                  <span className="text-oz2-text-muted">{event.activity}</span>
                </div>
              </div>
              <span className="shrink-0 font-mono text-[11px] text-oz2-text-faint">
                {dayjs(event.timestamp).fromNow(true)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </OzCard>
  );
}

function QuickActionsCard() {
  return (
    <OzCard flush>
      <div className="border-b border-oz2-border-soft px-[18px] py-3.5">
        <div className="text-[14px] font-semibold text-oz2-text">
          Quick actions
        </div>
        <div className="mt-0.5 text-[12px] text-oz2-text-muted">
          Get to the things you reach for first
        </div>
      </div>
      <ul className="divide-y divide-oz2-border-soft">
        <QuickAction
          href="/peers"
          icon={<Users2Icon size={14} />}
          title="Add a peer"
          sub="Install Openzro on a new device"
        />
        <QuickAction
          href="/setup-keys"
          icon={<KeyRoundIcon size={14} />}
          title="New setup key"
          sub="Generate a key for unattended installs"
        />
        <QuickAction
          href="/network"
          icon={<ServerIcon size={14} />}
          title="Create a network"
          sub="Define a new mesh segment with policies"
        />
        <QuickAction
          href="/team/users"
          icon={<PlusCircleIcon size={14} />}
          title="Invite a teammate"
          sub="Bring an admin or auditor into the workspace"
        />
      </ul>
    </OzCard>
  );
}

function QuickAction({
  href,
  icon,
  title,
  sub,
}: {
  href: string;
  icon: React.ReactNode;
  title: string;
  sub: string;
}) {
  return (
    <li>
      <Link
        href={href}
        className="group flex items-center gap-3 px-[18px] py-3 transition-colors hover:bg-oz2-hover"
      >
        <span className="grid h-7 w-7 shrink-0 place-items-center rounded-[8px] bg-oz2-acc-soft text-oz2-acc-text">
          {icon}
        </span>
        <div className="min-w-0 flex-1">
          <div className="text-[13px] font-medium text-oz2-text">{title}</div>
          <div className="text-[12px] text-oz2-text-muted">{sub}</div>
        </div>
        <ArrowUpRightIcon
          size={14}
          className="shrink-0 text-oz2-text-faint transition-colors group-hover:text-oz2-acc-text"
        />
      </Link>
    </li>
  );
}
