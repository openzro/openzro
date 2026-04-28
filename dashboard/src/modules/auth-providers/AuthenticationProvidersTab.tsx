"use client";

import Button from "@components/Button";
import HelpText from "@components/HelpText";
import { notify } from "@components/Notification";
import Paragraph from "@components/Paragraph";
import * as Tabs from "@radix-ui/react-tabs";
import useFetchApi, { useApiCall } from "@utils/api";
import {
  PencilIcon,
  PlusCircleIcon,
  ShieldIcon,
  Trash2Icon,
} from "lucide-react";
import React, { useState } from "react";
import { useSWRConfig } from "swr";
import {
  AuthenticationProvider,
  CONNECTOR_TYPES,
  ConnectorType,
} from "@/interfaces/AuthenticationProvider";
import AuthenticationProviderModal from "@/modules/auth-providers/AuthenticationProviderModal";

// Settings → Authentication Providers tab. Lists every Dex
// connector (rows in Dex's storage backend, fetched via the
// management's /api/admin/auth-providers proxy). Operators add /
// edit / delete connectors here and the change is visible at
// /dex/auth on the next page load — no Dex restart, no YAML edit.
//
// See ADR-0006.
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

  return (
    <Tabs.Content value="auth-providers" className={"p-default py-6 max-w-5xl"}>
      <h1>Authentication Providers</h1>
      <Paragraph>
        Configure the upstream identity providers your users sign in
        through. Each provider you add here shows up as a button on
        the openZro-branded /dex/auth page.
      </Paragraph>
      <HelpText>
        Connectors live in Dex&apos;s storage; the dashboard talks
        to Dex over a private gRPC channel via the management. Adding
        a provider takes effect immediately — no restart, no file
        edit. See <code className={"font-mono text-xs"}>docs/adr/0006-embed-dex.md</code>{" "}
        for the architecture.
      </HelpText>

      {error && (error as { status?: number }).status === 503 && (
        <div className="mt-6 rounded-md border border-orange-700/50 bg-orange-500/5 px-4 py-3 text-sm text-orange-200">
          Dex isn&apos;t reachable from the management server. Check
          that the dex container is running and the OPENZRO_DEX_GRPC_*
          env vars are set on management.
        </div>
      )}

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
              "No authentication providers configured yet. Click Add provider to connect Google, GitHub, Microsoft Entra, or any OIDC-compliant IdP."
            }
          />
        )}
        {data && data.length > 0 && (
          <table className="w-full text-sm">
            <TableHead cols={["Type", "ID", "Name", ""]} />
            <tbody>
              {data.map((row) => (
                <ProviderRow key={row.id} row={row} onEdit={() => openEdit(row)} />
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

  return (
    <tr className="border-t border-nb-gray-900">
      <td className="py-3">
        <ProviderTypeBadge type={row.type as ConnectorType | string} />
      </td>
      <td className="py-3 font-mono text-xs text-nb-gray-300">{row.id}</td>
      <td className="py-3">{row.name}</td>
      <td className="py-3 text-right">
        <RowActions onEdit={onEdit} onDelete={onDelete} />
      </td>
    </tr>
  );
}

function ProviderTypeBadge({
  type,
}: Readonly<{ type: ConnectorType | string }>) {
  const meta = CONNECTOR_TYPES.find((c) => c.value === type);
  return (
    <span className="inline-flex items-center gap-1 rounded bg-nb-gray-900 px-2 py-1 text-xs text-violet-300">
      <ShieldIcon size={12} /> {meta?.label ?? type}
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

