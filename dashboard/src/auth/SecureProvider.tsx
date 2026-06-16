import { OidcSecure, useOidc } from "@axa-fr/react-oidc";
import { usePathname } from "next/navigation";
import * as React from "react";
import { useEffect } from "react";

type Props = {
  children: React.ReactNode;
};

// safeCallbackPath collapses the OIDC callback routes (/auth,
// /silent-auth) down to "/" so the post-login redirect never points
// back at the route that just consumed the authorization code. Left
// untreated, a failed token exchange (expired code, state mismatch
// from a previous tab, etc.) lands the user on /auth?code=...&state=...,
// SecureProvider then calls login(currentPath) → Dex → redirect_uri
// /auth?code=...&state=... → React re-mounts on the SAME path → loop.
// Any non-callback path passes through unchanged so a user who hit
// /peers, got bounced to login, and authed successfully still lands
// back on /peers.
const safeCallbackPath = (path: string | null): string => {
  if (!path) return "/";
  if (path === "/auth" || path.startsWith("/auth/")) return "/";
  if (path === "/silent-auth" || path.startsWith("/silent-auth/")) return "/";
  return path;
};

export const SecureProvider = ({ children }: Props) => {
  const { isAuthenticated, login } = useOidc();
  const currentPath = usePathname();
  const callbackPath = safeCallbackPath(currentPath);

  useEffect(() => {
    let timeout: NodeJS.Timeout | undefined = undefined;
    if (!isAuthenticated) {
      timeout = setTimeout(async () => {
        if (!isAuthenticated) {
          await login(callbackPath);
        }
      }, 1500);
    }
    return () => {
      clearTimeout(timeout);
    };
  }, [callbackPath, isAuthenticated, login]);

  return (
    <>
      <OidcSecure callbackPath={callbackPath}>{children}</OidcSecure>
    </>
  );
};
