"use client";

import InlineLink from "@components/InlineLink";
import { notify } from "@components/Notification";
import { useExpirationState } from "@hooks/useExpirationState";
import { convertToSeconds } from "@hooks/useTimeFormatter";
import * as Tabs from "@radix-ui/react-tabs";
import { useApiCall } from "@utils/api";
import { CalendarClock, ExternalLinkIcon } from "lucide-react";
import React, { useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import {
  OzSelect,
  OzSelectContent,
  OzSelectItem,
  OzSelectTrigger,
  OzSelectValue,
} from "@/components/v2/OzSelect";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { useHasChanges } from "@/hooks/useHasChanges";
import { Account } from "@/interfaces/Account";
import OzSettingsCard from "@/modules/settings/v2/OzSettingsCard";
import OzSettingsField from "@/modules/settings/v2/OzSettingsField";
import OzSettingsToggle from "@/modules/settings/v2/OzSettingsToggle";

// AuthenticationTab — settings sub-page body for /settings/authentication.
// Functionality preserved verbatim from the pre-phase-5 legacy:
// `useExpirationState` for peer login + inactivity expiration, peer
// approval boolean, save flow through /accounts/{id}. Only the paint
// changes — sections move into OzSettingsCard blocks; toggles into
// OzSettingsToggle rows; the session-expiration form into an
// OzSettingsField row inside the expansion area. Input + Select stay
// on legacy paint pending dedicated v2 form primitives.

type Props = {
  account: Account;
};

export default function AuthenticationTab({ account }: Readonly<Props>) {
  const { permission } = usePermissions();
  const { mutate } = useSWRConfig();

  const [peerApproval, setPeerApproval] = useState<boolean>(() => {
    try {
      return account?.settings?.extra?.peer_approval_enabled || false;
    } catch (error) {
      return false;
    }
  });

  // openZro-side TOTP enforcement toggles (issue #31). Both default
  // OFF. Local = primary second factor for Dex staticPasswords; the
  // bundled local connector has no built-in MFA. Federated = optional
  // redundancy on top of the IdP's own MFA — users enter two codes
  // per login when this is on.
  const [mfaEnforceLocal, setMfaEnforceLocal] = useState<boolean>(
    () => account?.settings?.mfa_enforce_local ?? false,
  );
  const [mfaEnforceFederated, setMfaEnforceFederated] = useState<boolean>(
    () => account?.settings?.mfa_enforce_federated ?? false,
  );

  const [
    loginExpiration,
    setLoginExpiration,
    expiresIn,
    setExpiresIn,
    expireInterval,
    setExpireInterval,
  ] = useExpirationState({
    enabled: account.settings.peer_login_expiration_enabled,
    expirationInSeconds: account.settings.peer_login_expiration || 86400,
  });

  const [
    peerInactivityExpirationEnabled,
    setPeerInactivityExpirationEnabled,
    peerInactivityExpiresIn,
    setPeerInactivityExpiresIn,
    peerInactivityExpireInterval,
    setPeerInactivityExpireInterval,
  ] = useExpirationState({
    enabled: account.settings.peer_inactivity_expiration_enabled,
    expirationInSeconds: account.settings.peer_inactivity_expiration || 600,
    timeRange: ["minutes", "hours", "days"],
  });

  const saveRequest = useApiCall<Account>("/accounts/" + account.id);

  const { hasChanges, updateRef } = useHasChanges([
    peerApproval,
    loginExpiration,
    expiresIn,
    expireInterval,
    peerInactivityExpirationEnabled,
    peerInactivityExpiresIn,
    peerInactivityExpireInterval,
    mfaEnforceLocal,
    mfaEnforceFederated,
  ]);

  const saveChanges = async () => {
    const expiration = convertToSeconds(expiresIn, expireInterval);

    notify({
      title: "Save Authentication Settings",
      description: "Authentication settings successfully saved.",
      promise: saveRequest
        .put({
          id: account.id,
          settings: {
            ...account.settings,
            peer_login_expiration_enabled: loginExpiration,
            peer_login_expiration: loginExpiration ? expiration : 86400,
            peer_inactivity_expiration_enabled: loginExpiration
              ? peerInactivityExpirationEnabled
              : false,
            peer_inactivity_expiration: 600,
            mfa_enforce_local: mfaEnforceLocal,
            mfa_enforce_federated: mfaEnforceFederated,
            extra: {
              ...account.settings?.extra,
              peer_approval_enabled: peerApproval,
            },
          },
        } as Account)
        .then(() => {
          mutate("/accounts");
          updateRef([
            peerApproval,
            loginExpiration,
            expiresIn,
            expireInterval,
            peerInactivityExpirationEnabled,
            peerInactivityExpiresIn,
            peerInactivityExpireInterval,
            mfaEnforceLocal,
            mfaEnforceFederated,
          ]);
        }),
      loadingMessage: "Saving the authentication settings...",
    });
  };

  const editDisabled = !permission.settings.update;

  return (
    <Tabs.Content value="authentication" className="flex flex-col gap-5">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="text-[18px] font-semibold tracking-tight text-oz2-text">
            Authentication
          </h2>
          <p className="mt-1 max-w-2xl text-[13px] leading-[1.55] text-oz2-text-muted">
            Control how peers and humans prove who they are.{" "}
            <InlineLink
              href="https://docs.openzro.io/how-to/enforce-periodic-user-authentication"
              target="_blank"
            >
              Learn more
              <ExternalLinkIcon size={11} />
            </InlineLink>
          </p>
        </div>
        <OzButton
          variant="primary"
          type="button"
          disabled={!hasChanges || editDisabled}
          onClick={saveChanges}
          data-cy="save-authentication-settings"
        >
          Save Changes
        </OzButton>
      </header>

      <OzSettingsCard
        title="Peer approval"
        sub="Hold every new peer until an administrator admits it from the Peers list. Existing peers are unaffected."
      >
        <OzSettingsToggle
          value={peerApproval}
          onChange={setPeerApproval}
          disabled={editDisabled}
          dataCy="peer-approval-toggle"
          label="Require peer approval"
          desc={
            <>
              Use this for regulated environments where the admission audit
              trail matters; combine with{" "}
              <InlineLink href="/settings/device-admission">
                Device Admission
              </InlineLink>{" "}
              to gate by posture (MDM/EDR) instead of/in addition to manual
              review.
            </>
          }
        />
      </OzSettingsCard>

      <OzSettingsCard
        title="Peer session expiration"
        sub="Force peers registered with SSO to periodically re-authenticate, so a stolen device's access stops at the next interval."
      >
        <OzSettingsToggle
          value={loginExpiration}
          onChange={(state) => {
            setLoginExpiration(state);
            if (!state) setPeerInactivityExpirationEnabled(false);
          }}
          disabled={editDisabled}
          dataCy="peer-login-expiration"
          label="Peer session expiration"
          desc="Request periodic re-authentication of peers registered with SSO."
        />

        {loginExpiration && (
          <div className="flex flex-col gap-5 rounded-oz2-card border border-oz2-border-soft bg-oz2-bg-sunken p-4">
            <OzSettingsField
              label="Session expiration"
              hint="Time after which every peer added with SSO login will require re-authentication."
            >
              <div className="flex gap-3">
                <OzInput
                  placeholder="7"
                  min={1}
                  max={180}
                  value={expiresIn}
                  type="number"
                  disabled={editDisabled}
                  data-cy="peer-login-expiration-input"
                  onChange={(e) => setExpiresIn(e.target.value)}
                  wrapperClassName="w-[120px]"
                />
                <OzSelect
                  disabled={editDisabled}
                  value={expireInterval}
                  onValueChange={(v) => setExpireInterval(v)}
                >
                  <OzSelectTrigger
                    className="w-full"
                    data-cy="peer-login-expiration-select"
                  >
                    <div className="flex items-center gap-2">
                      <CalendarClock
                        size={14}
                        className="text-oz2-text-faint"
                      />
                      <OzSelectValue
                        placeholder="Select interval..."
                        data-cy="peer-login-expiration-select-value"
                      />
                    </div>
                  </OzSelectTrigger>
                  <OzSelectContent data-cy="peer-login-expiration-select-content">
                    <OzSelectItem value="days">Days</OzSelectItem>
                    <OzSelectItem value="hours">Hours</OzSelectItem>
                  </OzSelectContent>
                </OzSelect>
              </div>
            </OzSettingsField>

            <OzSettingsToggle
              value={peerInactivityExpirationEnabled}
              onChange={setPeerInactivityExpirationEnabled}
              disabled={editDisabled}
              dataCy="peer-inactivity-expiration"
              label="Require login after disconnect"
              desc="Force re-authentication when a peer disconnects from management for more than 10 minutes."
              nested
            />
          </div>
        )}
      </OzSettingsCard>

      <OzSettingsCard
        title="Two-factor authentication (TOTP)"
        sub="openZro-side TOTP applied at the management session. Users
        enrol via Profile → Security; an authenticator app
        (Google Authenticator, Authy, 1Password, Bitwarden) generates
        the 6-digit code."
      >
        <OzSettingsToggle
          value={mfaEnforceLocal}
          onChange={setMfaEnforceLocal}
          disabled={editDisabled}
          dataCy="mfa-enforce-local"
          label="Require 2FA for local accounts"
          desc={
            <>
              Bundled Dex <code>staticPasswords</code> has no built-in
              MFA — this is the <strong>primary</strong> second factor
              for teams running openZro without an external IdP. When
              on, local users without TOTP are forced into the
              enrollment flow on their next session check.
            </>
          }
        />
        <OzSettingsToggle
          value={mfaEnforceFederated}
          onChange={setMfaEnforceFederated}
          disabled={editDisabled}
          dataCy="mfa-enforce-federated"
          label="Require 2FA for federated accounts (redundancy)"
          desc={
            <>
              On top of your IdP&apos;s own MFA. Useful when the threat
              model treats the IdP as a separate trust domain
              (compromised IdP key, social engineering, IdP
              misconfiguration). Users enter two codes per login — one
              at the IdP, one at openZro. Leave off if you trust your
              IdP&apos;s MFA fully.
            </>
          }
        />
      </OzSettingsCard>
    </Tabs.Content>
  );
}
