"use client";

import dayjs from "dayjs";
import { Cog, FileTextIcon } from "lucide-react";
import { usePathname } from "next/navigation";
import React, { useEffect, useMemo, useRef, useState } from "react";
import { DateRange } from "react-day-picker";
import { useSWRConfig } from "swr";
import OzCard from "@/components/v2/OzCard";
import OzEmptyState from "@/components/v2/OzEmptyState";
import OzPill, { type OzPillVariants } from "@/components/v2/OzPill";
import { useLocalStorage } from "@/hooks/useLocalStorage";
import { ActivityEvent } from "@/interfaces/ActivityEvent";
import ActivityDescription from "@/modules/activity/ActivityDescription";
import ActivityTypeIcon from "@/modules/activity/ActivityTypeIcon";
import { type UserSelectOption } from "@/modules/activity/UsersDropdownSelector";
import { getColorFromCode } from "@/modules/activity/utils";
import DateRangePickerV2 from "@/modules/activity/v2/DateRangePickerV2";
import EventCodeSelectorV2 from "@/modules/activity/v2/EventCodeSelectorV2";
import InitiatorSelectorV2 from "@/modules/activity/v2/InitiatorSelectorV2";

// AuditTimelineV2 — phase-5.11 v2 paint over /events/audit.
// Replaces the legacy ActivityTable's flat row layout with the
// day-grouped timeline from the handoff (screens-2 ActivityScreen):
// each section starts with a sticky day header ("Today" / "Yesterday"
// / weekday + absolute date), followed by a list of events sharing
// a vertical rail. Each event renders as a 24px tonal-bordered dot
// over the rail with an actor / description / kind-pill / time line
// next to it.
//
// Behavior is preserved verbatim from ActivityTable:
//
//   - same SWR endpoint (/events/audit) and refresh wiring
//   - same default 14-day date range, persisted to localStorage by
//     pathname
//   - same multi-select event-code filter (ActivityEventCodeSelector)
//   - same initiator filter (UsersDropdownSelector)
//   - same client-side search by audit name / user / peer / meta
//   - same RestrictedAccess gate at the page level
//
// Pagination keeps the v2 "Load earlier events" pattern from the
// handoff — each click bumps the row window by `PAGE_SIZE`. The
// data set is fetched in full from /events/audit (server doesn't
// cursor-paginate today), so this is just a render slice.

interface Props {
  events: ActivityEvent[] | undefined;
  isLoading: boolean;
}

const PAGE_SIZE = 50;

const defaultFromDate = dayjs().subtract(14, "day").toDate();
const defaultToDate = dayjs().toDate();

type EventTone = "ok" | "err" | "warn" | "acc" | "info";

// getColorFromCode returns the legacy "green/red/openzro/blue-darker"
// names; map them onto v2 tones so we can pick the right OzPill /
// border / background per event.
function tonalFor(code: string): EventTone {
  const c = getColorFromCode(code);
  if (c === "green") return "ok";
  if (c === "red") return "err";
  if (c === "openzro") return "warn";
  return "info";
}

// Build the section bucket label for a given day. "Today" /
// "Yesterday" beat absolute dates so the operator can scan recent
// activity at a glance; everything else falls back to the weekday +
// short month-day string.
function dayLabelFor(d: dayjs.Dayjs): string {
  const today = dayjs().startOf("day");
  if (d.isSame(today, "day")) return "Today";
  if (d.isSame(today.subtract(1, "day"), "day")) return "Yesterday";
  return d.format("dddd");
}

export default function AuditTimelineV2({ events, isLoading }: Props) {
  const { mutate } = useSWRConfig();
  const path = usePathname();

  const [search, setSearch] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const [eventCodes, setEventCodes] = useState<string[]>([]);
  const [initiator, setInitiator] = useState<string>("");
  const [pageSize, setPageSize] = useState(PAGE_SIZE);

  const [persistedRange, setPersistedRange] = useLocalStorage<
    DateRange | undefined
  >("openzro-table-range" + path, {
    from: defaultFromDate,
    to: defaultToDate,
  });
  const [dateRange, setDateRange] = useState<DateRange | undefined>({
    from: persistedRange?.from
      ? dayjs(persistedRange.from).toDate()
      : defaultFromDate,
    to: persistedRange?.to ? dayjs(persistedRange.to).toDate() : defaultToDate,
  });

  const userSelectOptions = useMemo<UserSelectOption[]>(() => {
    if (!events) return [];
    const seen = new Map<string, UserSelectOption>();
    for (const e of events) {
      const email = e.initiator_email || "Openzro";
      if (seen.has(email)) continue;
      seen.set(email, {
        name: e.initiator_name,
        id: e.initiator_id,
        email,
        external: Boolean(e?.meta?.external),
      });
    }
    return Array.from(seen.values());
  }, [events]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    const from = dateRange?.from ? dayjs(dateRange.from).startOf("day") : null;
    const to = dateRange?.to ? dayjs(dateRange.to).endOf("day") : null;
    return (events ?? []).filter((e) => {
      if (eventCodes.length > 0 && !eventCodes.includes(e.activity_code)) {
        return false;
      }
      if (initiator) {
        const email = e.initiator_email || "Openzro";
        if (email !== initiator) return false;
      }
      if (from || to) {
        const ts = dayjs(e.timestamp);
        if (from && ts.isBefore(from)) return false;
        if (to && ts.isAfter(to)) return false;
      }
      if (!q) return true;
      const haystack = [
        e.activity,
        e.activity_code,
        e.initiator_name,
        e.initiator_email,
        ...(e.meta ? Object.values(e.meta) : []),
      ]
        .filter(Boolean)
        .join(" ")
        .toLowerCase();
      return haystack.includes(q);
    });
  }, [events, search, eventCodes, initiator, dateRange]);

  // Sort newest first, slice to current window. The full set lives in
  // SWR; pagination is a render concern only.
  const sorted = useMemo(() => {
    return [...filtered].sort((a, b) => {
      return (
        dayjs(b.timestamp).valueOf() - dayjs(a.timestamp).valueOf()
      );
    });
  }, [filtered]);

  const visible = useMemo(
    () => sorted.slice(0, pageSize),
    [sorted, pageSize],
  );

  // Re-bucket visible rows by day-of-timestamp. Map preserves
  // insertion order, so the sort above (newest first) carries
  // through to "Today" appearing before "Yesterday" etc.
  const grouped = useMemo(() => {
    const buckets = new Map<
      string,
      { label: string; date: dayjs.Dayjs; events: ActivityEvent[] }
    >();
    for (const e of visible) {
      const d = dayjs(e.timestamp).startOf("day");
      const key = d.format("YYYY-MM-DD");
      const existing = buckets.get(key);
      if (existing) {
        existing.events.push(e);
      } else {
        buckets.set(key, { label: dayLabelFor(d), date: d, events: [e] });
      }
    }
    return Array.from(buckets.values());
  }, [visible]);

  useEffect(() => {
    setPageSize(PAGE_SIZE);
  }, [search, eventCodes, initiator, dateRange]);

  const refreshClick = () => {
    setRefreshing(true);
    mutate("/events/audit").finally(() => setRefreshing(false));
  };

  const isColdStart = !isLoading && (events?.length ?? 0) === 0;

  return (
    <div className="space-y-6 p-8">
      <header>
        <h1 className="text-[24px] font-semibold tracking-tight">Activity</h1>
        <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
          Every action across the mesh — sign-ins, policy changes, peer
          lifecycle. Grouped by day, newest first.
        </p>
      </header>

      {isColdStart ? (
        <OzEmptyState
          title="No audit events yet"
          description="Once peers connect or operators make changes, every action lands here. The feed is grouped by day and newest first."
          learnMore={
            <>
              Learn more about{" "}
              <a
                href="https://docs.openzro.io/how-to/audit-events-logging"
                target="_blank"
                rel="noopener noreferrer"
                className="font-medium text-oz2-acc-text underline-offset-2 hover:underline"
              >
                Audit Events
              </a>
              .
            </>
          }
        />
      ) : (
        <>
          {/* Toolbar — search + filters above the timeline. Mirrors
              the layout other v2 list screens use, just adapted to
              the audit-specific filter widgets. */}
          <div className="flex flex-wrap items-center gap-2.5">
            <div className="inline-flex h-8 w-[280px] items-center gap-2 rounded-oz2-input border border-oz2-border bg-oz2-surface px-2.5">
              <span className="text-oz2-text-faint">{ICONS.search}</span>
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search by audit name, user, peer, meta…"
                className="h-full flex-1 border-0 bg-transparent text-[12.5px] outline-none placeholder:text-oz2-text-faint"
              />
            </div>

            <DateRangePickerV2
              value={dateRange}
              onChange={(range) => {
                setDateRange(range);
                setPersistedRange(range);
              }}
            />

            {events && (
              <EventCodeSelectorV2
                events={events}
                values={eventCodes}
                onChange={setEventCodes}
              />
            )}

            {events && (
              <InitiatorSelectorV2
                options={userSelectOptions}
                value={initiator}
                onChange={(item) => setInitiator(item ?? "")}
              />
            )}

            <PageSizeCombobox value={pageSize} onChange={setPageSize} />

            <button
              type="button"
              onClick={refreshClick}
              aria-label="Refresh audit events"
              className="grid h-8 w-8 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 hover:border-oz2-border-strong hover:bg-oz2-hover"
            >
              <span className={refreshing ? "animate-spin text-oz2-acc" : ""}>
                {ICONS.refresh}
              </span>
            </button>

            <span className="ml-auto font-mono text-[11px] uppercase tracking-[0.04em] text-oz2-text-faint">
              {filtered.length} events
            </span>
          </div>

          {grouped.length === 0 ? (
            <OzCard className="px-6 py-10 text-center text-[13.5px] text-oz2-text-muted">
              {isLoading
                ? "Loading audit events…"
                : "No events match your filters."}
            </OzCard>
          ) : (
            <div className="space-y-6">
              {grouped.map((section) => (
                <DaySection key={section.date.format("YYYY-MM-DD")} {...section} />
              ))}
            </div>
          )}

          {/* "Load earlier events" — handoff ghost button at the
              bottom; bumps the render window by another PAGE_SIZE. */}
          {sorted.length > visible.length && (
            <div className="flex justify-center pt-2">
              <button
                type="button"
                onClick={() => setPageSize((n) => n + PAGE_SIZE)}
                className="inline-flex h-8 items-center rounded-oz2-input bg-transparent px-4 text-[13px] font-medium text-oz2-text-2 transition-colors hover:bg-oz2-hover hover:text-oz2-text"
              >
                Load earlier events
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
}

function DaySection({
  label,
  date,
  events,
}: {
  label: string;
  date: dayjs.Dayjs;
  events: ActivityEvent[];
}) {
  return (
    <section>
      <div className="sticky top-0 z-10 flex items-baseline gap-3 border-b border-oz2-border-soft bg-oz2-bg pb-2 pt-1.5">
        <h3 className="m-0 text-[13px] font-semibold text-oz2-text">{label}</h3>
        <span className="text-[12px] text-oz2-text-muted">
          {date.format("ddd, MMM D")}
        </span>
        <span className="ml-auto font-mono text-[11px] text-oz2-text-faint">
          {events.length} {events.length === 1 ? "event" : "events"}
        </span>
      </div>
      <ol className="relative m-0 mt-2 list-none p-0">
        {/* Vertical rail running through the dot column. Sized so it
            doesn't poke past the first/last dot. */}
        <span
          aria-hidden
          className="absolute bottom-3 left-[19px] top-3 w-px bg-oz2-border-soft"
        />
        {events.map((e, i) => (
          <EventRow key={e.id || `${e.timestamp}-${i}`} event={e} />
        ))}
      </ol>
    </section>
  );
}

function EventRow({ event }: { event: ActivityEvent }) {
  const tone = tonalFor(event.activity_code);
  const dotClasses: Record<EventTone, string> = {
    ok: "border-oz2-ok text-oz2-ok",
    err: "border-oz2-err text-oz2-err",
    warn: "border-oz2-warn text-oz2-warn",
    acc: "border-oz2-acc text-oz2-acc",
    info: "border-oz2-acc text-oz2-acc",
  };
  const pillVariant: OzPillVariants["variant"] =
    tone === "err" ? "err" : tone === "warn" ? "warn" : tone === "ok" ? "ok" : "default";

  const actor =
    event.initiator_name ||
    event.initiator_email ||
    (event.initiator_id ? `user: ${event.initiator_id}` : "System");

  return (
    <li className="relative flex items-start gap-3.5 py-2.5 pl-[52px] pr-1">
      <span
        aria-hidden
        className={`absolute left-2 top-3.5 grid h-6 w-6 place-items-center rounded-full border-2 bg-oz2-surface ${dotClasses[tone]}`}
        // The shadow ring blends the dot into the page background
        // wherever the rail passes behind it; using --ozv2-bg as the
        // ring colour keeps that working in both themes.
        style={{ boxShadow: "0 0 0 4px var(--ozv2-bg)" }}
      >
        <ActivityTypeIcon code={event.activity_code} size={12} />
      </span>

      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-2 text-[13.5px]">
          <span
            className={
              "font-semibold " +
              (event.initiator_id ? "text-oz2-text" : "text-oz2-text-muted")
            }
          >
            {actor === "Openzro" ? (
              <span className="inline-flex items-center gap-1.5">
                <Cog size={12} className="text-oz2-text-muted" />
                System
              </span>
            ) : (
              actor
            )}
          </span>
          <span className="text-oz2-text-muted">
            <ActivityDescription event={event} />
          </span>
          <OzPill variant={pillVariant} className="ml-auto">
            <FileTextIcon size={11} className="text-current" />
            <span className="font-mono">{event.activity_code}</span>
          </OzPill>
        </div>
        <div className="mt-1 flex items-center gap-2 text-[12px] text-oz2-text-muted">
          <span className="font-mono text-oz2-text-faint tabular-nums">
            {dayjs(event.timestamp).format("HH:mm:ss")}
          </span>
          <span className="text-oz2-border-strong">·</span>
          <span className="text-oz2-text-muted">
            {event.initiator_email || "Openzro"}
            {event.target_id && (
              <>
                <span className="mx-1.5 text-oz2-border-strong">·</span>
                <span className="font-mono text-oz2-text-faint">
                  target: {event.target_id}
                </span>
              </>
            )}
          </span>
        </div>
      </div>
    </li>
  );
}

// ─── PageSizeCombobox (mirrors the rest of v2 tables) ───────────────────

function PageSizeCombobox({
  value,
  onChange,
}: {
  value: number;
  onChange: (next: number) => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const choices = [25, 50, 100, 200, 500];

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

// ─── Icons ─────────────────────────────────────────────────────────────────

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
