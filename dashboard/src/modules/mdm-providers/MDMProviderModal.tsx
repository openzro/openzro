"use client";

import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
} from "@components/modal/Modal";
import FancyToggleSwitch from "@components/FancyToggleSwitch";
import ModalHeader from "@components/modal/ModalHeader";
import { notify } from "@components/Notification";
import { useApiCall } from "@utils/api";
import { ShieldCheckIcon } from "lucide-react";
import React, { useEffect, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel from "@/components/v2/OzLabel";
import {
  OzSelect,
  OzSelectContent,
  OzSelectItem,
  OzSelectTrigger,
  OzSelectValue,
} from "@/components/v2/OzSelect";
import {
  CrowdStrikeCloud,
  MDMProvider,
  MDMProviderInput,
  MDMProviderType,
} from "@/interfaces/MDMProvider";

type Props = {
  open: boolean;
  setOpen: (open: boolean) => void;
  existing?: MDMProvider | null;
};

export default function MDMProviderModal({
  open,
  setOpen,
  existing,
}: Readonly<Props>) {
  const isEdit = !!existing;
  const [name, setName] = useState("");
  const [type, setType] = useState<MDMProviderType>("intune");
  const [refreshInterval, setRefreshInterval] = useState<number>(5);
  const [saving, setSaving] = useState(false);

  // Intune fields
  const [intuneTenant, setIntuneTenant] = useState("");
  const [intuneClient, setIntuneClient] = useState("");
  const [intuneSecret, setIntuneSecret] = useState("");
  const [intuneStrict, setIntuneStrict] = useState(false);

  // SentinelOne fields
  const [s1URL, setS1URL] = useState("");
  const [s1Token, setS1Token] = useState("");

  // Huntress fields
  const [huntressKey, setHuntressKey] = useState("");
  const [huntressSecret, setHuntressSecret] = useState("");

  // CrowdStrike Falcon fields
  const [csCloud, setCSCloud] = useState<CrowdStrikeCloud>("us-1");
  const [csClientID, setCSClientID] = useState("");
  const [csClientSecret, setCSClientSecret] = useState("");

  const { mutate } = useSWRConfig();
  const apiCreate = useApiCall<MDMProvider>("/admin/mdm-providers");
  const apiUpdate = useApiCall<MDMProvider>(
    `/admin/mdm-providers/${existing?.id ?? 0}`,
  );

  useEffect(() => {
    if (!open) return;
    if (existing) {
      setName(existing.name);
      setType(existing.type);
      setRefreshInterval(existing.refresh_interval_minutes || 5);
      const cfg = existing.config as any;
      switch (existing.type) {
        case "intune":
          setIntuneTenant(cfg?.tenant_id ?? "");
          setIntuneClient(cfg?.client_id ?? "");
          setIntuneSecret("");
          setIntuneStrict(!!cfg?.strict_compliance);
          break;
        case "sentinelone":
          setS1URL(cfg?.management_url ?? "");
          setS1Token("");
          break;
        case "huntress":
          setHuntressKey("");
          setHuntressSecret("");
          break;
        case "crowdstrike":
          setCSCloud((cfg?.cloud as CrowdStrikeCloud) || "us-1");
          setCSClientID(cfg?.client_id ?? "");
          setCSClientSecret("");
          break;
      }
    } else {
      setName("");
      setType("intune");
      setRefreshInterval(5);
      setIntuneTenant("");
      setIntuneClient("");
      setIntuneSecret("");
      setIntuneStrict(false);
      setS1URL("");
      setS1Token("");
      setHuntressKey("");
      setHuntressSecret("");
      setCSCloud("us-1");
      setCSClientID("");
      setCSClientSecret("");
    }
  }, [open, existing]);

  const buildInput = (): MDMProviderInput => {
    const base: MDMProviderInput = {
      name,
      type,
      enabled: true,
      refresh_interval_minutes: refreshInterval,
    };
    if (type === "intune") {
      base.intune = {
        tenant_id: intuneTenant,
        client_id: intuneClient,
        client_secret: intuneSecret || undefined,
        strict_compliance: intuneStrict,
      };
    } else if (type === "sentinelone") {
      base.sentinelone = {
        management_url: s1URL,
        api_token: s1Token || undefined,
      };
    } else if (type === "huntress") {
      base.huntress = {
        api_key: huntressKey || undefined,
        api_secret: huntressSecret || undefined,
      };
    } else if (type === "crowdstrike") {
      base.crowdstrike = {
        cloud: csCloud,
        client_id: csClientID,
        client_secret: csClientSecret || undefined,
      };
    }
    return base;
  };

  const validate = (): string | null => {
    if (!name.trim()) return "Name is required";
    if (
      !Number.isInteger(refreshInterval) ||
      refreshInterval < 1 ||
      refreshInterval > 60
    ) {
      return "Refresh interval must be a whole number between 1 and 60 minutes";
    }
    if (type === "intune") {
      if (!intuneTenant || !intuneClient) {
        return "Intune tenant and client IDs are required";
      }
      if (!isEdit && !intuneSecret) return "Client secret is required";
    } else if (type === "sentinelone") {
      if (!s1URL) return "Management URL is required";
      if (!isEdit && !s1Token) return "API token is required";
    } else if (type === "huntress") {
      if (!isEdit && (!huntressKey || !huntressSecret)) {
        return "API key and secret are required";
      }
    } else if (type === "crowdstrike") {
      if (!csClientID) return "Falcon API client ID is required";
      if (!isEdit && !csClientSecret) {
        return "Falcon API client secret is required";
      }
    }
    return null;
  };

  const onSave = async () => {
    const err = validate();
    if (err) {
      notify({ title: "Validation error", description: err });
      return;
    }
    setSaving(true);
    try {
      if (isEdit) {
        await apiUpdate.put(buildInput());
      } else {
        await apiCreate.post(buildInput());
      }
      await mutate("/admin/mdm-providers");
      setOpen(false);
      notify({
        title: isEdit ? "Updated" : "Created",
        description: `MDM provider "${name}" saved.`,
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal open={open} onOpenChange={setOpen}>
      <ModalContent maxWidthClass="max-w-2xl">
        <ModalHeader
          icon={<ShieldCheckIcon size={20} />}
          title={isEdit ? "Edit MDM/EDR provider" : "Add MDM/EDR provider"}
          description={
            isEdit
              ? "Update credentials. Leave secret fields blank to keep the current value."
              : "Connect Microsoft Intune, SentinelOne, Huntress, or CrowdStrike Falcon to require devices in good security standing."
          }
          truncate
        />

        <div className="px-8 pt-3 pb-6 grid gap-4">
          <div>
            <OzLabel htmlFor="mdm-name">Name</OzLabel>
            <OzInput
              id="mdm-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="prod-intune"
            />
          </div>

          <div>
            <OzLabel>Type</OzLabel>
            <OzSelect
              value={type}
              onValueChange={(v) => setType(v as MDMProviderType)}
              disabled={isEdit}
            >
              <OzSelectTrigger>
                <OzSelectValue placeholder="Select a provider type" />
              </OzSelectTrigger>
              <OzSelectContent>
                <OzSelectItem value="intune">Microsoft Intune</OzSelectItem>
                <OzSelectItem value="sentinelone">SentinelOne</OzSelectItem>
                <OzSelectItem value="huntress">Huntress</OzSelectItem>
                <OzSelectItem value="crowdstrike">
                  CrowdStrike Falcon
                </OzSelectItem>
              </OzSelectContent>
            </OzSelect>
          </div>

          <div>
            <OzLabel htmlFor="mdm-refresh-interval">
              Refresh interval (minutes)
            </OzLabel>
            <OzInput
              id="mdm-refresh-interval"
              type="number"
              min={1}
              max={60}
              step={1}
              value={refreshInterval}
              onChange={(e) =>
                setRefreshInterval(parseInt(e.target.value || "0", 10))
              }
            />
            <p className="mt-1.5 text-[11.5px] leading-[1.5] text-oz2-text-muted">
              How often openZro re-checks each device with the vendor and
              refreshes its cached compliance status. Lower = fresher
              posture, more API calls; higher = fewer calls, slightly
              staler data. Default is 5 minutes; allowed range 1–60.
            </p>
          </div>

          {type === "intune" && (
            <>
              <p className="text-[12px] leading-[1.5] text-oz2-text-muted">
                Register an app in Microsoft Entra (Azure AD) with the{" "}
                <b>DeviceManagementManagedDevices.Read.All</b> permission
                (admin consent required). Use the app&apos;s client ID +
                client secret here.
              </p>
              <div>
                <OzLabel htmlFor="mdm-intune-tenant">Tenant ID</OzLabel>
                <OzInput
                  id="mdm-intune-tenant"
                  value={intuneTenant}
                  onChange={(e) => setIntuneTenant(e.target.value)}
                  placeholder="00000000-0000-0000-0000-000000000000"
                />
              </div>
              <div>
                <OzLabel htmlFor="mdm-intune-client">
                  Client ID (Application ID)
                </OzLabel>
                <OzInput
                  id="mdm-intune-client"
                  value={intuneClient}
                  onChange={(e) => setIntuneClient(e.target.value)}
                />
              </div>
              <div>
                <OzLabel htmlFor="mdm-intune-secret">Client Secret</OzLabel>
                <OzInput
                  id="mdm-intune-secret"
                  type="password"
                  value={intuneSecret}
                  onChange={(e) => setIntuneSecret(e.target.value)}
                  placeholder={isEdit ? "(unchanged)" : ""}
                />
              </div>
              <FancyToggleSwitch
                value={intuneStrict}
                onChange={setIntuneStrict}
                label={"Strict compliance"}
                helpText={
                  "When OFF (default), Intune's `inGracePeriod` state counts as compliant — devices keep access while their config drifts back into policy. When ON, peers drop off the network the moment Intune flags them, even before the grace window expires."
                }
              />
            </>
          )}

          {type === "sentinelone" && (
            <>
              <p className="text-[12px] leading-[1.5] text-oz2-text-muted">
                Mint an API Token in the S1 console under Settings →
                Users → Service Users (Viewer role is enough).
              </p>
              <div>
                <OzLabel htmlFor="mdm-s1-url">Management URL</OzLabel>
                <OzInput
                  id="mdm-s1-url"
                  value={s1URL}
                  onChange={(e) => setS1URL(e.target.value)}
                  placeholder="https://acme.sentinelone.net"
                />
              </div>
              <div>
                <OzLabel htmlFor="mdm-s1-token">API Token</OzLabel>
                <OzInput
                  id="mdm-s1-token"
                  type="password"
                  value={s1Token}
                  onChange={(e) => setS1Token(e.target.value)}
                  placeholder={isEdit ? "(unchanged)" : ""}
                />
              </div>
            </>
          )}

          {type === "huntress" && (
            <>
              <p className="text-[12px] leading-[1.5] text-oz2-text-muted">
                Generate API credentials in the Huntress dashboard
                under Account Settings → API Credentials.
              </p>
              <div>
                <OzLabel htmlFor="mdm-huntress-key">API Key</OzLabel>
                <OzInput
                  id="mdm-huntress-key"
                  value={huntressKey}
                  onChange={(e) => setHuntressKey(e.target.value)}
                  placeholder={isEdit ? "(unchanged)" : ""}
                />
              </div>
              <div>
                <OzLabel htmlFor="mdm-huntress-secret">API Secret</OzLabel>
                <OzInput
                  id="mdm-huntress-secret"
                  type="password"
                  value={huntressSecret}
                  onChange={(e) => setHuntressSecret(e.target.value)}
                  placeholder={isEdit ? "(unchanged)" : ""}
                />
              </div>
            </>
          )}

          {type === "crowdstrike" && (
            <>
              <p className="text-[12px] leading-[1.5] text-oz2-text-muted">
                Mint a Falcon API client in the CrowdStrike console
                under Support → API Clients and Keys with the{" "}
                <b>Hosts: Read</b> scope. Pick the cloud region your
                Falcon tenant lives in — the same client cannot
                cross regions.
              </p>
              <div>
                <OzLabel>Cloud</OzLabel>
                <OzSelect
                  value={csCloud}
                  onValueChange={(v) => setCSCloud(v as CrowdStrikeCloud)}
                >
                  <OzSelectTrigger>
                    <OzSelectValue />
                  </OzSelectTrigger>
                  <OzSelectContent>
                    <OzSelectItem value="us-1">
                      US-1 (api.crowdstrike.com)
                    </OzSelectItem>
                    <OzSelectItem value="us-2">
                      US-2 (api.us-2.crowdstrike.com)
                    </OzSelectItem>
                    <OzSelectItem value="eu-1">
                      EU-1 (api.eu-1.crowdstrike.com)
                    </OzSelectItem>
                    <OzSelectItem value="us-gov-1">
                      US-GOV-1 (Falcon GovCloud)
                    </OzSelectItem>
                    <OzSelectItem value="us-gov-2">
                      US-GOV-2 (Falcon GovCloud 2)
                    </OzSelectItem>
                  </OzSelectContent>
                </OzSelect>
              </div>
              <div>
                <OzLabel htmlFor="mdm-cs-client-id">API Client ID</OzLabel>
                <OzInput
                  id="mdm-cs-client-id"
                  value={csClientID}
                  onChange={(e) => setCSClientID(e.target.value)}
                />
              </div>
              <div>
                <OzLabel htmlFor="mdm-cs-client-secret">
                  API Client Secret
                </OzLabel>
                <OzInput
                  id="mdm-cs-client-secret"
                  type="password"
                  value={csClientSecret}
                  onChange={(e) => setCSClientSecret(e.target.value)}
                  placeholder={isEdit ? "(unchanged)" : ""}
                />
              </div>
            </>
          )}
        </div>

        <ModalFooter className="items-center gap-3">
          <ModalClose asChild>
            <OzButton variant="default">Cancel</OzButton>
          </ModalClose>
          <OzButton variant="primary" onClick={onSave} disabled={saving}>
            {saving ? "Saving..." : isEdit ? "Save changes" : "Create"}
          </OzButton>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
