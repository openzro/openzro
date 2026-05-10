"use client";

import InlineLink from "@components/InlineLink";
import Paragraph from "@components/Paragraph";
import { ExternalLinkIcon } from "lucide-react";
import React from "react";
import PeersProvider, { usePeers } from "@/contexts/PeersProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { useUsers } from "@/contexts/UsersProvider";
import PeersTableV2 from "@/modules/peers/v2/PeersTableV2";
import { SetupModalContent } from "@/modules/setup-openzro-modal/SetupModal";

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

// PeersBlockedView is intentionally kept on the legacy primitives
// (Paragraph, InlineLink, SetupModalContent) for this commit — it's
// an empty-state fallback shown when usePermissions().isRestricted is
// true, which is rare. Phase 4.3 (or later) re-paints it in v2 once
// every other Peers surface is migrated.
function PeersBlockedView() {
  return (
    <div className="flex flex-col items-center justify-center">
      <div className="p-default py-6 max-w-3xl text-center">
        <h1>Add new device to your network</h1>
        <Paragraph className="inline">
          To get started, install Openzro and log in using your email account.
          After that you should be connected. If you have further questions
          check out our{" "}
          <InlineLink
            href="https://docs.openzro.io/how-to/getting-started#installation"
            target="_blank"
          >
            Installation Guide
            <ExternalLinkIcon size={12} />
          </InlineLink>
        </Paragraph>
      </div>
      <div className="px-3 pt-1 pb-8 max-w-3xl w-full">
        <div className="rounded-md border border-nb-gray-900/70 grid w-full bg-nb-gray-930/40">
          <SetupModalContent header={false} footer={false} />
        </div>
      </div>
    </div>
  );
}
