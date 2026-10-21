"use client";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@components/DropdownMenu";
import { Modal } from "@components/modal/Modal";
import { notify } from "@components/Notification";
import { useApiCall } from "@utils/api";
import { isOpenzroHosted } from "@utils/openzro";
import {
  KeyRound,
  Loader2,
  MailIcon,
  MoreVertical,
  ShieldOff,
  Trash2,
  UserX,
} from "lucide-react";
import * as React from "react";
import { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import { useDialog } from "@/contexts/DialogProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { User } from "@/interfaces/User";
import { isLocalUser } from "@/modules/users/local/LocalUserBadge";
import { LocalUserPasswordModalContent } from "@/modules/users/local/LocalUserPasswordModal";
import useResetUserMFA from "@/modules/users/v2/useResetUserMFA";

// UserActionCellV2 — v2 row actions for /team/users. Matches the
// canonical kebab pattern used by NameserverActionCellV2 /
// SetupKeyActionCellV2 / etc.: one MoreVertical button opens a
// DropdownMenu with the available actions. Behavior unchanged from
// the earlier two-button layout — invited users on openzro-hosted
// still get "Resend invite", every row (except the logged-in user
// or when the operator lacks the perm) gets a danger-variant
// "Delete".

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
  const inviteRequest = useApiCall<User>("/users", true);
  const { mutate } = useSWRConfig();
  const [inviteLoading, setInviteLoading] = useState(false);
  const [resetOpen, setResetOpen] = useState(false);
  const resetMFA = useResetUserMFA(user, serviceUser);
  const local = isLocalUser(user);
  const localActive = user.local_credential_state === "active";

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
      description: local
        ? "Deleting this local user removes their password and dashboard access, and disconnects their devices, which stay listed with their access expired. The email can't be used for a new local user. This action cannot be undone."
        : "Deleting this user will remove their devices and remove dashboard access. This action cannot be undone.",
      confirmText: "Delete",
      cancelText: "Cancel",
      type: "danger",
    });
    if (!choice) return;
    deleteUser().then();
  };

  const deactivate = async () => {
    const name = user.name || user.email || "User";
    const choice = await confirm({
      title: `Deactivate '${name}'?`,
      description:
        "Deactivation is permanent: the password is removed, the user is blocked and their devices are disconnected with access expired. The account can't be reactivated and the email can't be used for a new local user.",
      confirmText: "Deactivate",
      cancelText: "Cancel",
      type: "danger",
    });
    if (!choice) return;
    notify({
      title: `'${name}' deactivated`,
      description: "The local user can no longer sign in.",
      promise: userRequest.post({}, `/${user.id}/deactivate`).then(() => {
        mutate(`/users?service_user=${serviceUser}`);
      }),
      loadingMessage: "Deactivating the user...",
    });
  };

  const sendInvite = () => {
    setInviteLoading(true);
    notify({
      title: "Resend Invite",
      description: `The invitation is being sent to ${user.email}`,
      promise: inviteRequest
        .post("", `/${user.id}/invite`)
        .finally(() => setInviteLoading(false)),
      loadingMessage: "Sending invitation...",
    });
  };

  const deleteDisabled = useMemo(() => {
    if (!permission.users.delete) return true;
    return Boolean(user.is_current);
  }, [permission.users.delete, user.is_current]);

  // Invitations only exist on hosted (mirrors legacy gating). Service
  // users never receive an invite, so the resend item is suppressed
  // for them too.
  const showResend =
    !serviceUser && isOpenzroHosted() && user.status === "invited";

  // Local-user actions follow the Users-module update permission the
  // server checks. The caller can't act on their own account here.
  const localDisabled = !permission.users.update || Boolean(user.is_current);

  return (
    <div className="flex justify-end pr-2" data-stop-row-click>
      {/* The modal renders in a portal, but React still bubbles its
          clicks through this tree; stop them before the row navigates. */}
      {local && (
        <div onClick={(e) => e.stopPropagation()}>
          <Modal open={resetOpen} onOpenChange={setResetOpen}>
            {resetOpen && (
              <LocalUserPasswordModalContent
                user={user}
                onSuccess={() => setResetOpen(false)}
              />
            )}
          </Modal>
        </div>
      )}
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger
          asChild
          onClick={(e) => {
            e.stopPropagation();
            e.preventDefault();
          }}
        >
          <button
            type="button"
            aria-label="Row actions"
            className="grid h-8 w-8 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 transition-colors hover:bg-oz2-hover hover:border-oz2-border-strong"
          >
            <MoreVertical size={14} className="shrink-0" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-44">
          {showResend && (
            <DropdownMenuItem
              disabled={!permission.users.create || inviteLoading}
              onClick={(e) => {
                e.stopPropagation();
                sendInvite();
              }}
            >
              <div className="flex items-center gap-3">
                {inviteLoading ? (
                  <Loader2 size={14} className="shrink-0 animate-spin" />
                ) : (
                  <MailIcon size={14} className="shrink-0" />
                )}
                {inviteLoading ? "Sending…" : "Resend invite"}
              </div>
            </DropdownMenuItem>
          )}
          {localActive && (
            <>
              <DropdownMenuItem
                disabled={localDisabled}
                data-cy="reset-local-password"
                onClick={(e) => {
                  e.stopPropagation();
                  setResetOpen(true);
                }}
              >
                <div className="flex items-center gap-3">
                  <KeyRound size={14} className="shrink-0" />
                  Reset password
                </div>
              </DropdownMenuItem>
              <DropdownMenuItem
                disabled={localDisabled}
                data-cy="deactivate-local-user"
                onClick={(e) => {
                  e.stopPropagation();
                  deactivate();
                }}
                variant="danger"
              >
                <div className="flex items-center gap-3">
                  <UserX size={14} className="shrink-0" />
                  Deactivate
                </div>
              </DropdownMenuItem>
            </>
          )}
          {user.mfa_enrolled && (
            <DropdownMenuItem
              // Users clear their own 2FA from Profile → Security.
              disabled={!permission.users.update || Boolean(user.is_current)}
              data-cy="reset-user-mfa"
              onClick={(e) => {
                e.stopPropagation();
                resetMFA();
              }}
            >
              <div className="flex items-center gap-3">
                <ShieldOff size={14} className="shrink-0" />
                Reset 2FA
              </div>
            </DropdownMenuItem>
          )}
          <DropdownMenuItem
            disabled={deleteDisabled}
            data-cy="delete-user"
            onClick={(e) => {
              e.stopPropagation();
              openConfirm();
            }}
            variant="danger"
          >
            <div className="flex items-center gap-3">
              <Trash2 size={14} className="shrink-0" />
              Delete
            </div>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
