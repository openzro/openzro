"use client";

import { notify } from "@components/Notification";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import FullScreenLoading from "@components/ui/FullScreenLoading";
import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import useRedirect from "@hooks/useRedirect";
import useFetchApi, { useApiCall } from "@utils/api";
import { generateColorFromString } from "@utils/helpers";
import dayjs from "dayjs";
import {
  Ban,
  Clock,
  Cog,
  GalleryHorizontalEnd,
  History,
  Mail,
  PlusCircle,
  User2,
} from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { useLoggedInUser } from "@/contexts/UsersProvider";
import { useHasChanges } from "@/hooks/useHasChanges";
import { Group } from "@/interfaces/Group";
import { Role, User } from "@/interfaces/User";
import AccessTokensTable from "@/modules/access-tokens/AccessTokensTable";
import CreateAccessTokenModal from "@/modules/access-tokens/CreateAccessTokenModal";
import useGroupHelper from "@/modules/groups/useGroupHelper";
import { useGroupIdsToGroups } from "@/modules/groups/useGroupIdsToGroups";
import UserBlockCell from "@/modules/users/table-cells/UserBlockCell";
import { UserRoleSelector } from "@/modules/users/UserRoleSelector";
import UserStatusCellV2 from "@/modules/users/v2/UserStatusCellV2";

// /team/user — v2 paint over the legacy User detail page. Chrome
// (OzShell + sidebar + topbar with breadcrumbs) lives in
// (v2-dashboard)/layout.tsx; this page renders the body only. Page
// header (avatar + name + Cancel/Save buttons) sits inline near the
// top because Save's disabled state is state-dependent and the topbar
// slot captures children once on mount. Form widgets (PeerGroupSelector
// + UserRoleSelector + UserBlockCell) remain on legacy paint pending a
// Phase 5 cleanup — wrapping them in OzCards keeps the surface uniform
// in the meantime.

export default function UserPage() {
  const queryParameter = useSearchParams();
  const userId = queryParameter.get("id");
  const { permission } = usePermissions();
  const isServiceUser = queryParameter.get("service_user") === "true";
  const { data: users, isLoading } = useFetchApi<User[]>(
    `/users?service_user=${isServiceUser}`,
  );
  const { isOwnerOrAdmin } = useLoggedInUser();

  const user = useMemo(() => {
    return users?.find((u) => u.id === userId);
  }, [users, userId]);

  useRedirect("/team/users", false, !userId);

  const userGroups = useGroupIdsToGroups(user?.auto_groups);

  if (!permission.users.read) {
    return (
      <div className="space-y-6 p-8">
        <RestrictedAccess page={"User Information"} />
      </div>
    );
  }

  if (!isOwnerOrAdmin && user && !isLoading) {
    return <UserOverview user={user} initialGroups={[]} />;
  }

  if (isOwnerOrAdmin && user && !isLoading && userGroups) {
    return <UserOverview user={user} initialGroups={userGroups} />;
  }

  return <FullScreenLoading />;
}

type Props = {
  user: User;
  initialGroups: Group[];
};

function UserOverview({ user, initialGroups }: Readonly<Props>) {
  const router = useRouter();
  const userRequest = useApiCall<User>("/users");
  const { mutate } = useSWRConfig();
  const { loggedInUser, isOwnerOrAdmin, isUser } = useLoggedInUser();
  const isLoggedInUser = loggedInUser ? loggedInUser?.id === user.id : false;
  const { permission } = usePermissions();

  const [selectedGroups, setSelectedGroups, { save: saveGroups }] =
    useGroupHelper({
      initial: initialGroups,
    });

  const [role, setRole] = useState(user.role || Role.User);

  const { hasChanges, updateRef: updateChangesRef } = useHasChanges([
    role,
    selectedGroups,
  ]);

  const save = async () => {
    const groups = await saveGroups();
    const groupIds = groups.map((group) => group.id) as string[];

    notify({
      title: user.name,
      description: "Changes successfully saved.",
      promise: userRequest
        .put(
          {
            role: role,
            auto_groups: groupIds,
            is_blocked: user.is_blocked,
          },
          `/${user.id}`,
        )
        .then(() => {
          mutate(`/users?service_user=${user.is_service_user}`);
          updateChangesRef([role, selectedGroups]);
        }),
      loadingMessage: "Saving changes...",
    });
  };

  const cancel = () => {
    user.is_service_user
      ? router.push("/team/service-users")
      : router.push("/team/users");
  };

  const showAccessTokens =
    (user.is_current || user.is_service_user) && permission.pats.read;

  return (
    <div className="space-y-6 p-8">
      {/* Page header — avatar + name on the left, Cancel/Save on the
          right. Save is gated on hasChanges + update permission. */}
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex items-center gap-3">
          <UserAvatar user={user} />
          <h1 className="text-[24px] font-semibold tracking-tight">
            {user.name || user.id}
          </h1>
        </div>
        {!isUser && (
          <div className="flex items-center gap-2">
            <OzButton variant="default" type="button" onClick={cancel}>
              Cancel
            </OzButton>
            <OzButton
              variant="primary"
              type="button"
              onClick={save}
              disabled={!hasChanges || !permission.users.update}
              data-cy="save-changes"
            >
              Save Changes
            </OzButton>
          </div>
        )}
      </header>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <UserInformationCard user={user} />

        <div className="space-y-6">
          {!user.is_service_user && isOwnerOrAdmin && (
            <OzCard className="space-y-3">
              <div>
                <label className="text-[13px] font-semibold text-oz2-text">
                  Auto-assigned groups
                </label>
                <p className="mt-1 text-[12.5px] text-oz2-text-muted">
                  Groups will be assigned to peers added by this user.
                </p>
              </div>
              <PeerGroupSelector
                disabled={isUser}
                onChange={setSelectedGroups}
                values={selectedGroups}
                hideAllGroup={true}
                dataCy="user-group-selector"
              />
            </OzCard>
          )}

          <OzCard className="space-y-3">
            <div>
              <label className="text-[13px] font-semibold text-oz2-text">
                User Role
              </label>
              <p className="mt-1 text-[12.5px] text-oz2-text-muted">
                Set a role for the user to assign access permissions.
              </p>
            </div>
            <UserRoleSelector
              value={role}
              onChange={setRole}
              hideOwner={user.is_service_user}
              currentUser={user}
              disabled={isLoggedInUser || !permission.users.update}
            />
          </OzCard>
        </div>
      </div>

      {showAccessTokens && (
        <section className="space-y-4">
          <div className="flex flex-wrap items-end justify-between gap-3">
            <div>
              <h2 className="text-[18px] font-semibold tracking-tight">
                Access Tokens
              </h2>
              <p className="mt-1 max-w-2xl text-[13.5px] text-oz2-text-muted">
                Access tokens give programmatic access to the openZro API. Keep
                them secret — anyone with a token can act on this user&apos;s
                behalf.
              </p>
            </div>
            <CreateAccessTokenModal user={user}>
              <OzButton
                variant="primary"
                type="button"
                disabled={!permission.pats.create}
                data-cy="access-token-open-modal"
              >
                <PlusCircle size={14} />
                Create Access Token
              </OzButton>
            </CreateAccessTokenModal>
          </div>
          <AccessTokensTable user={user} />
        </section>
      )}
    </div>
  );
}

// UserAvatar — 40×40 v2-paint avatar mirroring the bubble in
// UserNameCellV2 but a touch larger to match the H1. Service users get
// the neutral surface; humans get the gradient with status overlay.
function UserAvatar({ user }: { user: User }) {
  const isService = Boolean(user.is_service_user);
  const status = user.status;

  if (isService) {
    return (
      <div
        className="relative grid h-10 w-10 place-items-center rounded-full border border-oz2-border bg-oz2-bg-sunken text-oz2-text-2"
        style={{ color: "white" }}
      >
        <Cog size={16} />
      </div>
    );
  }

  return (
    <div
      className="relative grid h-10 w-10 place-items-center rounded-full text-[13px] font-semibold uppercase text-white"
      style={{ background: "linear-gradient(135deg,#a78bfa,#f472b6)" }}
    >
      {initialsFor(user) || (
        <span
          style={{
            color: user?.name
              ? generateColorFromString(user?.name || user?.id || "System User")
              : "#808080",
          }}
        >
          {user?.name?.charAt(0) || user?.id?.charAt(0)}
        </span>
      )}
      {(status === "invited" || status === "blocked") && (
        <span
          aria-hidden
          className={
            "absolute -bottom-0.5 -right-0.5 grid h-4 w-4 place-items-center rounded-full border-2 border-oz2-surface " +
            (status === "invited"
              ? "bg-oz2-warn text-oz2-text-on-acc"
              : "bg-oz2-err text-oz2-text-on-acc")
          }
        >
          {status === "invited" ? <Clock size={10} /> : <Ban size={10} />}
        </span>
      )}
    </div>
  );
}

function initialsFor(user: User): string {
  const name = user.name?.trim();
  if (name) {
    const parts = name.split(/\s+/);
    if (parts.length >= 2) {
      return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
    }
    return name.slice(0, 2).toUpperCase();
  }
  if (user.email) return user.email.slice(0, 2).toUpperCase();
  if (user.id) return user.id.slice(0, 2).toUpperCase();
  return "";
}

function UserInformationCard({ user }: Readonly<{ user: User }>) {
  const isServiceUser = user.is_service_user || false;
  const neverLoggedIn = dayjs(user.last_login).isBefore(
    dayjs().subtract(1000, "years"),
  );

  const rows: { label: React.ReactNode; value: React.ReactNode }[] = [
    {
      label: (
        <>
          <User2 size={14} />
          {user.name ? "Name" : "User ID"}
        </>
      ),
      value: <span className="text-oz2-text">{user.name || user.id}</span>,
    },
  ];

  if (!isServiceUser) {
    rows.push({
      label: (
        <>
          <Mail size={14} />
          E-Mail
        </>
      ),
      value: (
        <span className="text-oz2-text">{user.email || "—"}</span>
      ),
    });
  }

  rows.push({
    label: (
      <>
        <GalleryHorizontalEnd size={14} />
        Status
      </>
    ),
    value: <UserStatusCellV2 user={user} />,
  });

  if (!isServiceUser) {
    if (!user.is_current && user.role !== Role.Owner) {
      rows.push({
        label: (
          <>
            <Ban size={14} />
            Block User
          </>
        ),
        value: <UserBlockCell user={user} isUserPage={true} />,
      });
    }
    rows.push({
      label: (
        <>
          <History size={14} />
          Last login
        </>
      ),
      value: (
        <span className="text-oz2-text">
          {neverLoggedIn
            ? "Never"
            : dayjs(user.last_login).format("D MMMM, YYYY [at] h:mm A") +
              " (" +
              dayjs().to(user.last_login) +
              ")"}
        </span>
      ),
    });
  }

  return (
    <OzCard flush>
      <ul className="divide-y divide-oz2-border-soft">
        {rows.map((row, i) => (
          <li
            key={i}
            className="flex flex-wrap items-center justify-between gap-3 px-[18px] py-3.5 text-[13.5px]"
          >
            <span className="inline-flex items-center gap-2 text-oz2-text-muted">
              {row.label}
            </span>
            <span className="text-right">{row.value}</span>
          </li>
        ))}
      </ul>
    </OzCard>
  );
}
