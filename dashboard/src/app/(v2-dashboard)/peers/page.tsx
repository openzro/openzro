"use client";

import { ExternalLinkIcon } from "lucide-react";
import React from "react";
import PeersProvider, { usePeers } from "@/contexts/PeersProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { useUsers } from "@/contexts/UsersProvider";
import PeersTableV2 from "@/modules/peers/v2/PeersTableV2";
import SetupModalV2 from "@/modules/setup-openzro-modal/v2/SetupModalV2";

// Peers — phase-4.2 entry point. The wrapping chrome (OzShell +
// OzSidebar + OzTopbar) lives in (v2-dashboard)/layout.tsx →
// V2DashboardLayout. This component owns the page-body composition
// and the permissions / restricted-view branching.

export default function Peers() {
  const { isRestricted } = usePermissions();

  return isRestricted ? (
    <PeersBlockedView />
  ) : (
    <PeersProvider>
      <PeersView />
    </PeersProvider>
  );
}

function PeersView() {
  const { peers, isLoading } = usePeers();
  const { users } = useUsers();

  // Mirror the legacy enrichment so user-related cells (Name, search)
  // can read peer.user.email/name without an extra round-trip.
  const peersWithUser = peers?.map((peer) => {
    if (!users) return peer;
    return {
      ...peer,
      user: users.find((u) => u.id === peer.user_id),
    };
  });

  return <PeersTableV2 peers={peersWithUser} isLoading={isLoading} />;
}

// PeersBlockedView — empty-state landing shown to restricted (peer-
// only) users who hit /peers. Mirrors the v2 paint of SetupModalV2's
// Hero (gradient icon + heading + intro paragraph) and embeds the
// same install body inline so a non-admin user can self-serve a
// device install without needing the admin-only "Add Peer" modal.
function PeersBlockedView() {
  return (
    <div className="mx-auto max-w-[640px] space-y-6 p-8">
      <header className="text-center">
        <div
          className="mx-auto mb-3.5 grid h-11 w-11 place-items-center rounded-[12px] text-white shadow-oz2-acc"
          style={{
            background: "linear-gradient(135deg, #8b5cf6 0%, #4c1d95 100%)",
          }}
        >
          <svg
            viewBox="0 0 24 24"
            width={22}
            height={22}
            fill="none"
            stroke="currentColor"
            strokeWidth={2.2}
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <rect x={3} y={4} width={18} height={14} rx={3} />
            <path d="M7 10l3 3-3 3" />
            <path d="M13 16h4" />
          </svg>
        </div>
        <h1 className="text-[24px] font-semibold tracking-tight text-oz2-text">
          Add a new device to your network
        </h1>
        <p className="mx-auto mt-1.5 max-w-[480px] text-[14.5px] leading-[1.5] text-oz2-text-muted">
          To get started, install{" "}
          <span className="oz-wordmark">
            open<span className="oz-z">Z</span>ro
          </span>{" "}
          and log in with your email account. If you have further questions
          check out our{" "}
          <a
            href="https://docs.openzro.io/how-to/getting-started#installation"
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 text-oz2-acc hover:underline"
          >
            Installation Guide
            <ExternalLinkIcon size={12} />
          </a>
          .
        </p>
      </header>
      <SetupModalV2 inline />
    </div>
  );
}
