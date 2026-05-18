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
import { NetworkResource } from "@/interfaces/Network";
import { Peer } from "@/interfaces/Peer";
import { User } from "@/interfaces/User";
import ControlCenterGraphCanvas from "@/modules/control-center/ControlCenterGraphCanvas";

// Control Center data layer (ADR-0017 2026-05-18b — v2 topology).
// Owns the focus tab (peer|user|group|network), the focus-node
// selector (sourced per tab), and the graph fetch. The columnar
// xyflow canvas is the primary view; the textual reachable list
// stays as the accessible / no-graph fallback.

const VIEWS: { value: FocusType; label: string }[] = [
  { value: "peer", label: "Peer" },
  { value: "user", label: "User" },
  { value: "group", label: "Group" },
  { value: "network", label: "Networks" },
];

function isFocusType(v: string | null): v is FocusType {
  return v === "peer" || v === "user" || v === "group" || v === "network";
}

export default function ControlCenterView() {
  const router = useRouter();
  const sp = useSearchParams();

  // Focus lives in the URL so the editor round-trip (F1) can return
  // to the SAME focus: navigating to the policy editor and back
  // remounts this view, which re-initialises from these params and
  // re-fetches the graph.
  const [view, setView] = useState<FocusType>(
    isFocusType(sp.get("view")) ? (sp.get("view") as FocusType) : "peer",
  );
  const [focusId, setFocusId] = useState<string>(sp.get("focus") ?? "");

  const syncUrl = (v: FocusType, f: string) => {
    const q = new URLSearchParams({ view: v });
    if (f) q.set("focus", f);
    router.replace(`/control-center?${q.toString()}`);
  };

  const { data: peers } = useFetchApi<Peer[]>("/peers", true);
  const { data: groups } = useFetchApi<Group[]>("/groups", true);
  const { data: users } = useFetchApi<User[]>("/users", true);
  const { data: resources } = useFetchApi<NetworkResource[]>(
    "/networks/resources",
    true,
  );

  const {
    data: graph,
    isLoading,
    mutate,
  } = useFetchApi<ControlCenterGraph>(
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
    if (view === "user") {
      return (users ?? [])
        .filter((u) => u.id)
        .map((u) => ({ id: u.id, label: u.name || u.email || u.id }));
    }
    if (view === "network") {
      return (resources ?? [])
        .filter((r) => r.id)
        .map((r) => ({ id: r.id, label: r.name || r.address || r.id }));
    }
    return (groups ?? [])
      .filter((g) => g.id)
      .map((g) => ({ id: g.id as string, label: g.name }));
  }, [view, peers, groups, users, resources]);

  const onViewChange = (v: string) => {
    if (!isFocusType(v)) return;
    setView(v);
    setFocusId(""); // an id is scoped to its focus type
    syncUrl(v, "");
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
          Read-only topology — who reaches what, through which policy,
          on which ports, and what is policy-permitted but
          posture-blocked.
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <OzTabs value={view} onValueChange={onViewChange}>
          <OzTabsList>
            {VIEWS.map((v) => (
              <OzTabsTrigger key={v.value} value={v.value}>
                {v.label}
              </OzTabsTrigger>
            ))}
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
        onRefresh={() => {
          void mutate();
        }}
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
  onRefresh,
}: {
  view: FocusType;
  focusId: string;
  isLoading: boolean;
  graph?: ControlCenterGraph;
  onFocusNode: (v: FocusType, id: string) => void;
  onPolicyOpen: (policyId: string) => void;
  onRefresh: () => void;
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
        onRefresh={onRefresh}
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
