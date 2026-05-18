"use client";

import { Modal } from "@components/modal/Modal";
import {
  OzTabs,
  OzTabsList,
  OzTabsTrigger,
} from "@components/v2/OzTabs";
import useFetchApi from "@utils/api";
import {
  Monitor,
  Network,
  User as UserIcon,
  Users as UsersIcon,
} from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import React, { useEffect, useMemo, useState } from "react";
import {
  ControlCenterGraph,
  FocusType,
} from "@/interfaces/ControlCenter";
import { Group } from "@/interfaces/Group";
import { NetworkResource } from "@/interfaces/Network";
import { Peer } from "@/interfaces/Peer";
import { Policy } from "@/interfaces/Policy";
import { User } from "@/interfaces/User";
import { AccessControlModalContent } from "@/modules/access-control/AccessControlModal";
import ControlCenterGraphCanvas from "@/modules/control-center/ControlCenterGraphCanvas";

// Control Center data layer (ADR-0017 2026-05-18b — v2 topology).
// Owns the focus tab (peer|user|group|network), the focus-node
// selector (sourced per tab), and the graph fetch. The columnar
// xyflow canvas is the primary view; the textual reachable list
// stays as the accessible / no-graph fallback.

const VIEWS: {
  value: FocusType;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
}[] = [
  { value: "peer", label: "Peer", icon: Monitor },
  { value: "user", label: "User", icon: UserIcon },
  { value: "group", label: "Group", icon: UsersIcon },
  { value: "network", label: "Networks", icon: Network },
];

const TABS_H = 48;

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
  const { data: policies, mutate: mutatePolicies } = useFetchApi<
    Policy[]
  >("/policies", true);

  // Policy edit happens in a modal ON the Control Center, not by
  // navigating to /access-control — the operator must not lose the
  // topology context (owner-decided 2026-05-18).
  const [editPolicyId, setEditPolicyId] = useState<string | null>(null);
  const editPolicy = useMemo(
    () => (policies ?? []).find((p) => p.id === editPolicyId) ?? null,
    [policies, editPolicyId],
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

  // NetBird-style: a tab always renders something. When no valid
  // focus is selected (tab just opened, or a stale URL id that
  // doesn't belong to this view), auto-pick the first entity; the
  // user then switches via the picker on the focus card.
  useEffect(() => {
    if (options.length === 0) return;
    if (focusId && options.some((o) => o.id === focusId)) return;
    const first = options[0].id;
    setFocusId(first);
    syncUrl(view, first);
    // syncUrl is a stable router.replace wrapper; re-running on its
    // identity would loop. Intentionally keyed on the data only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [options, focusId, view]);

  return (
    <div className="flex h-full min-h-0 flex-col p-3">
      {/* One self-contained card: tabs pinned at the TOP (like the
          legend/footer at the bottom), graph filling everything in
          between — no external chrome, maximum area. The tab bar
          lives in the persistent card shell so it stays visible
          through loading / empty / error states too. */}
      <div
        className="oz-cc-scroll relative min-h-0 flex-1 overflow-hidden
          rounded-oz2-card border border-oz2-border-strong bg-oz2-bg"
      >
        <div
          className="absolute inset-x-0 top-0 z-30 flex items-center gap-3
            border-b border-oz2-border bg-oz2-surface/80 px-3 backdrop-blur-md"
          style={{ height: TABS_H }}
        >
          <OzTabs value={view} onValueChange={onViewChange}>
            <OzTabsList>
              {VIEWS.map((v) => (
                <OzTabsTrigger key={v.value} value={v.value}>
                  <v.icon className="h-3.5 w-3.5" />
                  {v.label}
                </OzTabsTrigger>
              ))}
            </OzTabsList>
          </OzTabs>
          <span
            className="font-mono ml-auto rounded-md bg-oz2-acc-soft px-2
              py-0.5 text-[10px] uppercase text-oz2-acc-text"
          >
            Beta
          </span>
        </div>

        <div
          className="absolute inset-x-0 bottom-0"
          style={{ top: TABS_H }}
        >
          <ControlCenterBody
            view={view}
            focusId={focusId}
            isLoading={isLoading}
            graph={graph}
            onFocusNode={onFocusNode}
            focusOptions={options}
            onPickFocus={onFocusChange}
            onRefresh={() => {
              void mutate();
            }}
            onPolicyOpen={(policyId) => setEditPolicyId(policyId)}
          />
        </div>
      </div>

      <Modal
        open={!!editPolicy}
        onOpenChange={(s) => {
          if (!s) setEditPolicyId(null);
        }}
      >
        {editPolicy && (
          <AccessControlModalContent
            key={editPolicy.id}
            policy={editPolicy}
            onSuccess={async () => {
              setEditPolicyId(null);
              await Promise.all([mutate(), mutatePolicies()]);
            }}
          />
        )}
      </Modal>
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
  focusOptions,
  onPickFocus,
}: {
  view: FocusType;
  focusId: string;
  isLoading: boolean;
  graph?: ControlCenterGraph;
  onFocusNode: (v: FocusType, id: string) => void;
  onPolicyOpen: (policyId: string) => void;
  onRefresh: () => void;
  focusOptions: { id: string; label: string }[];
  onPickFocus: (id: string) => void;
}) {
  if (focusId === "") {
    return (
      <Centered tone="faint">
        No {view} available to inspect.
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
  // Even with zero edges we still render the canvas: the focus card
  // (and its picker) must stay on screen so the operator can switch
  // to another entity instead of being dropped onto a dead-end text
  // message (#39 v2 review). The empty state is conveyed by the
  // footer ("0 connections") and the empty columns.
  return (
    <div className="h-full min-h-0">
      <ControlCenterGraphCanvas
        graph={graph}
        onEdgeClick={onPolicyOpen}
        onFocusNode={onFocusNode}
        onRefresh={onRefresh}
        focusOptions={focusOptions}
        focusId={focusId}
        onPickFocus={onPickFocus}
      />
    </div>
  );
}
