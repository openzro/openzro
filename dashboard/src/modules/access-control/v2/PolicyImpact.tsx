"use client";

import * as React from "react";
import OzCard from "@/components/v2/OzCard";
import { Group } from "@/interfaces/Group";
import { PolicyRuleResource } from "@/interfaces/Policy";

// PolicyImpact — companion card to PolicyLivePreview in the right rail.
// Mirrors the handoff Impact card (screens-4 ACLPolicyEditorScreen)
// with the two stats we can derive client-side from groups: how many
// peers would initiate connections under this policy, and how many
// destinations they'd reach.
//
// "Estimated hits" from the handoff is dropped — the dashboard has
// no aggregate-policy-traffic API today, so we'd be showing made-up
// numbers. Bring it back when there's a real signal to plot.

type Props = {
  sourceGroups: Group[];
  destinationGroups: Group[];
  destinationResource?: PolicyRuleResource;
};

export default function PolicyImpact({
  sourceGroups,
  destinationGroups,
  destinationResource,
}: Readonly<Props>) {
  const affectedPeers = React.useMemo(
    () => sumPeers(sourceGroups),
    [sourceGroups],
  );

  const reaches = React.useMemo(() => {
    if (destinationResource) {
      return {
        value: 1,
        label: "service",
        sub: "single resource target",
      };
    }
    const peers = sumPeers(destinationGroups);
    const resources = sumResources(destinationGroups);
    const total = peers + resources;
    const groupSub =
      destinationGroups.length === 0
        ? "no destination groups yet"
        : destinationGroups.length === 1
          ? "in 1 destination group"
          : `in ${destinationGroups.length} destination groups`;
    return {
      value: total,
      label:
        resources > 0 && peers === 0
          ? "services"
          : resources === 0
            ? "peers"
            : "endpoints",
      sub: groupSub,
    };
  }, [destinationGroups, destinationResource]);

  return (
    <OzCard>
      <div className="font-mono text-[10.5px] uppercase tracking-[0.1em] text-oz2-text-faint mb-3">
        Impact
      </div>
      <div className="flex flex-col">
        <Row
          label="Affected peers"
          value={`${affectedPeers} ${affectedPeers === 1 ? "peer" : "peers"}`}
          sub={
            sourceGroups.length === 0
              ? "no source groups yet"
              : sourceGroups.length === 1
                ? "in 1 source group"
                : `in ${sourceGroups.length} source groups`
          }
        />
        <Row
          label="Reaches"
          value={`${reaches.value} ${reaches.label}`}
          sub={reaches.sub}
          last
        />
      </div>
    </OzCard>
  );
}

function Row({
  label,
  value,
  sub,
  last,
}: {
  label: string;
  value: string;
  sub: string;
  last?: boolean;
}) {
  return (
    <div
      className={
        "flex items-center justify-between gap-3 py-2.5" +
        (last ? "" : " border-b border-oz2-border-soft")
      }
    >
      <div className="min-w-0">
        <div className="text-[12.5px] text-oz2-text-2">{label}</div>
        <div className="mt-0.5 text-[11px] text-oz2-text-faint">{sub}</div>
      </div>
      <div className="shrink-0 font-mono text-[13px] font-medium text-oz2-text">
        {value}
      </div>
    </div>
  );
}

function sumPeers(groups: Group[]): number {
  return groups.reduce((acc, g) => {
    if (typeof g.peers_count === "number") return acc + g.peers_count;
    if (Array.isArray(g.peers)) return acc + g.peers.length;
    return acc;
  }, 0);
}

function sumResources(groups: Group[]): number {
  return groups.reduce((acc, g) => {
    if (typeof g.resources_count === "number") return acc + g.resources_count;
    if (Array.isArray(g.resources)) return acc + g.resources.length;
    return acc;
  }, 0);
}
