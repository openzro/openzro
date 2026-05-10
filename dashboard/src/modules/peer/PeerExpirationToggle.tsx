"use client";

import FullTooltip from "@components/FullTooltip";
import { IconInfoCircle } from "@tabler/icons-react";
import { LockIcon } from "lucide-react";
import * as React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Peer } from "@/interfaces/Peer";
import OzSettingsToggle from "@/modules/settings/v2/OzSettingsToggle";

type Props = {
  peer: Peer;
  value: boolean;
  onChange: (value: boolean) => void;
  title?: string;
  description?: string;
  /**
   * `nested` removes the outer row padding so a parent OzSettingsCard
   * (or a sub-card with its own padding) can compose the toggle
   * without doubling whitespace.
   */
  nested?: boolean;
};

// PeerExpirationToggle — login-expiration / inactivity-expiration
// toggle for /peer detail. Renders in v2 paint via OzSettingsToggle,
// wrapped in a FullTooltip that explains why the toggle is locked
// (setup-key peers can't expire; or the operator lacks the perm).

export const PeerExpirationToggle = ({
  peer,
  value,
  onChange,
  title = "Session Expiration",
  description = "Enable to require SSO login peers to re-authenticate when their session expires after a certain period of time.",
  nested,
}: Props) => {
  const { permission } = usePermissions();
  const disabled = !peer.user_id || !permission.peers.update;

  return (
    <FullTooltip
      content={
        <div className="flex items-center gap-2 !text-nb-gray-300 text-xs">
          {!peer.user_id ? (
            <>
              <IconInfoCircle size={14} />
              <span>
                This setting is disabled for all peers added with an
                setup-key.
              </span>
            </>
          ) : (
            <>
              <LockIcon size={14} />
              <span>
                {`You don't have the required permissions to update this setting.`}
              </span>
            </>
          )}
        </div>
      }
      className="w-full block"
      disabled={!!peer.user_id && permission.peers.update}
    >
      <OzSettingsToggle
        value={value}
        onChange={onChange}
        disabled={disabled}
        label={title}
        desc={description}
        nested={nested}
      />
    </FullTooltip>
  );
};
