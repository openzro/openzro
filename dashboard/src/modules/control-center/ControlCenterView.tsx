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
import { Filter } from "lucide-react";
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
  // Controlled so the focus card in the canvas can open this picker.
  const [pickerOpen, setPickerOpen] = useState(false);

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

  const viewLabel = VIEWS.find((v) => v.value === view)?.label ?? "focus";
  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* Full-width toolbar strip over the canvas (hifi handoff): the
          breadcrumb in the shell already names the page, so the
          in-page title is dropped to give the graph 100% of the area.
          The focus picker is a compact filter pill, not a wide
          input. */}
      <div
        className="flex flex-wrap items-center gap-3 border-b border-oz2-border
          bg-oz2-bg-sunken px-5 py-2.5"
      >
        <OzTabs value={view} onValueChange={onViewChange}>
          <OzTabsList>
            {VIEWS.map((v) => (
              <OzTabsTrigger key={v.value} value={v.value}>
                {v.label}
              </OzTabsTrigger>
            ))}
          </OzTabsList>
        </OzTabs>

        <OzSelect
          value={focusId}
          onValueChange={onFocusChange}
          open={pickerOpen}
          onOpenChange={setPickerOpen}
        >
          <OzSelectTrigger
            className="!h-8 !w-auto min-w-[200px] gap-2 !rounded-full !px-3
              text-xs text-oz2-text-2"
          >
            <Filter className="h-3.5 w-3.5 shrink-0 text-oz2-text-muted" />
            <OzSelectValue
              placeholder={`Filter ${viewLabel.toLowerCase()}…`}
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

        <p className="ml-auto hidden text-[11px] text-oz2-text-muted lg:block">
          Read-only topology — who reaches what, through which policy,
          on which ports.
        </p>
      </div>

      <div className="relative min-h-0 flex-1 p-4">
        <ControlCenterBody
          view={view}
          focusId={focusId}
          isLoading={isLoading}
          graph={graph}
          onFocusNode={onFocusNode}
          onOpenPicker={() => setPickerOpen(true)}
          onRefresh={() => {
            void mutate();
          }}
          onPolicyOpen={(policyId) => {
            // Round-trip (F1): tell the editor to return to THIS
            // focus, not the access-control list, so the audit loop
            // closes.
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
    </div>
  );
}

function Centered({
  children,
  tone = "muted",
}: {
  children: React.ReactNode;
  tone?: "muted" | "faint" | "err";
}) {
  const cls =
    tone === "err"
      ? "text-oz2-err"
      : tone === "faint"
        ? "text-oz2-text-faint"
        : "text-oz2-text-muted";
  return (
    <div
      className={`flex h-full items-center justify-center px-6 text-center text-sm ${cls}`}
    >
      <span className="max-w-md">{children}</span>
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
  onOpenPicker,
}: {
  view: FocusType;
  focusId: string;
  isLoading: boolean;
  graph?: ControlCenterGraph;
  onFocusNode: (v: FocusType, id: string) => void;
  onPolicyOpen: (policyId: string) => void;
  onRefresh: () => void;
  onOpenPicker: () => void;
}) {
  if (focusId === "") {
    return (
      <Centered tone="faint">
        Pick a {view} from the filter to render its topology.
      </Centered>
    );
  }
  if (isLoading) {
    return <Centered>Resolving access…</Centered>;
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
      <Centered tone="err">
        Could not resolve the topology for the selected {view} — it
        may no longer exist, or the request failed. Re-select a focus
        or retry.
      </Centered>
    );
  }
  // Defensive at the API boundary: the backend now always emits
  // arrays, but a nil slice would marshal as JSON null — guard so
  // .length can never throw (#39 v2 review).
  const edges = graph.edges ?? [];
  if (edges.length === 0) {
    return (
      <Centered>
        This {graph.focus.type} reaches nothing through any policy
        right now.
      </Centered>
    );
  }

  return (
    <div className="h-full min-h-0">
      <ControlCenterGraphCanvas
        graph={graph}
        onEdgeClick={onPolicyOpen}
        onFocusNode={onFocusNode}
        onRefresh={onRefresh}
        onOpenPicker={onOpenPicker}
      />
    </div>
  );
}
