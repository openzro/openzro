import dayjs from "dayjs";
import React from "react";
import { PeerLoginApproval } from "@/modules/mfa/peer-login/api";

// PeerLoginDeviceCard shows what is being approved. What the device
// reported (its name and OS) is labelled as such, apart from what openZro
// observed (the address it connected from), so a user can tell an
// attacker's machine from their own even if the name was faked.

type Props = {
  approval: PeerLoginApproval;
};

function Row({
  label,
  children,
}: Readonly<{ label: string; children: React.ReactNode }>) {
  return (
    <div className="flex items-start justify-between gap-4 py-1.5">
      <span className="shrink-0 text-[12px] text-oz2-text-muted">{label}</span>
      <span className="text-right text-[13px] font-medium text-oz2-text break-all">
        {children}
      </span>
    </div>
  );
}

export default function PeerLoginDeviceCard({ approval }: Readonly<Props>) {
  const place = [approval.geo_city, approval.geo_country]
    .filter(Boolean)
    .join(", ");
  return (
    <div
      className="divide-y divide-oz2-border-soft rounded-oz2-input border border-oz2-border bg-oz2-bg-soft px-3"
      data-cy="peer-login-device"
    >
      <div className="py-1">
        <p className="pt-1 font-mono text-[10.5px] uppercase tracking-[0.08em] text-oz2-text-faint">
          Reported by the device
        </p>
        <Row label="Device name">{approval.hostname || "—"}</Row>
        <Row label="Operating system">{approval.os || "—"}</Row>
      </div>
      <div className="py-1">
        <p className="pt-1 font-mono text-[10.5px] uppercase tracking-[0.08em] text-oz2-text-faint">
          Observed by openZro
        </p>
        <Row label="Connected from">
          {approval.ip || "—"}
          {place && (
            <span className="block text-[12px] font-normal text-oz2-text-muted">
              {place}
            </span>
          )}
        </Row>
        <Row label="Device key">
          <span className="font-mono text-[12px]">
            {approval.key_fingerprint}
          </span>
        </Row>
        <Row label="Requested">
          {dayjs(approval.created_at).format("HH:mm:ss")}
        </Row>
        <Row label="Sign-in">
          {approval.new_peer
            ? "New device"
            : "Returning device (session expired)"}
        </Row>
        <Row label="Account">
          {approval.masked_email}
          <span className="block text-[12px] font-normal text-oz2-text-muted">
            {approval.account_domain}
          </span>
        </Row>
      </div>
    </div>
  );
}
