"use client";

import { notify } from "@components/Notification";
import * as Tabs from "@radix-ui/react-tabs";
import { useApiCall } from "@utils/api";
import { Trash2 } from "lucide-react";
import React from "react";
import { useDialog } from "@/contexts/DialogProvider";
import { useLoggedInUser } from "@/contexts/UsersProvider";
import { Account } from "@/interfaces/Account";
import OzSettingsCard from "@/modules/settings/v2/OzSettingsCard";

// DangerZoneTab — settings sub-page body for /settings/danger-zone.
// Functionality preserved verbatim: confirm dialog → DELETE
// /accounts/{id} → clear browser storage → logout. Only paint changes
// — the legacy hand-rolled red card becomes a danger-variant
// OzSettingsCard containing one DangerRow.

type Props = {
  account: Account;
};

export default function DangerZoneTab({ account }: Readonly<Props>) {
  const { confirm } = useDialog();
  const deleteRequest = useApiCall<Account>("/accounts/" + account.id);
  const { logout } = useLoggedInUser();

  const deleteAccount = async () => {
    const deletePromise = new Promise<void>((resolve, reject) => {
      return deleteRequest
        .del()
        .catch((error) => reject(error))
        .then(() => {
          if (typeof window !== "undefined") {
            localStorage.clear();
            sessionStorage.clear();
          }
          logout().then();
          resolve();
        });
    });

    notify({
      title: "Delete openZro account",
      description: "openZro account was successfully deleted.",
      promise: deletePromise,
      loadingMessage: "Deleting the account...",
    });
  };

  const handleConfirm = async () => {
    const choice = await confirm({
      title: "Delete openZro account",
      description:
        "Are you sure you want to delete your openZro account? This action cannot be undone.",
      confirmText: "Delete",
      cancelText: "Cancel",
      type: "danger",
    });
    if (!choice) return;
    deleteAccount().then();
  };

  return (
    <Tabs.Content value="danger-zone" className="flex flex-col gap-5">
      <header>
        <h2 className="text-[18px] font-semibold tracking-tight text-oz2-err">
          Danger Zone
        </h2>
        <p className="mt-1 max-w-2xl text-[13px] leading-[1.55] text-oz2-text-muted">
          Irreversible operations. Read the description twice and only proceed
          if you know exactly what you&apos;re doing.
        </p>
      </header>

      <OzSettingsCard
        title="Destructive operations"
        sub="These actions cannot be undone. Backups, exports, and audit history will be removed alongside the data they describe."
        danger
      >
        <DangerRow
          title="Delete openZro account"
          desc="Permanently delete your openZro account, all associated peers, users, groups, policies, and routes. You will be signed out immediately."
          ctaLabel="Delete Account"
          onClick={handleConfirm}
        />
      </OzSettingsCard>
    </Tabs.Content>
  );
}

// DangerRow — inline within DangerZoneTab. Splits a destructive
// operation into a title + description on the left and a danger
// button on the right. Matches the handoff's `DangerRow` (screens-5.jsx,
// SettingsGeneral) shape; only used here for now, so it stays
// co-located.
function DangerRow({
  title,
  desc,
  ctaLabel,
  onClick,
}: {
  title: string;
  desc: string;
  ctaLabel: string;
  onClick: () => void;
}) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0 flex-1">
        <div className="text-[13.5px] font-semibold text-oz2-err">{title}</div>
        <p className="mt-[2px] text-[12.5px] leading-[1.5] text-oz2-text-muted">
          {desc}
        </p>
      </div>
      <button
        type="button"
        onClick={onClick}
        className="inline-flex h-[34px] shrink-0 items-center gap-2 whitespace-nowrap rounded-oz2-input border border-oz2-err bg-transparent px-3.5 text-[13px] font-medium text-oz2-err transition-colors hover:bg-oz2-err hover:text-oz2-text-on-acc"
      >
        <Trash2 size={13} />
        {ctaLabel}
      </button>
    </div>
  );
}
