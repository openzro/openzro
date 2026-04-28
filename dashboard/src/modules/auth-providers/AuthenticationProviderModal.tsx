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
import { ShieldIcon } from "lucide-react";
import React, { useEffect, useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import {
  AuthenticationProvider,
  AuthenticationProviderInput,
  AuthenticationProviderType,
  PROVIDER_TYPES,
  providerTypeMeta,
} from "@/interfaces/AuthenticationProvider";

type Props = {
  open: boolean;
  setOpen: (open: boolean) => void;
  existing?: AuthenticationProvider | null;
};

export default function AuthenticationProviderModal({
  open,
  setOpen,
  existing,
}: Readonly<Props>) {
  const isEdit = !!existing;
  const [name, setName] = useState("");
  const [type, setType] = useState<AuthenticationProviderType>("oidc-generic");
  const [issuerURL, setIssuerURL] = useState("");
  const [clientID, setClientID] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [scopesRaw, setScopesRaw] = useState("");
  const [brandLabel, setBrandLabel] = useState("");
  const [brandLogoURL, setBrandLogoURL] = useState("");
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [authEndpoint, setAuthEndpoint] = useState("");
  const [tokenEndpoint, setTokenEndpoint] = useState("");
  const [userInfoEndpoint, setUserInfoEndpoint] = useState("");
  const [jwksURL, setJwksURL] = useState("");
  const [saving, setSaving] = useState(false);

  const { mutate } = useSWRConfig();
  const apiCreate = useApiCall<AuthenticationProvider>("/admin/auth-providers");
  const apiUpdate = useApiCall<AuthenticationProvider>(
    `/admin/auth-providers/${existing?.id ?? 0}`,
  );

  const meta = useMemo(() => providerTypeMeta(type), [type]);

  // Reset / hydrate the form when the modal opens. On open with
  // `existing`, fill from the public projection (client_secret is
  // never on the wire). On open without `existing`, reset to a
  // fresh "Add provider" state with per-type defaults.
  useEffect(() => {
    if (!open) return;
    if (existing) {
      setName(existing.name);
      setType(existing.type);
      setIssuerURL(existing.config?.issuer_url ?? "");
      setClientID(existing.config?.client_id ?? "");
      setClientSecret("");
      setScopesRaw((existing.config?.scopes ?? []).join(" "));
      setBrandLabel(existing.brand_label ?? "");
      setBrandLogoURL(existing.brand_logo_url ?? "");
      setAuthEndpoint(existing.config?.authorization_endpoint ?? "");
      setTokenEndpoint(existing.config?.token_endpoint ?? "");
      setUserInfoEndpoint(existing.config?.userinfo_endpoint ?? "");
      setJwksURL(existing.config?.jwks_url ?? "");
      setShowAdvanced(
        Boolean(
          existing.config?.authorization_endpoint ||
            existing.config?.token_endpoint ||
            existing.config?.userinfo_endpoint ||
            existing.config?.jwks_url,
        ),
      );
    } else {
      setName("");
      setType("oidc-generic");
      setIssuerURL("");
      setClientID("");
      setClientSecret("");
      setScopesRaw(PROVIDER_TYPES[0].defaultScopes.join(" "));
      setBrandLabel("");
      setBrandLogoURL("");
      setShowAdvanced(false);
      setAuthEndpoint("");
      setTokenEndpoint("");
      setUserInfoEndpoint("");
      setJwksURL("");
    }
  }, [open, existing]);

  // When the operator picks a new type on the create path, swap
  // the scopes default to the type's preferred set. Edit path
  // doesn't touch scopes — the operator's stored values win.
  useEffect(() => {
    if (existing) return;
    setScopesRaw(meta.defaultScopes.join(" "));
  }, [type, existing, meta.defaultScopes]);

  const buildInput = (): AuthenticationProviderInput => {
    const scopes = scopesRaw.split(/\s+/).filter(Boolean);
    return {
      name: name.trim(),
      type,
      enabled: existing?.enabled ?? true,
      brand_label: brandLabel.trim() || undefined,
      brand_logo_url: brandLogoURL.trim() || undefined,
      config: {
        issuer_url: issuerURL.trim(),
        client_id: clientID.trim(),
        client_secret: clientSecret || undefined,
        scopes: scopes.length > 0 ? scopes : undefined,
        authorization_endpoint: authEndpoint.trim() || undefined,
        token_endpoint: tokenEndpoint.trim() || undefined,
        userinfo_endpoint: userInfoEndpoint.trim() || undefined,
        jwks_url: jwksURL.trim() || undefined,
      },
    };
  };

  const validate = (): string | null => {
    if (!name.trim()) return "Name is required";
    if (!issuerURL.trim()) return "Issuer URL is required";
    if (!clientID.trim()) return "Client ID is required";
    if (!isEdit && !clientSecret) return "Client Secret is required on create";
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
        description: `Authentication provider "${name}" saved.`,
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
              ? "Update connection details. Leave the client secret blank to keep the current value."
              : "Connect an OIDC identity provider so users can sign in to openZro through it."
          }
          truncate
        />

        <div className="px-8 pt-3 pb-6 grid gap-4">
          <div>
            <Label>Name</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="prod-zitadel"
            />
            <Paragraph className="text-xs text-nb-gray-300 mt-1">
              Internal label. Operators see this in the Authentication
              Providers list.
            </Paragraph>
          </div>

          <div>
            <Label>Type</Label>
            <select
              value={type}
              disabled={isEdit}
              onChange={(e) =>
                setType(e.target.value as AuthenticationProviderType)
              }
              className="w-full rounded-md border border-nb-gray-700 bg-nb-gray-940 px-3 py-2 text-sm"
            >
              {PROVIDER_TYPES.map((p) => (
                <option key={p.value} value={p.value}>
                  {p.label}
                </option>
              ))}
            </select>
            <Paragraph className="text-xs text-nb-gray-300 mt-1">
              {meta.description}
            </Paragraph>
            {meta.experimental && (
              <Paragraph className="text-xs text-orange-400 mt-1">
                Experimental: this provider type is not yet end-to-end
                supported by the callback flow. Sign-in will fail with
                501 until the userinfo path lands.
              </Paragraph>
            )}
          </div>

          <div>
            <Label>Issuer URL</Label>
            <Input
              value={issuerURL}
              onChange={(e) => setIssuerURL(e.target.value)}
              placeholder={meta.issuerPlaceholder}
            />
          </div>

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
            <Label>Scopes</Label>
            <Input
              value={scopesRaw}
              onChange={(e) => setScopesRaw(e.target.value)}
              placeholder={meta.defaultScopes.join(" ")}
            />
            <Paragraph className="text-xs text-nb-gray-300 mt-1">
              Space-separated. Defaults to {meta.defaultScopes.join(", ")}.
            </Paragraph>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div>
              <Label>Brand label (optional)</Label>
              <Input
                value={brandLabel}
                onChange={(e) => setBrandLabel(e.target.value)}
                placeholder={meta.label}
              />
              <Paragraph className="text-xs text-nb-gray-300 mt-1">
                Shown on the &quot;Sign in with …&quot; button.
              </Paragraph>
            </div>
            <div>
              <Label>Logo URL (optional)</Label>
              <Input
                value={brandLogoURL}
                onChange={(e) => setBrandLogoURL(e.target.value)}
                placeholder="https://…/logo.svg"
              />
            </div>
          </div>

          <button
            type="button"
            onClick={() => setShowAdvanced(!showAdvanced)}
            className="text-xs text-nb-gray-300 hover:text-white text-left"
          >
            {showAdvanced ? "▾" : "▸"} Advanced — explicit endpoints
          </button>

          {showAdvanced && (
            <div className="grid gap-4 border-l-2 border-nb-gray-800 pl-4">
              <Paragraph className="text-xs text-nb-gray-300">
                Override these only if your provider does not publish a
                discovery document at <code>/.well-known/openid-configuration</code>{" "}
                (legacy IdPs, GitHub OAuth). Leave blank for standards-
                compliant OIDC.
              </Paragraph>
              <div>
                <Label>Authorization endpoint</Label>
                <Input
                  value={authEndpoint}
                  onChange={(e) => setAuthEndpoint(e.target.value)}
                  placeholder="https://idp.example.com/oauth/authorize"
                />
              </div>
              <div>
                <Label>Token endpoint</Label>
                <Input
                  value={tokenEndpoint}
                  onChange={(e) => setTokenEndpoint(e.target.value)}
                  placeholder="https://idp.example.com/oauth/token"
                />
              </div>
              <div>
                <Label>Userinfo endpoint (optional)</Label>
                <Input
                  value={userInfoEndpoint}
                  onChange={(e) => setUserInfoEndpoint(e.target.value)}
                />
              </div>
              <div>
                <Label>JWKs URL (optional)</Label>
                <Input
                  value={jwksURL}
                  onChange={(e) => setJwksURL(e.target.value)}
                />
              </div>
            </div>
          )}
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
