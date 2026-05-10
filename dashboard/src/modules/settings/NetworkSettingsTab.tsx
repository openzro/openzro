"use client";

import InlineLink from "@components/InlineLink";
import { Input } from "@components/Input";
import { notify } from "@components/Notification";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import { useHasChanges } from "@hooks/useHasChanges";
import * as Tabs from "@radix-ui/react-tabs";
import { useApiCall } from "@utils/api";
import loadConfig from "@utils/config";
import { validator } from "@utils/helpers";
import { API_ORIGIN, isOpenzroHosted } from "@utils/openzro";
import { ExternalLinkIcon, Filter } from "lucide-react";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Account } from "@/interfaces/Account";
import useGroupHelper from "@/modules/groups/useGroupHelper";
import OzSettingsCard from "@/modules/settings/v2/OzSettingsCard";
import OzSettingsField from "@/modules/settings/v2/OzSettingsField";
import OzSettingsToggle from "@/modules/settings/v2/OzSettingsToggle";

type Props = {
  account: Account;
};

// collidesWithControlPlane returns the conflicting host when `input`
// would make the mesh DNS proxy claim authority over the management
// or gRPC API hostname. Two collision shapes count:
// 1. exact match (input === mgmt host) — proxy owns the bare zone, so
//    the bare hostname returns NXDOMAIN.
// 2. mgmt host is a subdomain of input — proxy owns the parent zone
//    AND every child, including the mgmt host.
// Returns the offending host so the error message can name it.
function collidesWithControlPlane(input: string): string | null {
  const cfg = loadConfig();
  const candidates: string[] = [];
  for (const origin of [API_ORIGIN, cfg.grpcApiOrigin, cfg.authority]) {
    if (!origin) continue;
    try {
      const host = new URL(origin).hostname.toLowerCase();
      if (host) candidates.push(host);
    } catch {
      // origin not a parseable URL (dev fallback) — skip
    }
  }
  const zone = input.toLowerCase();
  for (const host of candidates) {
    if (host === zone) return host;
    if (host.endsWith("." + zone)) return host;
  }
  return null;
}

// NetworkSettingsTab — settings sub-page body for /settings/networks.
// Functionality preserved verbatim: custom DNS domain (validated +
// control-plane-collision-guarded), routing-peer DNS wildcard toggle,
// network traffic events toggle with optional per-group filter. Only
// paint changes — three OzSettingsCards (DNS domain / DNS wildcard /
// Traffic events), with the traffic-events scope filter in a sunken
// sub-card. Input + PeerGroupSelector keep their legacy paint here
// because they handle complex validation/multi-select state that
// would balloon scope to refactor.

export default function NetworkSettingsTab({ account }: Readonly<Props>) {
  const { permission } = usePermissions();
  const { mutate } = useSWRConfig();
  const saveRequest = useApiCall<Account>("/accounts/" + account.id, true);

  const [routingPeerDNSSetting, setRoutingPeerDNSSetting] = useState(
    account.settings.routing_peer_dns_resolution_enabled,
  );
  const [customDNSDomain, setCustomDNSDomain] = useState(
    account.settings.dns_domain || "",
  );
  const [flowEnabled, setFlowEnabled] = useState(
    account.settings.extra?.network_traffic_logs_enabled ?? false,
  );

  const [flowGroups, setFlowGroups, { save: saveFlowGroups }] = useGroupHelper(
    {
      initial: account.settings.extra?.network_traffic_logs_groups ?? [],
    },
  );

  const initialFlowGroupIDs = useMemo(
    () =>
      [...(account.settings.extra?.network_traffic_logs_groups ?? [])].sort(),
    [account.settings.extra?.network_traffic_logs_groups],
  );
  const flowGroupsDirty = useMemo(() => {
    const current = [...flowGroups.map((g) => g.id)].sort();
    if (current.length !== initialFlowGroupIDs.length) return true;
    for (let i = 0; i < current.length; i++) {
      if (current[i] !== initialFlowGroupIDs[i]) return true;
    }
    return false;
  }, [flowGroups, initialFlowGroupIDs]);

  const persistFlowExtra = async (
    enabled: boolean,
    successMessage: string,
    loadingMessage: string,
  ) => {
    const persistedGroups = await saveFlowGroups();
    const groupIDs = persistedGroups.map((g) => g.id);
    notify({
      title: "Network Traffic Events",
      description: successMessage,
      promise: saveRequest
        .put({
          id: account.id,
          settings: {
            ...account.settings,
            extra: {
              ...account.settings.extra,
              network_traffic_logs_enabled: enabled,
              network_traffic_logs_groups: groupIDs,
            },
          },
        })
        .then(() => {
          setFlowEnabled(enabled);
          setFlowGroups(persistedGroups);
          mutate("/accounts");
        }),
      loadingMessage,
    });
  };

  const toggleFlowSetting = (toggle: boolean) =>
    persistFlowExtra(
      toggle,
      `Network Traffic Events successfully ${toggle ? "enabled" : "disabled"}.`,
      "Updating Network Traffic Events...",
    );

  const saveFlowGroupsFilter = () =>
    persistFlowExtra(
      flowEnabled,
      flowGroups.length > 0
        ? `Traffic events scoped to ${flowGroups.length} ${
            flowGroups.length === 1 ? "group" : "groups"
          }.`
        : "Traffic events now apply to all peers.",
      "Updating traffic events scope...",
    );

  const toggleNetworkDNSSetting = async (toggle: boolean) => {
    notify({
      title: "DNS Wildcard Routing",
      description: `DNS Wildcard Routing successfully ${
        toggle ? "enabled" : "disabled"
      }.`,
      promise: saveRequest
        .put({
          id: account.id,
          settings: {
            ...account.settings,
            routing_peer_dns_resolution_enabled: toggle,
          },
        })
        .then(() => {
          setRoutingPeerDNSSetting(toggle);
          mutate("/accounts");
        }),
      loadingMessage: "Updating DNS wildcard setting...",
    });
  };

  const { hasChanges, updateRef } = useHasChanges([customDNSDomain]);

  const saveChanges = async () => {
    notify({
      title: "Custom DNS Domain",
      description: `Custom DNS Domain successfully updated.`,
      promise: saveRequest
        .put({
          id: account.id,
          settings: {
            ...account.settings,
            dns_domain: customDNSDomain || "",
          },
        })
        .then(() => {
          mutate("/accounts");
          updateRef([customDNSDomain]);
        }),
      loadingMessage: "Updating Custom DNS domain...",
    });
  };

  const domainError = useMemo(() => {
    if (customDNSDomain == "") return "";
    const valid = validator.isValidDomain(customDNSDomain, {
      allowWildcard: false,
      allowOnlyTld: false,
    });
    if (!valid) {
      return "Please enter a valid domain, e.g. example.com or intra.example.com";
    }
    const collision = collidesWithControlPlane(customDNSDomain);
    if (collision) {
      return `Cannot use "${collision}" — peers would no longer resolve the openZro control plane (this zone covers it).`;
    }
  }, [customDNSDomain]);

  const editDisabled = !permission.settings.update;

  return (
    <Tabs.Content value="networks" className="flex flex-col gap-5">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="text-[18px] font-semibold tracking-tight text-oz2-text">
            Networks
          </h2>
          <p className="mt-1 max-w-2xl text-[13px] leading-[1.55] text-oz2-text-muted">
            Per-network defaults that flow to every peer: the DNS zone the mesh
            owns, wildcard routing, and the per-flow traffic events surface.
          </p>
        </div>
        <OzButton
          variant="primary"
          type="button"
          disabled={!hasChanges || !!domainError || editDisabled}
          onClick={saveChanges}
        >
          Save Changes
        </OzButton>
      </header>

      <OzSettingsCard
        title="Custom DNS domain"
        sub="Override the default mesh DNS zone. Peers register short names under this zone and resolve each other as <name>.<zone>."
      >
        <OzSettingsField
          label="DNS Domain"
          hint="Must not overlap with the management server's own hostname, or peers will lose access to the control plane on every reconnect."
        >
          <Input
            placeholder={
              isOpenzroHosted() ? "openzro.cloud" : "openzro.selfhosted"
            }
            errorTooltip={true}
            errorTooltipPosition="top"
            error={domainError}
            value={customDNSDomain}
            disabled={editDisabled}
            onChange={(e) => setCustomDNSDomain(e.target.value)}
          />
        </OzSettingsField>
      </OzSettingsCard>

      <OzSettingsCard
        title="DNS wildcard routing"
        sub={
          <>
            Allow routing rules to target whole DNS zones via wildcards instead
            of explicit IPs.{" "}
            <InlineLink
              href="https://docs.openzro.io/how-to/accessing-entire-domains-within-networks#enabling-dns-wildcard-routing"
              target="_blank"
            >
              Learn more
              <ExternalLinkIcon size={11} />
            </InlineLink>
          </>
        }
      >
        <OzSettingsToggle
          value={routingPeerDNSSetting}
          onChange={toggleNetworkDNSSetting}
          disabled={editDisabled}
          label="Enable DNS wildcard routing"
          desc="Requires openZro client v0.35 or higher; changes apply after each client restarts."
        />
      </OzSettingsCard>

      <OzSettingsCard
        title="Network traffic events"
        sub={
          <>
            Capture per-flow events from peers (start / drop / end of every TCP
            / UDP / ICMP connection) and persist them on the management server
            for the Flow Traffic page and any configured exporters. Persistence
            requires management to be configured with a flow store engine
            (postgres/mysql/sqlite via{" "}
            <code className="font-mono text-[11.5px]">
              OPENZRO_FLOW_STORE_ENGINE
            </code>{" "}
            + DSN); without those env vars events are accepted on the gRPC
            stream and dropped after acking.
          </>
        }
      >
        <OzSettingsToggle
          value={flowEnabled}
          onChange={toggleFlowSetting}
          disabled={editDisabled}
          label="Enable network traffic events"
          desc="Disabled by default — enable when you need connection-level audit visibility."
        />

        {flowEnabled && (
          <div
            className={
              "flex flex-col gap-3 rounded-oz2-card border border-oz2-border-soft bg-oz2-bg-sunken p-4 " +
              (editDisabled ? "opacity-60" : "")
            }
          >
            <div>
              <div className="inline-flex items-center gap-2 text-[13px] font-medium text-oz2-text-2">
                <Filter size={13} />
                Limit to specific groups
              </div>
              <p className="mt-[3px] text-[11.5px] leading-[1.45] text-oz2-text-faint">
                Optional. When set, only peers in these groups capture and
                report traffic events — excluded peers never spend CPU on
                conntrack and never push events to management. Leave empty to
                apply to all peers.
              </p>
            </div>
            <PeerGroupSelector
              values={flowGroups}
              onChange={setFlowGroups}
              disabled={editDisabled}
              hideAllGroup
            />
            <div>
              <OzButton
                variant="primary"
                type="button"
                disabled={!flowGroupsDirty || editDisabled}
                onClick={saveFlowGroupsFilter}
                className="!h-[30px] !px-3 !text-[12.5px]"
              >
                Save filter
              </OzButton>
            </div>
          </div>
        )}
      </OzSettingsCard>
    </Tabs.Content>
  );
}
