"use client";

import useFetchApi from "@utils/api";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import {
  Check as CheckIcon,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
  X as XIcon,
} from "lucide-react";
import * as React from "react";

dayjs.extend(relativeTime);

// PostureEvaluation — JSON shape returned by
// GET /api/peers/{peerId}/posture-evaluations (see
// management/server/http/handlers/peers/peers_handler.go).
// Defined locally (not in types.gen.go) because the timeline is
// dashboard UX, not OpenAPI surface.
type PostureEvaluation = {
  id: number;
  posture_check_id: string;
  check_type: string;
  compliant: boolean;
  reason: string;
  evaluated_at: string; // ISO 8601
};

type Props = {
  peerID: string;
};

// Map the backend's check-type identifiers (Check.Name() return
// values) to a human-readable label for the timeline row. New
// posture checks added in the backend will fall through to the
// raw identifier — fine as a default, can be added here when the
// check ships.
const CHECK_LABELS: Record<string, string> = {
  EndpointSecurityCheck: "Endpoint Security (MDM/EDR)",
  NBVersionCheck: "openZro client version",
  OSVersionCheck: "OS version",
  ProcessCheck: "Required process",
  PeerNetworkRangeCheck: "Peer network range",
  GeoLocationCheck: "Geolocation",
  ScheduleCheck: "Schedule",
};

function labelFor(checkType: string): string {
  return CHECK_LABELS[checkType] ?? checkType;
}

export const PostureEvaluationsSection = ({ peerID }: Props) => {
  const { data, isLoading, mutate } = useFetchApi<PostureEvaluation[]>(
    `/peers/${peerID}/posture-evaluations`,
  );

  const rows = data ?? [];

  return (
    <div className="flex flex-col gap-4 px-8 py-6">
      <div className="flex items-start justify-between gap-4">
        <p className="max-w-2xl text-[13px] leading-[1.55] text-oz2-text-muted">
          Every posture check that has run against this peer in the last 24
          hours, newest first. Failures carry the exact reason the check
          returned — the same string the management server logs at Info on
          denial.
        </p>
        <button
          type="button"
          onClick={() => mutate()}
          className="inline-flex items-center gap-1.5 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 py-1.5 text-[12.5px] font-medium text-oz2-text-2 transition-colors hover:bg-oz2-hover hover:text-oz2-text"
          aria-label="Refresh posture evaluations"
        >
          <RefreshCw size={12} />
          Refresh
        </button>
      </div>

      <div className="max-w-4xl">
        {isLoading ? (
          <PostureSkeleton />
        ) : rows.length === 0 ? (
          <EmptyState />
        ) : (
          <ul className="divide-y divide-oz2-border-soft rounded-oz2-input border border-oz2-border bg-oz2-surface">
            {rows.map((row) => (
              <PostureRow key={row.id} row={row} />
            ))}
          </ul>
        )}
      </div>
    </div>
  );
};

function PostureRow({ row }: { row: PostureEvaluation }) {
  const when = dayjs(row.evaluated_at);
  const relative = dayjs().to(when);
  const absolute = when.format("D MMMM, YYYY [at] HH:mm:ss");

  // Result chip: green check on pass, red x on fail. The reason
  // text only shows on fail — on pass we leave it blank so the eye
  // skips to the failed rows naturally.
  const compliant = row.compliant;

  return (
    <li className="flex items-start gap-4 px-4 py-3">
      <div
        className={
          compliant
            ? "mt-0.5 grid h-7 w-7 shrink-0 place-items-center rounded-full bg-oz2-ok-bg text-oz2-ok"
            : "mt-0.5 grid h-7 w-7 shrink-0 place-items-center rounded-full bg-oz2-err-bg text-oz2-err"
        }
      >
        {compliant ? <CheckIcon size={14} /> : <XIcon size={14} />}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <span className="text-[13.5px] font-medium text-oz2-text">
            {labelFor(row.check_type)}
          </span>
          <span
            className="text-[12px] text-oz2-text-faint"
            title={absolute}
          >
            {relative}
          </span>
        </div>
        {!compliant && row.reason && (
          <p className="mt-1 break-all font-mono text-[11.5px] leading-[1.55] text-oz2-text-muted">
            {row.reason}
          </p>
        )}
      </div>
    </li>
  );
}

function PostureSkeleton() {
  return (
    <div className="space-y-2 rounded-oz2-input border border-oz2-border bg-oz2-surface p-4">
      {Array.from({ length: 4 }).map((_, i) => (
        <div
          key={i}
          className="flex items-center gap-4 py-1"
          aria-hidden="true"
        >
          <div className="h-7 w-7 shrink-0 rounded-full bg-oz2-bg-sunken" />
          <div className="flex-1 space-y-1">
            <div className="h-3 w-2/3 rounded bg-oz2-bg-sunken" />
            <div className="h-2.5 w-1/3 rounded bg-oz2-bg-sunken" />
          </div>
        </div>
      ))}
    </div>
  );
}

function EmptyState() {
  return (
    <div className="grid place-items-center gap-3 rounded-oz2-input border border-oz2-border-soft bg-oz2-surface px-8 py-12 text-center">
      <div className="grid h-10 w-10 place-items-center rounded-full bg-oz2-acc-soft text-oz2-acc-text">
        <ShieldCheck size={18} />
      </div>
      <p className="text-[13px] text-oz2-text-muted">
        No posture evaluations recorded for this peer yet.
        <br />
        Evaluations are stored whenever a policy with a posture check
        targets this peer.
      </p>
      <p className="inline-flex items-center gap-1.5 text-[12px] text-oz2-text-faint">
        <ShieldAlert size={11} />
        Records older than 24 hours are purged automatically.
      </p>
    </div>
  );
}
