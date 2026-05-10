"use client";

import { notify } from "@components/Notification";
import * as Tabs from "@radix-ui/react-tabs";
import useFetchApi, { useApiCall } from "@utils/api";
import {
  AlertTriangle,
  PencilLine,
  PlusCircle,
  Trash2,
} from "lucide-react";
import React, { useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import {
  AuthenticationProvider,
  CONNECTOR_TYPES,
  ConnectorType,
  inferConnectorType,
} from "@/interfaces/AuthenticationProvider";
import AuthenticationProviderModal from "@/modules/auth-providers/AuthenticationProviderModal";
import OzSettingsCard from "@/modules/settings/v2/OzSettingsCard";

// AuthenticationProvidersTab — settings sub-page body for
// /settings/auth-providers. Functionality preserved verbatim: lists
// Dex connectors via /admin/auth-providers, opens a shared modal for
// create/edit, deletes through the same endpoint. Visual paint matches
// the handoff's SettingsAuth "Identity providers" card: title + sub +
// Add CTA in the header, then a stack of provider rows inside the same
// card (avatar / name / type+id / Configure + Delete).

export default function AuthenticationProvidersTab() {
  const { data, isLoading, error } = useFetchApi<AuthenticationProvider[]>(
    "/admin/auth-providers",
  );
  const [editing, setEditing] = useState<AuthenticationProvider | null>(null);
  const [modalOpen, setModalOpen] = useState(false);

  const openCreate = () => {
    setEditing(null);
    setModalOpen(true);
  };
  const openEdit = (row: AuthenticationProvider) => {
    setEditing(row);
    setModalOpen(true);
  };

  const dexUnreachable =
    error && (error as { status?: number }).status === 503;

  return (
    <Tabs.Content value="auth-providers" className="flex flex-col gap-5">
      <header>
        <h2 className="text-[18px] font-semibold tracking-tight text-oz2-text">
          Authentication Providers
        </h2>
        <p className="mt-1 max-w-3xl text-[13px] leading-[1.55] text-oz2-text-muted">
          Configure the upstream identity providers your users sign in
          through. Each provider you add here shows up as a button on the
          openZro-branded sign-in page. Connectors live in Dex&apos;s storage
          — adding one takes effect immediately, no restart, no file edit.
        </p>
      </header>

      {dexUnreachable && (
        <div className="flex items-start gap-3 rounded-oz2-card border border-oz2-warn/40 bg-oz2-warn-bg/40 px-4 py-3 text-[12.5px] text-oz2-warn">
          <span aria-hidden className="mt-0.5 shrink-0">
            <AlertTriangle size={14} />
          </span>
          <p className="leading-[1.5]">
            Dex isn&apos;t reachable from the management server. Check that
            the dex container is running and the{" "}
            <code className="font-mono text-[11.5px]">
              OPENZRO_DEX_GRPC_*
            </code>{" "}
            env vars are set on management.
          </p>
        </div>
      )}

      <OzSettingsCard
        title="Identity providers"
        sub="Users sign in via an external IdP. Multiple providers can be active simultaneously — every connector you add here becomes a sign-in button on the openZro auth page."
        right={
          <OzButton variant="primary" type="button" onClick={openCreate}>
            <PlusCircle size={14} />
            Add provider
          </OzButton>
        }
      >
        {isLoading ? (
          <p className="px-1 py-2 text-[12.5px] text-oz2-text-muted">
            Loading…
          </p>
        ) : !data || data.length === 0 ? (
          <div className="rounded-oz2-card border border-dashed border-oz2-border bg-oz2-bg-sunken/40 px-6 py-8 text-center text-[12.5px] text-oz2-text-muted">
            No authentication providers configured yet. Click{" "}
            <strong className="font-semibold text-oz2-text">Add provider</strong>{" "}
            to connect Google, GitHub, Microsoft Entra, or any OIDC-compliant
            IdP.
          </div>
        ) : (
          <ul className="-mx-1 flex flex-col">
            {data.map((row, i) => (
              <ProviderRow
                key={row.id}
                row={row}
                onEdit={() => openEdit(row)}
                isFirst={i === 0}
              />
            ))}
          </ul>
        )}
      </OzSettingsCard>

      <AuthenticationProviderModal
        open={modalOpen}
        setOpen={setModalOpen}
        existing={editing}
      />
    </Tabs.Content>
  );
}

function ProviderRow({
  row,
  onEdit,
  isFirst,
}: Readonly<{
  row: AuthenticationProvider;
  onEdit: () => void;
  isFirst: boolean;
}>) {
  const { mutate } = useSWRConfig();
  const api = useApiCall(`/admin/auth-providers/${row.id}`);

  const onDelete = async () => {
    if (
      !confirm(
        `Delete "${row.name}"? Users that signed in through this provider lose access on their next session refresh.`,
      )
    ) {
      return;
    }
    try {
      await api.del();
      await mutate("/admin/auth-providers");
      notify({ title: "Provider deleted", description: row.name });
    } catch {
      // useApiCall surfaces the error via its own toast.
    }
  };

  const visualType = inferConnectorType(
    row.type,
    row.config,
  ) as ConnectorType;
  const meta = CONNECTOR_TYPES.find((c) => c.value === visualType);
  const typeLabel = meta?.label ?? row.type;
  const initial = typeLabel.slice(0, 1).toUpperCase();

  return (
    <li
      className={
        "flex flex-wrap items-center gap-3 px-1 py-3 " +
        (isFirst ? "" : "border-t border-oz2-border-soft")
      }
    >
      <div
        aria-hidden
        className="grid h-9 w-9 shrink-0 place-items-center rounded-[8px] border border-oz2-border-soft bg-oz2-bg-sunken font-mono text-[14px] font-semibold text-oz2-text-2"
      >
        {initial}
      </div>
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-[13.5px] font-semibold text-oz2-text">
          {row.name}
        </span>
        <span className="mt-[2px] text-[12px] text-oz2-text-muted">
          <span className="font-medium text-oz2-text-2">{typeLabel}</span>
          <span className="px-1.5 text-oz2-text-faint">·</span>
          <span className="font-mono text-[11.5px] text-oz2-text-faint">
            {row.id}
          </span>
        </span>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <OzButton
          variant="default"
          type="button"
          onClick={onEdit}
          className="!h-[30px] !px-3 !text-[12.5px]"
        >
          <PencilLine size={12} />
          Configure
        </OzButton>
        <button
          type="button"
          onClick={onDelete}
          aria-label={`Delete ${row.name}`}
          className="grid h-[30px] w-[30px] place-items-center rounded-oz2-input border border-oz2-border bg-transparent text-oz2-err transition-colors hover:border-oz2-err hover:bg-oz2-err-bg"
        >
          <Trash2 size={13} />
        </button>
      </div>
    </li>
  );
}
