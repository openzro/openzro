"use client";

import Button from "@components/Button";
import HelpText from "@components/HelpText";
import { notify } from "@components/Notification";
import Paragraph from "@components/Paragraph";
import * as Tabs from "@radix-ui/react-tabs";
import useFetchApi, { useApiCall } from "@utils/api";
import dayjs from "dayjs";
import {
  CheckCircle2Icon,
  CircleSlashIcon,
  PencilIcon,
  PlusCircleIcon,
  ShieldIcon,
  Trash2Icon,
} from "lucide-react";
import React, { useState } from "react";
import { useSWRConfig } from "swr";
import {
  AuthenticationProvider,
  AuthenticationProviderType,
  providerTypeMeta,
} from "@/interfaces/AuthenticationProvider";
import AuthenticationProviderModal from "@/modules/auth-providers/AuthenticationProviderModal";

// AuthenticationProvidersTab is the body of the Settings →
// Authentication Providers tab. Lists configured IdPs, lets the
// admin add / edit / delete them. Backed by /admin/auth-providers
// (PR 6 of ADR-0005). The /login surface (PR 5) consumes the
// same rows the operator manages here.
export default function AuthenticationProvidersTab() {
  const { data, isLoading } = useFetchApi<AuthenticationProvider[]>(
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

  return (
    <Tabs.Content value="auth-providers" className={"p-default py-6 max-w-5xl"}>
      <h1>Authentication Providers</h1>
      <Paragraph>
        Configure the OIDC identity providers users sign in through.
        Each provider you add here shows up as a button on the
        openZro-branded /login page.
      </Paragraph>
      <HelpText>
        Credentials are encrypted at rest with the management&apos;s
        DataStoreEncryptionKey. The dashboard never reads client
        secrets back — edit a provider and re-enter the value to
        rotate it. See{" "}
        <code className={"font-mono text-xs"}>docs/adr/0005-centralized-login.md</code>{" "}
        for the full design.
      </HelpText>

      <div className="mt-6 flex justify-end">
        <Button variant="primary" onClick={openCreate}>
          <PlusCircleIcon size={16} /> Add provider
        </Button>
      </div>

      <div className="mt-4">
        {isLoading && (
          <Paragraph className="text-nb-gray-300">Loading…</Paragraph>
        )}
        {!isLoading && (!data || data.length === 0) && (
          <EmptyState
            message={
              "No authentication providers configured yet. Click Add provider to connect Zitadel, Keycloak, Entra ID, Okta, Authentik, or any OIDC-compliant IdP."
            }
          />
        )}
        {data && data.length > 0 && (
          <table className="w-full text-sm">
            <TableHead
              cols={["Type", "Name", "Issuer", "Status", "Updated", ""]}
            />
            <tbody>
              {data.map((row) => (
                <ProviderRow
                  key={row.id}
                  row={row}
                  onEdit={() => openEdit(row)}
                />
              ))}
            </tbody>
          </table>
        )}
      </div>

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
}: Readonly<{
  row: AuthenticationProvider;
  onEdit: () => void;
}>) {
  const { mutate } = useSWRConfig();
  const api = useApiCall(`/admin/auth-providers/${row.id}`);

  const onDelete = async () => {
    if (
      !confirm(
        `Delete "${row.name}"? Users signed in through this provider will lose access on their next session refresh.`,
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

  return (
    <tr className="border-t border-nb-gray-900">
      <td className="py-3">
        <ProviderTypeBadge type={row.type} />
      </td>
      <td className="py-3">{row.name}</td>
      <td className="py-3 font-mono text-xs text-nb-gray-300 max-w-xs truncate">
        {row.config?.issuer_url ?? ""}
      </td>
      <td className="py-3">
        <EnabledStatus enabled={row.enabled} />
      </td>
      <td className="py-3 text-xs text-nb-gray-300">
        {dayjs(row.updated_at).fromNow()}
      </td>
      <td className="py-3 text-right">
        <RowActions onEdit={onEdit} onDelete={onDelete} />
      </td>
    </tr>
  );
}

function ProviderTypeBadge({ type }: Readonly<{ type: AuthenticationProviderType }>) {
  return (
    <span className="inline-flex items-center gap-1 rounded bg-nb-gray-900 px-2 py-1 text-xs text-violet-300">
      <ShieldIcon size={12} /> {providerTypeMeta(type).label}
    </span>
  );
}

function EnabledStatus({ enabled }: Readonly<{ enabled: boolean }>) {
  if (enabled) {
    return (
      <span className="inline-flex items-center gap-1 text-xs text-green-400">
        <CheckCircle2Icon size={12} /> Enabled
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 text-xs text-nb-gray-300">
      <CircleSlashIcon size={12} /> Disabled
    </span>
  );
}

function TableHead({ cols }: Readonly<{ cols: string[] }>) {
  return (
    <thead>
      <tr className="text-left text-xs text-nb-gray-300 uppercase tracking-wider">
        {cols.map((c, i) => (
          <th key={i} className="pb-2 font-medium">
            {c}
          </th>
        ))}
      </tr>
    </thead>
  );
}

function EmptyState({ message }: Readonly<{ message: string }>) {
  return (
    <div className="rounded-md border border-dashed border-nb-gray-800 bg-nb-gray-940 p-8 text-center">
      <Paragraph className="text-nb-gray-300">{message}</Paragraph>
    </div>
  );
}

function RowActions({
  onEdit,
  onDelete,
}: Readonly<{
  onEdit: () => void;
  onDelete: () => void;
}>) {
  return (
    <div className="inline-flex gap-2">
      <button
        type="button"
        onClick={onEdit}
        className="rounded p-1.5 text-nb-gray-300 hover:bg-nb-gray-900 hover:text-white"
        title="Edit"
      >
        <PencilIcon size={14} />
      </button>
      <button
        type="button"
        onClick={onDelete}
        className="rounded p-1.5 text-red-400 hover:bg-red-500/10 hover:text-red-300"
        title="Delete"
      >
        <Trash2Icon size={14} />
      </button>
    </div>
  );
}
