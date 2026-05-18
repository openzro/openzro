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
import { useRouter, useSearchParams } from "next/navigation";
import React, { useMemo, useState } from "react";
import {
  ControlCenterGraph,
  FocusType,
} from "@/interfaces/ControlCenter";
import { Group } from "@/interfaces/Group";
import { Peer } from "@/interfaces/Peer";
import ControlCenterGraphCanvas from "@/modules/control-center/ControlCenterGraphCanvas";

// Control Center data layer (ADR-0017 Phase 2, P3). Owns the focus
// view (peer|group — v1 scope; Users/inverse-Networks are v2), the
// focus-node selector, and the graph fetch. The xyflow canvas (P4)
// replaces the textual reachable list below; that list stays as the
// accessible / no-JS-graph fallback.

export default function ControlCenterView() {
  const router = useRouter();
  const sp = useSearchParams();

  // Focus lives in the URL so the editor round-trip (F1) can return
  // to the SAME focus: navigating to the policy editor and back
  // remounts this view, which re-initialises from these params and
  // re-fetches the graph.
  const [view, setView] = useState<FocusType>(
    sp.get("view") === "group" ? "group" : "peer",
  );
  const [focusId, setFocusId] = useState<string>(sp.get("focus") ?? "");

  const syncUrl = (v: FocusType, f: string) => {
    const q = new URLSearchParams({ view: v });
    if (f) q.set("focus", f);
    router.replace(`/control-center?${q.toString()}`);
  };

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
    const fv = v as FocusType;
    setView(fv);
    setFocusId(""); // a peer id is not a valid group focus and vice-versa
    syncUrl(fv, "");
  };

  const onFocusChange = (f: string) => {
    setFocusId(f);
    syncUrl(view, f);
  };

  // Click-to-switch-focus (Phase 3): clicking a peer/group node in the
  // graph re-centres on it (URL synced → graph refetches on the new
  // focus, same path as the selector).
  const onFocusNode = (v: FocusType, id: string) => {
    setView(v);
    setFocusId(id);
    syncUrl(v, id);
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

        <OzSelect value={focusId} onValueChange={onFocusChange}>
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
        view={view}
        focusId={focusId}
        isLoading={isLoading}
        graph={graph}
        onFocusNode={onFocusNode}
        onPolicyOpen={(policyId) => {
          // Round-trip (F1): tell the editor to return to THIS focus,
          // not the access-control list, so the audit loop closes.
          const returnTo = `/control-center?view=${view}&focus=${encodeURIComponent(
            focusId,
          )}`;
          router.push(
            `/access-control/edit?id=${policyId}&returnTo=${encodeURIComponent(
              returnTo,
            )}`,
          );
        }}
      />
    </div>
  );
}

function ControlCenterBody({
  view,
  focusId,
  isLoading,
  graph,
  onFocusNode,
  onPolicyOpen,
}: {
  view: FocusType;
  focusId: string;
  isLoading: boolean;
  graph?: ControlCenterGraph;
  onFocusNode: (v: FocusType, id: string) => void;
  onPolicyOpen: (policyId: string) => void;
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
  // Absence of evidence is NOT evidence of absence (audit tool). The
  // backend returns a graph OBJECT (edges possibly []) for a valid
  // focus; `graph` undefined after load means the request failed /
  // the focus is gone. AND because the shared useFetchApi sets
  // keepPreviousData:true, a failed fetch for focus B can leave
  // `graph` holding focus A's data — showing one focus's access under
  // another's selector is worse than an error. So accept the graph
  // ONLY when its own focus identity matches the current selection;
  // anything else is treated as unresolved, never as data or "nothing"
  // (#51-r2 F1).
  if (
    !graph ||
    graph.focus.id !== focusId ||
    graph.focus.type !== view
  ) {
    return (
      <div className="text-sm text-oz2-err">
        Could not resolve the access graph for the selected {view} —
        it may no longer exist, or the request failed. Re-select a
        focus or retry.
      </div>
    );
  }
  if (graph.edges.length === 0) {
    return (
      <div className="text-sm text-oz2-text-muted">
        This {graph.focus.type} reaches nothing right now.
      </div>
    );
  }

  // P4: the xyflow canvas is the primary view; the textual list is
  // kept as the accessible / no-graph fallback in a <details>.
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <ControlCenterGraphCanvas
        graph={graph}
        onEdgeClick={onPolicyOpen}
        onFocusNode={onFocusNode}
      />
      <details className="text-sm text-oz2-text-muted">
        <summary className="cursor-pointer select-none">
          Reachable, as a list ({graph.edges.length})
        </summary>
        <ul className="mt-2 flex flex-col gap-1 text-oz2-text">
          {graph.edges.map((e, i) => (
            <li
              key={`${e.from}-${e.to}-${e.policyId ?? e.permitSource}-${i}`}
            >
              → <span className="font-medium">{e.to}</span>{" "}
              <span className="text-oz2-text-muted">
                ({e.state},{" "}
                {e.permitSource === "policy" && e.policyId ? (
                  <button
                    type="button"
                    onClick={() => onPolicyOpen(e.policyId as string)}
                    className="text-oz2-acc underline underline-offset-2"
                  >
                    {e.policyName || "policy"}
                  </button>
                ) : (
                  e.permitSource
                )}
                {e.protocol ? `, ${e.protocol}` : ""})
              </span>
            </li>
          ))}
        </ul>
      </details>
    </div>
  );
}
