import { useOidc } from "@axa-fr/react-oidc";
import Button from "@components/Button";
import Paragraph from "@components/Paragraph";
import loadConfig from "@utils/config";
import { clearMfaSessionToken } from "@utils/mfaSession";
import { LogIn } from "lucide-react";
import * as React from "react";
import { useEffect, useRef } from "react";
import OpenzroIcon from "@/assets/icons/OpenzroIcon";

const config = loadConfig();

// Rendered by @axa-fr/react-oidc when refresh/sync fails — i.e. the
// stored token can no longer be validated against the issuer (Dex
// restart in dev, signing key rotation, expired refresh token, etc.).
// We trigger logout() on mount so the lib clears its session storage
// and bounces the user back to the IdP's login page; the visible UI
// is a one-frame fallback in case the redirect takes a moment, with
// a manual button if it doesn't fire (e.g. logout endpoint unreachable).
export const SessionLost = () => {
  const { logout } = useOidc();
  const triggered = useRef(false);

  useEffect(() => {
    if (triggered.current) return;
    triggered.current = true;
    // Clear the stale X-MFA-Token so the re-login flow forces a
    // fresh TOTP challenge — without this the token in sessionStorage
    // would silently re-elevate the next session (see logout() in
    // UsersProvider.tsx for the full rationale).
    clearMfaSessionToken();
    // Empty post-logout target = the dashboard origin. After Dex
    // logs the user out, the dashboard root re-arms the OIDC flow
    // and pushes them through the login form again.
    void logout("", { client_id: config.clientId });
  }, [logout]);

  return (
    <div
      className={
        "flex items-center justify-center flex-col h-screen max-w-md mx-auto"
      }
    >
      <div
        className={
          "bg-nb-gray-930 mb-3 border border-nb-gray-900 h-10 w-10 rounded-md flex items-center justify-center "
        }
      >
        <OpenzroIcon size={20} />
      </div>
      <h1>Session Expired</h1>
      <Paragraph className={"text-center"}>
        Your session is no longer active. Redirecting you to sign in again…
      </Paragraph>
      <Button
        variant={"primary"}
        size={"sm"}
        className={"mt-5"}
        onClick={() => logout("", { client_id: config.clientId })}
      >
        Login
        <LogIn size={16} />
      </Button>
    </div>
  );
};
