"use client";

import InlineLink from "@components/InlineLink";
import { notify } from "@components/Notification";
import * as Tabs from "@radix-ui/react-tabs";
import { useApiCall } from "@utils/api";
import { ExternalLinkIcon, FlaskConical } from "lucide-react";
import React, { useState } from "react";
import { useSWRConfig } from "swr";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Account } from "@/interfaces/Account";
import ClientUpdateSettingsCard from "@/modules/settings/ClientUpdateSettingsCard";
import OzSettingsCard from "@/modules/settings/v2/OzSettingsCard";
import OzSettingsToggle from "@/modules/settings/v2/OzSettingsToggle";

// ClientSettingsTab — settings sub-page body for /settings/clients.
// Functionality preserved verbatim: lazy_connection_enabled toggled
// directly (no Save button, the change applies immediately through
// /accounts/{id}). Only paint changes — the toggle moves into an
// OzSettingsCard whose title carries the "experimental" badge so the
// caveat reads at a glance without a separate H2 above.

type Props = {
  account: Account;
};

export default function ClientSettingsTab({ account }: Readonly<Props>) {
  const { permission } = usePermissions();
  const { mutate } = useSWRConfig();
  const saveRequest = useApiCall<Account>("/accounts/" + account.id, true);

  const [lazyConnection, setLazyConnection] = useState(
    account.settings?.lazy_connection_enabled ?? false,
  );

  const toggleLazyConnection = async (toggle: boolean) => {
    notify({
      title: "Lazy Connections",
      description: `Lazy Connections successfully ${
        toggle ? "enabled" : "disabled"
      }.`,
      promise: saveRequest
        .put({
          id: account.id,
          settings: {
            ...account.settings,
            lazy_connection_enabled: toggle,
          },
        })
        .then(() => {
          setLazyConnection(toggle);
          mutate("/accounts");
        }),
      loadingMessage: "Updating Lazy Connections setting...",
    });
  };

  return (
    <Tabs.Content value="clients" className="flex flex-col gap-5">
      <header>
        <h2 className="text-[18px] font-semibold tracking-tight text-oz2-text">
          Clients
        </h2>
        <p className="mt-1 max-w-2xl text-[13px] leading-[1.55] text-oz2-text-muted">
          Behavior of the openZro client running on every peer. Defaults here
          flow to the next time a client reconnects to management.
        </p>
      </header>

      <ClientUpdateSettingsCard account={account} />

      <OzSettingsCard
        title={
          <span className="inline-flex items-center gap-2">
            Experimental
            <span className="inline-flex items-center gap-1 rounded-full border border-oz2-warn-bg bg-oz2-warn-bg/40 px-2 py-0.5 font-mono text-[10px] uppercase tracking-wider text-oz2-warn">
              <FlaskConical size={10} />
              Beta
            </span>
          </span>
        }
        sub={
          <>
            Lazy connections are experimental. Functionality and behavior may
            evolve. Instead of maintaining always-on connections, openZro
            activates them on-demand based on activity or signaling.{" "}
            <InlineLink
              href="https://docs.openzro.io/how-to/lazy-connection"
              target="_blank"
            >
              Learn more
              <ExternalLinkIcon size={11} />
            </InlineLink>
          </>
        }
      >
        <OzSettingsToggle
          value={lazyConnection}
          onChange={toggleLazyConnection}
          disabled={!permission.settings.update}
          label="Enable lazy connections"
          desc="Establish peer-to-peer connections only when required. Requires openZro client v0.45 or higher; changes apply after each client restarts."
        />
      </OzSettingsCard>
    </Tabs.Content>
  );
}
