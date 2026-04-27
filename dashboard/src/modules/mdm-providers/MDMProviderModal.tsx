"use client";

import Button from "@components/Button";
import { Input } from "@components/Input";
import { Label } from "@components/Label";
import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { notify } from "@components/Notification";
import Paragraph from "@components/Paragraph";
import { useApiCall } from "@utils/api";
import { ShieldCheckIcon } from "lucide-react";
import React, { useEffect, useState } from "react";
import { useSWRConfig } from "swr";
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
  const [saving, setSaving] = useState(false);

  // Intune fields
  const [intuneTenant, setIntuneTenant] = useState("");
  const [intuneClient, setIntuneClient] = useState("");
  const [intuneSecret, setIntuneSecret] = useState("");

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
      const cfg = existing.config as any;
      switch (existing.type) {
        case "intune":
          setIntuneTenant(cfg?.tenant_id ?? "");
          setIntuneClient(cfg?.client_id ?? "");
          setIntuneSecret("");
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
      setIntuneTenant("");
      setIntuneClient("");
      setIntuneSecret("");
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
    const base: MDMProviderInput = { name, type, enabled: true };
    if (type === "intune") {
      base.intune = {
        tenant_id: intuneTenant,
        client_id: intuneClient,
        client_secret: intuneSecret || undefined,
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
            <Label>Name</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="prod-intune"
            />
          </div>

          <div>
            <Label>Type</Label>
            <select
              value={type}
              disabled={isEdit}
              onChange={(e) => setType(e.target.value as MDMProviderType)}
              className="w-full rounded-md border border-nb-gray-700 bg-nb-gray-940 px-3 py-2 text-sm"
            >
              <option value="intune">Microsoft Intune</option>
              <option value="sentinelone">SentinelOne</option>
              <option value="huntress">Huntress</option>
              <option value="crowdstrike">CrowdStrike Falcon</option>
            </select>
          </div>

          {type === "intune" && (
            <>
              <Paragraph className="text-xs text-nb-gray-300">
                Register an app in Microsoft Entra (Azure AD) with the{" "}
                <b>DeviceManagementManagedDevices.Read.All</b> permission
                (admin consent required). Use the app&apos;s client ID +
                client secret here.
              </Paragraph>
              <div>
                <Label>Tenant ID</Label>
                <Input
                  value={intuneTenant}
                  onChange={(e) => setIntuneTenant(e.target.value)}
                  placeholder="00000000-0000-0000-0000-000000000000"
                />
              </div>
              <div>
                <Label>Client ID (Application ID)</Label>
                <Input
                  value={intuneClient}
                  onChange={(e) => setIntuneClient(e.target.value)}
                />
              </div>
              <div>
                <Label>Client Secret</Label>
                <Input
                  type="password"
                  value={intuneSecret}
                  onChange={(e) => setIntuneSecret(e.target.value)}
                  placeholder={isEdit ? "(unchanged)" : ""}
                />
              </div>
            </>
          )}

          {type === "sentinelone" && (
            <>
              <Paragraph className="text-xs text-nb-gray-300">
                Mint an API Token in the S1 console under Settings →
                Users → Service Users (Viewer role is enough).
              </Paragraph>
              <div>
                <Label>Management URL</Label>
                <Input
                  value={s1URL}
                  onChange={(e) => setS1URL(e.target.value)}
                  placeholder="https://acme.sentinelone.net"
                />
              </div>
              <div>
                <Label>API Token</Label>
                <Input
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
              <Paragraph className="text-xs text-nb-gray-300">
                Generate API credentials in the Huntress dashboard
                under Account Settings → API Credentials.
              </Paragraph>
              <div>
                <Label>API Key</Label>
                <Input
                  value={huntressKey}
                  onChange={(e) => setHuntressKey(e.target.value)}
                  placeholder={isEdit ? "(unchanged)" : ""}
                />
              </div>
              <div>
                <Label>API Secret</Label>
                <Input
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
              <Paragraph className="text-xs text-nb-gray-300">
                Mint a Falcon API client in the CrowdStrike console
                under Support → API Clients and Keys with the{" "}
                <b>Hosts: Read</b> scope. Pick the cloud region your
                Falcon tenant lives in — the same client cannot
                cross regions.
              </Paragraph>
              <div>
                <Label>Cloud</Label>
                <select
                  value={csCloud}
                  onChange={(e) =>
                    setCSCloud(e.target.value as CrowdStrikeCloud)
                  }
                  className="w-full rounded-md border border-nb-gray-700 bg-nb-gray-940 px-3 py-2 text-sm"
                >
                  <option value="us-1">US-1 (api.crowdstrike.com)</option>
                  <option value="us-2">US-2 (api.us-2.crowdstrike.com)</option>
                  <option value="eu-1">EU-1 (api.eu-1.crowdstrike.com)</option>
                  <option value="us-gov-1">US-GOV-1 (Falcon GovCloud)</option>
                  <option value="us-gov-2">US-GOV-2 (Falcon GovCloud 2)</option>
                </select>
              </div>
              <div>
                <Label>API Client ID</Label>
                <Input
                  value={csClientID}
                  onChange={(e) => setCSClientID(e.target.value)}
                />
              </div>
              <div>
                <Label>API Client Secret</Label>
                <Input
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
            <Button variant="secondary">Cancel</Button>
          </ModalClose>
          <Button variant="primary" onClick={onSave} disabled={saving}>
            {saving ? "Saving..." : isEdit ? "Save changes" : "Create"}
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
