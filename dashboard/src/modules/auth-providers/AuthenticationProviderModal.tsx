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
import loadConfig from "@utils/config";
import { ShieldIcon } from "lucide-react";
import React, { useEffect, useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import {
  AuthenticationProvider,
  AuthenticationProviderInput,
  CONNECTOR_TYPES,
  ConnectorType,
  defaultRedirectURI,
  dexConnectorType,
  inferConnectorType,
  issuerPlaceholder,
} from "@/interfaces/AuthenticationProvider";

const dashboardConfig = loadConfig();

type Props = {
  open: boolean;
  setOpen: (open: boolean) => void;
  existing?: AuthenticationProvider | null;
};

// One source of truth: the dashboard form is just a flat key/value
// view, with the type dropdown selecting which fields are required.
// The rendered config object is composed at submit time.
export default function AuthenticationProviderModal({
  open,
  setOpen,
  existing,
}: Readonly<Props>) {
  const isEdit = !!existing;
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const [type, setType] = useState<ConnectorType>("oidc");
  const [clientID, setClientID] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [issuer, setIssuer] = useState("");
  const [tenant, setTenant] = useState("");
  const [redirectURI, setRedirectURI] = useState("");
  const [saving, setSaving] = useState(false);

  const { mutate } = useSWRConfig();
  const apiCreate = useApiCall<AuthenticationProvider>("/admin/auth-providers");
  const apiUpdate = useApiCall<AuthenticationProvider>(
    `/admin/auth-providers/${existing?.id ?? ""}`,
  );

  const meta = useMemo(
    () => CONNECTOR_TYPES.find((t) => t.value === type) ?? CONNECTOR_TYPES[0],
    [type],
  );

  useEffect(() => {
    if (!open) return;
    const fallbackRedirect = defaultRedirectURI(dashboardConfig.authority);
    if (existing) {
      setId(existing.id);
      setName(existing.name);
      // existing.type may be a Dex type we don't model (saml/ldap):
      // we still surface it but the form's per-type fields will
      // be limited to the OAuth-style ones below. For type=oidc we
      // also sniff the issuer URL to label Keycloak / Okta.
      const cfg = (existing.config as Record<string, unknown>) ?? {};
      const inferred = inferConnectorType(existing.type, cfg);
      setType((inferred as ConnectorType) ?? "oidc");
      setClientID(typeof cfg.clientID === "string" ? cfg.clientID : "");
      // Secrets never come back on the wire (Dex returns them
      // but the management layer could redact in the future).
      // Leave blank so a save without re-typing keeps the
      // existing secret on Dex's side.
      setClientSecret("");
      setIssuer(typeof cfg.issuer === "string" ? cfg.issuer : "");
      setTenant(typeof cfg.tenant === "string" ? cfg.tenant : "");
      setRedirectURI(
        typeof cfg.redirectURI === "string" ? cfg.redirectURI : fallbackRedirect,
      );
    } else {
      setId("");
      setName("");
      setType("oidc");
      setClientID("");
      setClientSecret("");
      setIssuer("");
      setTenant("");
      setRedirectURI(fallbackRedirect);
    }
  }, [open, existing]);

  const isOIDC = type === "oidc" || type === "keycloak" || type === "okta";

  const buildConfig = (): Record<string, unknown> => {
    const base: Record<string, unknown> = {
      clientID,
      clientSecret,
      redirectURI,
    };
    if (isOIDC) base.issuer = issuer;
    if (type === "microsoft" && tenant) base.tenant = tenant;
    return base;
  };

  const buildInput = (): AuthenticationProviderInput => ({
    id: id.trim(),
    // Keycloak / Okta are UI-only labels; Dex stores them as `oidc`.
    type: dexConnectorType(type),
    name: name.trim(),
    config: buildConfig(),
  });

  const validate = (): string | null => {
    if (!id.trim()) return "ID is required";
    if (!/^[a-z0-9][a-z0-9-_]*$/.test(id.trim()))
      return "ID must be lowercase letters/digits/hyphens (used in /dex/auth/<id> URL)";
    if (!name.trim()) return "Name is required";
    if (!clientID.trim()) return "Client ID is required";
    if (!isEdit && !clientSecret) return "Client Secret is required on create";
    if (isOIDC && !issuer.trim()) return "Issuer URL is required";
    if (!redirectURI.trim()) return "Redirect URI is required";
    return null;
  };

  const save = async () => {
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
      await mutate("/admin/auth-providers");
      setOpen(false);
      notify({
        title: isEdit ? "Provider updated" : "Provider added",
        description: `Authentication provider "${name}" saved. Visible at /dex/auth on the next page load.`,
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal open={open} onOpenChange={setOpen}>
      <ModalContent maxWidthClass="max-w-2xl">
        <ModalHeader
          icon={<ShieldIcon size={20} />}
          title={
            isEdit
              ? "Edit authentication provider"
              : "Add authentication provider"
          }
          description={
            isEdit
              ? "Update connection details. Leave the client secret blank to keep the current value stored in Dex."
              : "Connect an upstream identity provider. Dex (https://dexidp.io) handles the federation; this form proxies into Dex's gRPC API."
          }
          truncate
        />

        <div className="px-8 pt-3 pb-6 grid gap-4">
          <div>
            <Label>ID</Label>
            <Input
              value={id}
              onChange={(e) => setId(e.target.value)}
              placeholder="acme-google"
              disabled={isEdit}
            />
            <Paragraph className="text-xs text-nb-gray-300 mt-1">
              URL-safe identifier used in <code>/dex/auth/{"{"}id{"}"}</code>.
              Cannot be changed after create.
            </Paragraph>
          </div>

          <div>
            <Label>Type</Label>
            <select
              value={type}
              disabled={isEdit}
              onChange={(e) => setType(e.target.value as ConnectorType)}
              className="w-full rounded-md border border-nb-gray-700 bg-nb-gray-940 px-3 py-2 text-sm"
            >
              {CONNECTOR_TYPES.map((p) => (
                <option key={p.value} value={p.value}>
                  {p.label}
                </option>
              ))}
            </select>
            <Paragraph className="text-xs text-nb-gray-300 mt-1">
              {meta.description}
            </Paragraph>
          </div>

          <div>
            <Label>Display name</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Acme Google"
            />
            <Paragraph className="text-xs text-nb-gray-300 mt-1">
              Shown on the Dex login page as &quot;Log in with{" "}
              {name || meta.label}&quot;.
            </Paragraph>
          </div>

          {isOIDC && (
            <div>
              <Label>Issuer URL</Label>
              <Input
                value={issuer}
                onChange={(e) => setIssuer(e.target.value)}
                placeholder={issuerPlaceholder(type)}
              />
            </div>
          )}

          {type === "microsoft" && (
            <div>
              <Label>Tenant (optional)</Label>
              <Input
                value={tenant}
                onChange={(e) => setTenant(e.target.value)}
                placeholder="common, organizations, or a specific tenant ID"
              />
            </div>
          )}

          <div className="grid gap-4 sm:grid-cols-2">
            <div>
              <Label>Client ID</Label>
              <Input
                value={clientID}
                onChange={(e) => setClientID(e.target.value)}
              />
            </div>
            <div>
              <Label>Client Secret</Label>
              <Input
                type="password"
                value={clientSecret}
                onChange={(e) => setClientSecret(e.target.value)}
                placeholder={isEdit ? "(unchanged)" : ""}
              />
            </div>
          </div>

          <div>
            <Label>Redirect URI</Label>
            <Input
              value={redirectURI}
              onChange={(e) => setRedirectURI(e.target.value)}
            />
            <Paragraph className="text-xs text-nb-gray-300 mt-1">
              This is Dex&apos;s callback endpoint, not the
              dashboard&apos;s — Dex receives the OIDC response from
              your IdP, then forwards a session token to the
              dashboard. Whitelist this exact URL in your IdP&apos;s
              app config (e.g. Keycloak: Clients →{" "}
              <em>your-client</em> → Valid redirect URIs). Defaults to{" "}
              <code>{defaultRedirectURI(dashboardConfig.authority)}</code>.
            </Paragraph>
          </div>
        </div>

        <ModalFooter>
          <ModalClose asChild>
            <Button variant="secondary" disabled={saving}>
              Cancel
            </Button>
          </ModalClose>
          <Button variant="primary" onClick={save} disabled={saving}>
            {saving ? "Saving…" : isEdit ? "Save changes" : "Add provider"}
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
