import { ShieldAlert, ShieldCheck, ShieldX } from "lucide-react";
import React from "react";
import OzCard from "@/components/v2/OzCard";
import { PeerLoginApproval } from "@/modules/mfa/peer-login/api";
import PeerLoginDeviceCard from "@/modules/mfa/peer-login/PeerLoginDeviceCard";

// The approval page's end states: the attempt was approved, connected or
// denied, or the page cannot act on the link.

export type PeerLoginNoticeKind = "no_token" | "invalid" | "gone" | "error";

function Panel({
  icon,
  title,
  children,
  cy,
}: Readonly<{
  icon: React.ReactNode;
  title: string;
  children: React.ReactNode;
  cy: string;
}>) {
  return (
    <OzCard className="w-full space-y-3" data-cy={cy}>
      <div className="flex items-center gap-2 text-[16px] font-semibold text-oz2-text">
        {icon}
        {title}
      </div>
      <div className="space-y-3 text-[13px] text-oz2-text-muted">
        {children}
      </div>
    </OzCard>
  );
}

export function PeerLoginOutcome({
  approval,
}: Readonly<{ approval: PeerLoginApproval }>) {
  switch (approval.state) {
    case "approved":
      return (
        <Panel
          icon={<ShieldCheck size={20} className="text-oz2-acc" />}
          title="Sign-in approved"
          cy="peer-login-approved"
        >
          <p>
            Go back to the device: openZro connects on its own within a few
            seconds. You can close this page.
          </p>
          <PeerLoginDeviceCard approval={approval} />
        </Panel>
      );
    case "consumed":
      return (
        <Panel
          icon={<ShieldCheck size={20} className="text-oz2-acc" />}
          title="Device connected"
          cy="peer-login-consumed"
        >
          <p>
            This sign-in was approved and the device is connected. You can close
            this page.
          </p>
        </Panel>
      );
    case "denied":
      return (
        <Panel
          icon={<ShieldX size={20} className="text-oz2-err" />}
          title="Sign-in denied"
          cy="peer-login-denied"
        >
          <p>
            The device cannot connect with this attempt. If someone else has
            your password, change it and tell your administrator.
          </p>
          <PeerLoginDeviceCard approval={approval} />
        </Panel>
      );
    default:
      return <PeerLoginNotice kind="gone" />;
  }
}

export function PeerLoginNotice({
  kind,
}: Readonly<{ kind: PeerLoginNoticeKind }>) {
  const icon = <ShieldAlert size={20} className="text-oz2-text-muted" />;
  switch (kind) {
    case "gone":
      return (
        <Panel icon={icon} title="Request expired" cy="peer-login-expired">
          <p>
            This sign-in request is no longer open. To connect, start again from
            the openZro app on the device.
          </p>
        </Panel>
      );
    case "invalid":
      return (
        <Panel icon={icon} title="Link not valid" cy="peer-login-invalid">
          <p>
            This approval link is not valid or has expired. Start again from the
            openZro app on the device.
          </p>
        </Panel>
      );
    case "no_token":
      return (
        <Panel
          icon={icon}
          title="Open the link from openZro"
          cy="peer-login-no-token"
        >
          <p>
            This page needs the approval link the openZro app opened. If you
            reloaded it, open the link from the app again.
          </p>
        </Panel>
      );
    default:
      return (
        <Panel icon={icon} title="Something went wrong" cy="peer-login-error">
          <p>
            openZro could not be reached. Check your connection and reload the
            link from the app.
          </p>
        </Panel>
      );
  }
}
