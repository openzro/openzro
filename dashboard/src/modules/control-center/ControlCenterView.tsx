"use client";

import {
  OzSelect,
  OzSelectContent,
  OzSelectItem,
  OzSelectTrigger,
  OzSelectValue,
} from "@components/v2/OzSelect";
import {
  OzTabs,
  OzTabsList,
  OzTabsTrigger,
} from "@components/v2/OzTabs";
import useFetchApi from "@utils/api";
import React, { useMemo, useState } from "react";
import {
  ControlCenterGraph,
  FocusType,
} from "@/interfaces/ControlCenter";
import { Group } from "@/interfaces/Group";
import { Peer } from "@/interfaces/Peer";

// Control Center data layer (ADR-0017 Phase 2, P3). Owns the focus
// view (peer|group — v1 scope; Users/inverse-Networks are v2), the
// focus-node selector, and the graph fetch. The xyflow canvas (P4)
// replaces the textual reachable list below; that list stays as the
// accessible / no-JS-graph fallback.

export default function ControlCenterView() {
  const [view, setView] = useState<FocusType>("peer");
  const [focusId, setFocusId] = useState<string>("");

  const { data: peers } = useFetchApi<Peer[]>("/peers", true);
  const { data: groups } = useFetchApi<Group[]>("/groups", true);

  const { data: graph, isLoading } = useFetchApi<ControlCenterGraph>(
    `/control-center/${view}/${focusId}`,
    true,
    true,
    focusId !== "",
  );

  const options = useMemo(() => {
    if (view === "peer") {
      return (peers ?? [])
        .filter((p) => p.id)
        .map((p) => ({ id: p.id as string, label: p.name || (p.id as string) }));
    }
    return (groups ?? [])
      .filter((g) => g.id)
      .map((g) => ({ id: g.id as string, label: g.name }));
  }, [view, peers, groups]);

  const onViewChange = (v: string) => {
    setView(v as FocusType);
    setFocusId(""); // a peer id is not a valid group focus and vice-versa
  };

  return (
    <div className="flex h-full flex-col gap-4 p-6">
      <div>
        <h1 className="text-xl font-semibold text-oz2-text">
          Control Center
        </h1>
        <p className="mt-1 text-sm text-oz2-text-muted">
          Read-only access graph — what a {view} reaches, through which
          policy, and what is policy-permitted but posture-blocked.
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <OzTabs value={view} onValueChange={onViewChange}>
          <OzTabsList>
            <OzTabsTrigger value="peer">Peers</OzTabsTrigger>
            <OzTabsTrigger value="group">Groups</OzTabsTrigger>
          </OzTabsList>
        </OzTabs>

        <OzSelect value={focusId} onValueChange={setFocusId}>
          <OzSelectTrigger className="w-[280px]">
            <OzSelectValue
              placeholder={`Select a ${view} to inspect…`}
            />
          </OzSelectTrigger>
          <OzSelectContent>
            {options.map((o) => (
              <OzSelectItem key={o.id} value={o.id}>
                {o.label}
              </OzSelectItem>
            ))}
          </OzSelectContent>
        </OzSelect>
      </div>

      <ControlCenterBody
        focusId={focusId}
        isLoading={isLoading}
        graph={graph}
      />
    </div>
  );
}

function ControlCenterBody({
  focusId,
  isLoading,
  graph,
}: {
  focusId: string;
  isLoading: boolean;
  graph?: ControlCenterGraph;
}) {
  if (focusId === "") {
    return (
      <div className="text-sm text-oz2-text-faint">
        Pick a focus node above to render its access graph.
      </div>
    );
  }
  if (isLoading) {
    return (
      <div className="text-sm text-oz2-text-muted">Resolving access…</div>
    );
  }
  if (!graph || graph.edges.length === 0) {
    return (
      <div className="text-sm text-oz2-text-muted">
        This {graph?.focus.type ?? "node"} reaches nothing right now.
      </div>
    );
  }

  // P3 textual fallback — replaced by the xyflow canvas in P4, kept as
  // the accessible representation.
  return (
    <ul className="flex flex-col gap-1 text-sm text-oz2-text">
      {graph.edges.map((e, i) => (
        <li key={`${e.from}-${e.to}-${e.policyId ?? e.permitSource}-${i}`}>
          → <span className="font-medium">{e.to}</span>{" "}
          <span className="text-oz2-text-muted">
            ({e.state}, {e.permitSource}
            {e.policyName ? `: ${e.policyName}` : ""}
            {e.protocol ? `, ${e.protocol}` : ""})
          </span>
        </li>
      ))}
    </ul>
  );
}
