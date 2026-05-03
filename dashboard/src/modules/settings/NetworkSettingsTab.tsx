import Breadcrumbs from "@components/Breadcrumbs";
import Button from "@components/Button";
import FancyToggleSwitch from "@components/FancyToggleSwitch";
import HelpText from "@components/HelpText";
import InlineLink from "@components/InlineLink";
import { Input } from "@components/Input";
import { Label } from "@components/Label";
import { notify } from "@components/Notification";
import Paragraph from "@components/Paragraph";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import { useHasChanges } from "@hooks/useHasChanges";
import * as Tabs from "@radix-ui/react-tabs";
import { useApiCall } from "@utils/api";
import loadConfig from "@utils/config";
import { cn, validator } from "@utils/helpers";
import { API_ORIGIN, isOpenzroHosted } from "@utils/openzro";
import { ActivityIcon, ExternalLinkIcon, FilterIcon, GlobeIcon, NetworkIcon } from "lucide-react";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import SettingsIcon from "@/assets/icons/SettingsIcon";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Account } from "@/interfaces/Account";
import { Group } from "@/interfaces/Group";
import useGroupHelper from "@/modules/groups/useGroupHelper";

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

  // Group filter for traffic events. The dashboard owns Group[] (with name
  // + meta) for display, but the API only round-trips group IDs — we
  // initialize from the IDs present in account.settings.extra and the
  // useGroupHelper hook resolves them to full Group objects against the
  // GroupsProvider cache. On save we extract IDs back out.
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

  // persistFlowExtra round-trips the toggle + the (saved) group set in a
  // single PUT. Group rows that did not exist server-side are created
  // first via saveFlowGroups so the /accounts endpoint receives only
  // canonical IDs. Used by both the toggle handler and the explicit
  // Save Filter button.
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
    // Reject zones that the management server's own hostname lives in.
    // The local DNS proxy on each peer claims authority over this zone,
    // so if it overlaps with management's FQDN, peers will resolve the
    // management URL to NXDOMAIN and lose connection on every reconnect.
    // We learned this the hard way (see ADR-0002 footnote / fix in
    // helm chart 2.1.0-alpha.6).
    const collision = collidesWithControlPlane(customDNSDomain);
    if (collision) {
      return `Cannot use "${collision}" — peers would no longer resolve the openZro control plane (this zone covers it).`;
    }
  }, [customDNSDomain]);

  return (
    <Tabs.Content value={"networks"}>
      <div className={"p-default py-6 max-w-2xl"}>
        <Breadcrumbs>
          <Breadcrumbs.Item
            href={"/settings"}
            label={"Settings"}
            icon={<SettingsIcon size={13} />}
          />
          <Breadcrumbs.Item
            href={"/settings?tab=networks"}
            label={"Networks"}
            icon={<NetworkIcon size={14} />}
            active
          />
        </Breadcrumbs>
        <div className={"flex items-start justify-between"}>
          <div>
            <h1>Networks</h1>
          </div>
          <Button
            variant={"primary"}
            disabled={
              !hasChanges || !!domainError || !permission.settings.update
            }
            onClick={saveChanges}
          >
            Save Changes
          </Button>
        </div>

        <div className={"flex flex-col gap-6 w-full mt-8"}>
          <div>
            <div
              className={
                "flex flex-col gap-1 sm:flex-row w-full sm:gap-4 items-center"
              }
            >
              <div className={"min-w-[330px]"}>
                <Label>DNS Domain</Label>
                <HelpText>
                  Specify a custom peer DNS domain for your network. This should
                  not point to a domain that is already in use elsewhere, to avoid overriding DNS results.
                </HelpText>
              </div>
              <div className={"w-full"}>
                <Input
                  placeholder={
                    isOpenzroHosted() ? "openzro.cloud" : "openzro.selfhosted"
                  }
                  errorTooltip={true}
                  errorTooltipPosition={"top"}
                  error={domainError}
                  value={customDNSDomain}
                  disabled={!permission.settings.update}
                  onChange={(e) => setCustomDNSDomain(e.target.value)}
                />
              </div>
            </div>
          </div>

          <FancyToggleSwitch
            value={routingPeerDNSSetting}
            onChange={toggleNetworkDNSSetting}
            label={
              <>
                <GlobeIcon size={15} />
                Enable DNS Wildcard Routing
              </>
            }
            helpText={
              <>
                Allow routing using DNS wildcards. This requires Openzro client
                v0.35 or higher. Changes will only take effect after restarting
                the clients.{" "}
                <InlineLink
                  href={
                    "https://docs.openzro.io/how-to/accessing-entire-domains-within-networks#enabling-dns-wildcard-routing"
                  }
                  target={"_blank"}
                  onClick={(e) => e.stopPropagation()}
                >
                  Learn more
                  <ExternalLinkIcon size={12} />
                </InlineLink>
              </>
            }
            disabled={!permission.settings.update}
          />

          {/* Toggle + its expansion live in their own gap-less
              wrapper so the parent's `gap-6` between settings cards
              does not push the two surfaces apart — same trick the
              AuthenticationTab uses for its Session Expiration block. */}
          <div className={"flex flex-col"}>
            <FancyToggleSwitch
              value={flowEnabled}
              onChange={toggleFlowSetting}
              label={
                <>
                  <ActivityIcon size={15} />
                  Enable Network Traffic Events
                </>
              }
              helpText={
                <>
                  Capture per-flow events from peers (start / drop / end of
                  every TCP / UDP / ICMP connection) and persist them on the
                  management server for the Network Traffic page and any
                  configured exporters. Disabled by default — enable when you
                  need connection-level audit visibility. Persistence requires
                  management to be configured with a flow store engine
                  (postgres/mysql/sqlite via{" "}
                  <code>OPENZRO_FLOW_STORE_ENGINE</code> + DSN); without those
                  env vars events are accepted on the gRPC stream and dropped
                  after acking.
                </>
              }
              disabled={!permission.settings.update}
              // Square the toggle's bottom corners when the expansion
              // is open so the two surfaces visually join.
              className={flowEnabled ? "!rounded-b-none" : undefined}
            />

            {flowEnabled && (
              <div
                className={cn(
                  "border border-t-0 rounded-b-md px-[1.28rem] pt-3 pb-5 flex flex-col gap-3 mx-[0.25rem]",
                  "border-neutral-200 bg-neutral-50",
                  "dark:border-nb-gray-900 dark:bg-nb-gray-940",
                  !permission.settings.update &&
                    "opacity-50 pointer-events-none",
                )}
              >
                <div>
                  <Label className={"flex items-center gap-2"}>
                    <FilterIcon size={14} />
                    Limit to specific groups
                  </Label>
                  <HelpText>
                    Optional. When set, only peers in these groups capture
                    and report traffic events — excluded peers never spend
                    CPU on conntrack and never push events to management.
                    Leave empty to apply to all peers.
                  </HelpText>
                </div>
                <PeerGroupSelector
                  values={flowGroups}
                  onChange={setFlowGroups}
                  disabled={!permission.settings.update}
                  hideAllGroup
                />
                <div className={"flex"}>
                  <Button
                    size={"sm"}
                    variant={"primary"}
                    disabled={
                      !flowGroupsDirty || !permission.settings.update
                    }
                    onClick={saveFlowGroupsFilter}
                  >
                    Save filter
                  </Button>
                </div>
              </div>
            )}
          </div>
        </div>
      </div>
    </Tabs.Content>
  );
}
