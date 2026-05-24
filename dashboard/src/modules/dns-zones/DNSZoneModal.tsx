"use client";

import FancyToggleSwitch from "@components/FancyToggleSwitch";
import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { notify } from "@components/Notification";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import { useApiCall } from "@utils/api";
import {
  ExternalLinkIcon,
  Layers,
  PlusCircle,
  Power,
  Scan,
  Users,
} from "lucide-react";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import DNSIcon from "@/assets/icons/DNSIcon";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import {
  OzTabs as Tabs,
  OzTabsContent as TabsContent,
  OzTabsList as TabsList,
  OzTabsTrigger as TabsTrigger,
} from "@/components/v2/OzTabs";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { DNSZone, DNSZoneRequest } from "@/interfaces/DNSZone";
import useGroupHelper from "@/modules/groups/useGroupHelper";

type Props = {
  preset?: DNSZone;
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

export default function DNSZoneModal({
  preset,
  open,
  onOpenChange,
}: Readonly<Props>) {
  return (
    <Modal open={open} onOpenChange={onOpenChange} key={open ? 1 : 0}>
      {open && (
        <DNSZoneModalContent
          onSuccess={() => onOpenChange(false)}
          preset={preset}
        />
      )}
    </Modal>
  );
}

type ModalContentProps = {
  onSuccess?: () => void;
  preset?: DNSZone;
};

const FQDN_RE = /^(?=.{1,253}$)(?:(?!-)[A-Za-z0-9-]{1,63}(?<!-)\.)+[A-Za-z]{2,63}$/;

function validateDomain(domain: string): string {
  if (!domain) return "Domain is required";
  if (!FQDN_RE.test(domain)) {
    return "Enter a valid FQDN, e.g. internal.example";
  }
  return "";
}

export function DNSZoneModalContent({
  onSuccess,
  preset,
}: Readonly<ModalContentProps>) {
  const { permission } = usePermissions();
  const zoneRequest = useApiCall<DNSZone>("/dns/zones", true);
  const { mutate } = useSWRConfig();

  const isUpdate = useMemo(() => !!(preset && preset.id), [preset]);

  const [name, setName] = useState(preset?.name || "");
  const [domain, setDomain] = useState(preset?.domain || "");
  const [enabled, setEnabled] = useState<boolean>(
    typeof preset?.enabled === "boolean" ? preset.enabled : true,
  );
  const [searchDomain, setSearchDomain] = useState<boolean>(
    typeof preset?.enable_search_domain === "boolean"
      ? preset.enable_search_domain
      : false,
  );
  const [groups, setGroups, { save: saveGroups }] = useGroupHelper({
    initial: preset?.distribution_groups || [],
  });

  const [tab, setTab] = useState<"zone" | "distribution">("zone");

  const nameError = useMemo(() => {
    if (!name) return "";
    if (name.length > 255) return "Name should be less than 255 characters";
    return "";
  }, [name]);

  const domainError = useMemo(() => validateDomain(domain), [domain]);

  const canAction = useMemo(() => {
    return isUpdate
      ? permission.dns_zones.update
      : permission.dns_zones.create;
  }, [isUpdate, permission]);

  // Zone tab → Distribution tab gate. Mirrors the NameserverModal
  // step-flow: the operator can't advance until the zone metadata is
  // syntactically valid.
  const canContinueToDistribution = useMemo(() => {
    return !!name && !nameError && !domainError;
  }, [name, nameError, domainError]);

  const canSubmit = useMemo(() => {
    return canContinueToDistribution && groups.length > 0 && canAction;
  }, [canContinueToDistribution, groups.length, canAction]);

  const submit = async () => {
    const savedGroups = await saveGroups();
    const groupIds = savedGroups.map((g) => g.id).filter(Boolean) as string[];

    const body: DNSZoneRequest = {
      name,
      domain,
      enabled,
      enable_search_domain: searchDomain,
      distribution_groups: groupIds,
    };

    if (isUpdate) {
      notify({
        title: "Update DNS Zone",
        description: "Zone was updated successfully.",
        loadingMessage: "Updating your zone...",
        promise: zoneRequest.put(body, `/${preset?.id}`).then(() => {
          // List + detail share state — invalidate both so the
          // /dns/zone header (which fetches /dns/zones/${id}) picks
          // up the new name/enabled/groups/search-domain values
          // immediately after the modal closes.
          mutate("/dns/zones");
          if (preset?.id) mutate(`/dns/zones/${preset.id}`);
          onSuccess?.();
        }),
      });
    } else {
      notify({
        title: "Create DNS Zone",
        description: "Zone was created successfully.",
        loadingMessage: "Creating your zone...",
        promise: zoneRequest.post(body).then(() => {
          mutate("/dns/zones");
          onSuccess?.();
        }),
      });
    }
  };

  return (
    <ModalContent maxWidthClass={"max-w-xl"}>
      <ModalHeader
        icon={<DNSIcon className={"fill-openzro"} />}
        title={isUpdate ? preset?.name : "Add DNS Zone"}
        description={
          "Publish authoritative DNS records to peers in selected groups"
        }
        color={"openzro"}
      />

      <Tabs
        defaultValue={tab}
        onValueChange={(v) => setTab(v as typeof tab)}
        value={tab}
      >
        <div className="px-8 pb-3 pt-1">
          <TabsList>
            <TabsTrigger value={"zone"}>
              <Layers
                size={16}
                className="text-oz2-text-faint group-data-[state=active]/trigger:text-oz2-acc transition-colors"
              />
              Zone
            </TabsTrigger>
            <TabsTrigger
              value={"distribution"}
              disabled={!isUpdate && !canContinueToDistribution}
            >
              <Users
                size={16}
                className="text-oz2-text-faint group-data-[state=active]/trigger:text-oz2-acc transition-colors"
              />
              Distribution
            </TabsTrigger>
          </TabsList>
        </div>

        {/* ── Zone tab ─────────────────────────────────────────────── */}
        <TabsContent value={"zone"} className={"pb-8"}>
          <div className={"px-8 flex flex-col gap-6"}>
            <div>
              <OzLabel htmlFor="zone-name">Name</OzLabel>
              <OzHelpText className="mb-2">
                A human-readable label for this zone.
              </OzHelpText>
              <OzInput
                id="zone-name"
                autoFocus={true}
                error={nameError}
                placeholder={"e.g., Internal services"}
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={!canAction}
              />
            </div>

            <div>
              <OzLabel htmlFor="zone-domain">Domain</OzLabel>
              <OzHelpText className="mb-2">
                The FQDN apex of this zone (e.g.{" "}
                <code className="font-mono">internal.example</code>). Immutable
                after creation — create a new zone if you need to change it.
              </OzHelpText>
              <OzInput
                id="zone-domain"
                mono
                error={domain ? domainError : ""}
                placeholder={"e.g., internal.example"}
                value={domain}
                onChange={(e) => setDomain(e.target.value.trim().toLowerCase())}
                disabled={!canAction || isUpdate}
              />
            </div>

            <FancyToggleSwitch
              value={enabled}
              onChange={setEnabled}
              label={
                <>
                  <Power size={15} />
                  Enable Zone
                </>
              }
              helpText={
                "When disabled, the zone is not distributed to peers — useful for pausing without deleting."
              }
              disabled={!canAction}
            />

            <FancyToggleSwitch
              value={searchDomain}
              onChange={setSearchDomain}
              label={
                <>
                  <Scan size={15} />
                  Append to search domain
                </>
              }
              helpText={
                "When enabled, peers can resolve bare names against this zone (e.g. 'db' → 'db.<domain>')."
              }
              disabled={!canAction}
            />
          </div>
        </TabsContent>

        {/* ── Distribution tab ────────────────────────────────────── */}
        <TabsContent value={"distribution"} className={"pb-8"}>
          <div className={"px-8 flex flex-col gap-6"}>
            <div>
              <OzLabel>Distribution Groups</OzLabel>
              <OzHelpText className="mb-2">
                Peers that belong to any of these groups receive this zone.
                At least one group is required.
              </OzHelpText>
              <PeerGroupSelector
                onChange={setGroups}
                values={groups}
                disabled={!canAction}
                showResources={false}
              />
              {groups.length === 0 && (
                <p className="mt-2 text-[11.5px] text-oz2-err">
                  Select at least one distribution group.
                </p>
              )}
            </div>
          </div>
        </TabsContent>
      </Tabs>

      <ModalFooter className={"items-center"}>
        <div className={"w-full"}>
          <p className={"text-sm mt-auto text-oz2-text-muted"}>
            Learn more about{" "}
            <a
              href={
                "https://docs.openzro.io/how-to/manage-dns-in-your-network"
              }
              target={"_blank"}
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
            >
              DNS zones
              <ExternalLinkIcon size={12} />
            </a>
          </p>
        </div>
        <div className={"flex gap-3 w-full justify-end"}>
          {!isUpdate ? (
            // Step-flow on create: Zone → Distribution → Add Zone.
            // Records tab is disabled on create (records are managed
            // post-creation via the dedicated endpoints).
            <>
              {tab === "zone" && (
                <>
                  <ModalClose asChild={true}>
                    <OzButton variant={"default"}>Cancel</OzButton>
                  </ModalClose>
                  <OzButton
                    variant={"primary"}
                    disabled={!canContinueToDistribution}
                    onClick={() => setTab("distribution")}
                  >
                    Continue
                  </OzButton>
                </>
              )}
              {tab === "distribution" && (
                <>
                  <OzButton
                    variant={"default"}
                    onClick={() => setTab("zone")}
                  >
                    Back
                  </OzButton>
                  <OzButton
                    variant={"primary"}
                    disabled={!canSubmit}
                    onClick={submit}
                  >
                    <PlusCircle size={16} />
                    Add Zone
                  </OzButton>
                </>
              )}
            </>
          ) : (
            // Edit: any tab saves directly. Records sub-CRUD is
            // self-contained inside its tab.
            <>
              <ModalClose asChild={true}>
                <OzButton variant={"default"}>Cancel</OzButton>
              </ModalClose>
              <OzButton
                variant={"primary"}
                disabled={!canSubmit}
                onClick={submit}
              >
                Save Changes
              </OzButton>
            </>
          )}
        </div>
      </ModalFooter>
    </ModalContent>
  );
}
