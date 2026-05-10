import { notify } from "@components/Notification";
import { useApiCall } from "@utils/api";
import { isOpenzroHosted } from "@utils/openzro";
import { Loader2, MailIcon, Trash2 } from "lucide-react";
import * as React from "react";
import { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import { useDialog } from "@/contexts/DialogProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { User } from "@/interfaces/User";

// UserActionCellV2 — v2 paint for the row-end actions on
// /team/users. Behavior mirrors the legacy UserActionCell + the
// inlined UserResendInviteButton: invited users on openzro-hosted
// get a Resend Invite button, all rows (except the logged-in user
// or when the operator lacks the perm) get a danger-outline Delete
// button with the existing confirm dialog flow.
//
// Visual pass: 28px-tall row buttons that respect the v2 tokens —
// neutral outline for Resend, danger-outline using oz2-err for
// Delete. Same data-cy hooks the legacy renderer used so existing
// E2E specs keep matching.

type Props = {
  user: User;
  serviceUser?: boolean;
};

export default function UserActionCellV2({
  user,
  serviceUser = false,
}: Readonly<Props>) {
  const { confirm } = useDialog();
  const { permission } = usePermissions();
  const userRequest = useApiCall<User>("/users");
  const { mutate } = useSWRConfig();

  const deleteUser = async () => {
    const name = user.name || "User";
    notify({
      title: `'${name}' deleted`,
      description: "User was successfully deleted.",
      promise: userRequest.del("", `/${user.id}`).then(() => {
        mutate(`/users?service_user=${serviceUser}`);
      }),
      loadingMessage: "Deleting the user...",
    });
  };

  const openConfirm = async () => {
    const name = user.name || "User";
    const choice = await confirm({
      title: `Delete '${name}'?`,
      description:
        "Deleting this user will remove their devices and remove dashboard access. This action cannot be undone.",
      confirmText: "Delete",
      cancelText: "Cancel",
      type: "danger",
    });
    if (!choice) return;
    deleteUser().then();
  };

  const deleteDisabled = useMemo(() => {
    if (!permission.users.delete) return true;
    return Boolean(user.is_current);
  }, [permission.users.delete, user.is_current]);

  // Invitations only exist on hosted (mirrors legacy gating). Service
  // users never receive an invite, so the resend button is suppressed
  // for them too.
  const showResend =
    !serviceUser && isOpenzroHosted() && user.status === "invited";

  return (
    <div
      className="flex items-center justify-end gap-2 pr-3"
      data-stop-row-click
    >
      {showResend && <ResendInviteButton user={user} />}
      <button
        type="button"
        onClick={openConfirm}
        disabled={deleteDisabled}
        data-cy="delete-user"
        aria-label={`Delete ${user.name || "user"}`}
        className={
          // Row-scale danger button. At rest: neutral border + red
          // text reads quietly inside a busy row; on hover it flips
          // to a soft red fill (oz2-err-bg) with a red border so the
          // destructive intent is obvious before the click. Tokens
          // only — no alpha modifiers since --ozv2-err is a flat hex.
          "inline-flex h-7 items-center gap-1.5 whitespace-nowrap rounded-[8px] border border-oz2-border bg-transparent px-2.5 text-[12.5px] font-medium text-oz2-err transition-colors hover:border-oz2-err hover:bg-oz2-err-bg disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:border-oz2-border disabled:hover:bg-transparent"
        }
      >
        <Trash2 size={13} />
        Delete
      </button>
    </div>
  );
}

// Resend invite — invited users on openzro-hosted only. Mirrors the
// legacy UserResendInviteButton's loading state + permission gate
// but renders a v2 row-button (28px, neutral outline).
function ResendInviteButton({ user }: { user: User }) {
  const userRequest = useApiCall<User>("/users", true);
  const [isLoading, setIsLoading] = useState(false);
  const { permission } = usePermissions();

  const inviteUser = () => {
    setIsLoading(true);
    notify({
      title: "Resend Invite",
      description: `The invitation is being sent to ${user.email}`,
      promise: userRequest
        .post("", `/${user.id}/invite`)
        .finally(() => setIsLoading(false)),
      loadingMessage: "Sending invitation...",
    });
  };

  return (
    <button
      type="button"
      onClick={inviteUser}
      disabled={!permission.users.create || isLoading}
      className={
        "inline-flex h-7 items-center gap-1.5 whitespace-nowrap rounded-[8px] border border-oz2-border bg-oz2-surface px-2.5 text-[12.5px] font-medium text-oz2-text-2 transition-colors hover:bg-oz2-hover hover:border-oz2-border-strong disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-oz2-surface" +
        (isLoading ? " animate-pulse" : "")
      }
    >
      {isLoading ? (
        <Loader2 size={12} className="block animate-spin" />
      ) : (
        <MailIcon size={12} />
      )}
      {isLoading ? "Sending…" : "Resend invite"}
    </button>
  );
}
