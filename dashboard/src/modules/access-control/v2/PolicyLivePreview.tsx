"use client";

import { cn } from "@utils/helpers";
import * as React from "react";
import OzCard from "@/components/v2/OzCard";
import { Group } from "@/interfaces/Group";
import { PolicyRuleResource, PortRange, Protocol } from "@/interfaces/Policy";
import { Direction } from "@/components/ui/PolicyDirection";

// PolicyLivePreview — sticky side-rail visualization of the policy
// currently being edited. Mirrors the handoff "Live preview" card
// (screens-4 ACLPolicyEditorScreen): name + status pills at the top,
// then a From-block / arrow-bridge / To-block path. Reads from
// useAccessControl state passed by the parent so the preview stays
// in lockstep with the form without owning any state of its own.

type Props = {
  name: string;
  enabled: boolean;
  direction: Direction;
  sourceGroups: Group[];
  destinationGroups: Group[];
  destinationResource?: PolicyRuleResource;
  protocol: Protocol;
  ports: number[];
  portRanges: PortRange[];
};

export default function PolicyLivePreview({
  name,
  enabled,
  direction,
  sourceGroups,
  destinationGroups,
  destinationResource,
  protocol,
  ports,
  portRanges,
}: Readonly<Props>) {
  const isBi = direction === "bi";

  // Rule cluster — what the bridge between From and To shows. ICMP and
  // ALL drop ports entirely; tcp/udp render the active port list +
  // ranges. Empty TCP/UDP falls back to "any port" so the operator
  // sees that the policy currently covers the whole range.
  const ruleLines = useRuleLines({ protocol, ports, portRanges });

  return (
    <OzCard>
      <div className="font-mono text-[10.5px] uppercase tracking-[0.1em] text-oz2-text-faint mb-3">
        Live preview
      </div>

      <div className="mb-3 flex flex-wrap items-center gap-2">
        <span className="text-[14px] font-semibold text-oz2-text">
          {name || "Untitled policy"}
        </span>
        <DirectionPill direction={direction} />
        {!enabled && <StatusPill tone="muted">Disabled</StatusPill>}
      </div>

      {/* From block */}
      <Endpoint label="From" groups={sourceGroups} />

      {/* Bridge — vertical rail with the rule cluster + direction
          notice running between From and To. The circle sits over the
          rail at midpoint, accent-bordered to echo the form's
          PolicyDirection arrows. */}
      <Bridge isBi={isBi}>
        {ruleLines.map((line, i) => (
          <span
            key={i}
            className="self-start rounded-[4px] bg-oz2-acc-soft px-2 py-0.5 font-mono text-[11.5px] font-medium text-oz2-acc-text"
          >
            {line}
          </span>
        ))}
        <span className="mt-1 text-[10.5px] italic text-oz2-text-faint">
          {isBi
            ? "Either side may initiate."
            : "Only source initiates; destination cannot reach back."}
        </span>
      </Bridge>

      {/* To block */}
      <Endpoint
        label="To"
        groups={destinationGroups}
        resource={destinationResource}
      />
    </OzCard>
  );
}

function DirectionPill({ direction }: { direction: Direction }) {
  const isBi = direction === "bi";
  return (
    <span className="inline-flex items-center gap-1 rounded-full border border-oz2-acc-soft-2 bg-oz2-acc-soft px-2 py-0.5 text-[11px] font-medium text-oz2-acc-text">
      <span className="font-mono text-[13px] leading-none">
        {isBi ? "↔" : "→"}
      </span>
      {isBi ? "Bidirectional" : "Unidirectional"}
    </span>
  );
}

function StatusPill({
  children,
  tone,
}: {
  children: React.ReactNode;
  tone: "muted";
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full border border-oz2-border-soft bg-oz2-bg-sunken px-2 py-0.5 text-[11px] font-medium",
        tone === "muted" && "text-oz2-text-muted",
      )}
    >
      {children}
    </span>
  );
}

function Endpoint({
  label,
  groups,
  resource,
}: {
  label: string;
  groups: Group[];
  resource?: PolicyRuleResource;
}) {
  const hasContent =
    (resource && resource.id) || (groups && groups.length > 0);

  return (
    <div className="rounded-[10px] border border-oz2-border-soft bg-oz2-bg-sunken px-3 py-2.5">
      <div className="mb-1.5 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-oz2-text-faint">
        {label}
      </div>
      {hasContent ? (
        <div className="flex flex-wrap gap-1.5">
          {resource && resource.id && (
            <Pill mono>{resource.type ?? "resource"}:{resource.id.slice(0, 8)}</Pill>
          )}
          {groups.map((g) => (
            <Pill key={g.id ?? g.name}>{g.name}</Pill>
          ))}
        </div>
      ) : (
        <span className="text-[11.5px] italic text-oz2-text-faint">
          (no groups selected yet)
        </span>
      )}
    </div>
  );
}

function Bridge({
  children,
  isBi,
}: {
  children: React.ReactNode;
  isBi: boolean;
}) {
  // Handoff Bridge: the rail (border-left) lives ~24px from the
  // container's left edge, the circle is absolutely positioned with
  // negative left so its right edge tucks UNDER the rail rather than
  // intruding into the rules column. Content gets a generous left
  // padding so the pills sit comfortably right of the rail with no
  // overlap with the circle's box-shadow ring.
  return (
    <div className="relative ml-6 my-1 flex flex-col gap-1.5 border-l-2 border-oz2-acc py-3.5 pl-[22px] pr-3.5">
      <span
        aria-hidden
        className={cn(
          "absolute top-1/2 grid h-10 w-10 -translate-y-1/2 place-items-center rounded-full",
          "left-[-21px]",
          "border border-oz2-acc bg-oz2-surface text-oz2-acc",
          "font-mono text-[18px] font-semibold leading-none",
          "shadow-[0_0_0_4px_var(--ozv2-surface)]",
        )}
      >
        {isBi ? "↕" : "↓"}
      </span>
      {children}
    </div>
  );
}

function Pill({
  children,
  mono = false,
}: {
  children: React.ReactNode;
  mono?: boolean;
}) {
  return (
    <span
      className={cn(
        "inline-flex max-w-full items-center rounded-full bg-oz2-acc-soft px-2 py-0.5 text-[11px] font-medium text-oz2-acc-text",
        mono && "font-mono text-[10.5px]",
      )}
    >
      <span className="truncate">{children}</span>
    </span>
  );
}

function useRuleLines({
  protocol,
  ports,
  portRanges,
}: {
  protocol: Protocol;
  ports: number[];
  portRanges: PortRange[];
}): string[] {
  return React.useMemo(() => {
    if (protocol === "all") return ["ALL"];
    if (protocol === "icmp") return ["ICMP"];
    const proto = protocol.toUpperCase();
    const portLines = ports.map((p) => `${proto}:${p}`);
    const rangeLines = portRanges.map((r) => `${proto}:${r.start}-${r.end}`);
    const lines = [...portLines, ...rangeLines];
    return lines.length > 0 ? lines : [`${proto} (any port)`];
  }, [protocol, ports, portRanges]);
}
