import { OidcSecure, useOidc } from "@axa-fr/react-oidc";
import loadConfig from "@utils/config";
import { usePathname } from "next/navigation";
import * as React from "react";
import { useEffect, useMemo } from "react";

type Props = {
  children: React.ReactNode;
};

const config = loadConfig();

// callbackPathBase strips any query/hash and a leading "/#" so a
// configured redirect like "/#callback" or "/auth?foo=bar" still
// matches the bare "/auth" route comparisons we get from
// usePathname(). Returns null when the input cannot be normalised
// (empty / non-path), so callers can decide whether to keep the
// original.
const callbackPathBase = (raw: string | undefined): string | null => {
  if (!raw) return null;
  let s = raw.startsWith("/#") ? raw.slice(2) : raw;
  if (!s.startsWith("/")) s = "/" + s;
  const stop = Math.min(
    ...["?", "#"]
      .map((c) => s.indexOf(c))
      .filter((i) => i !== -1)
      .concat(s.length),
  );
  const base = s.slice(0, stop);
  return base === "/" || base === "" ? null : base;
};

// blockedCallbackPaths returns the set of routes (no query, no hash)
// that we MUST NOT use as the post-login destination: the OIDC
// callback itself and the silent-renew callback. Derived from
// config.redirectURI / config.silentRedirectURI so deployments that
// customise these (e.g. `/nb-auth`, the Zitadel quickstart, or the
// legacy hash-style `/#callback`) get the same loop-prevention as
// the default `/auth`.
const blockedCallbackPaths = (): Set<string> => {
  const set = new Set<string>();
  for (const candidate of [config.redirectURI, config.silentRedirectURI]) {
    const base = callbackPathBase(candidate);
    if (base) set.add(base);
  }
  return set;
};

// safeCallbackPath collapses any callback route down to "/" so the
// post-login redirect never points back at the route that just
// consumed the authorization code. Left untreated, a failed token
// exchange (expired Dex code, state mismatch from a previous tab,
// etc.) lands the user on /<callback>?code=…&state=…, SecureProvider
// then calls login(currentPath) → Dex → redirect_uri /<callback> →
// React re-mounts on the SAME path → loop. Non-callback paths pass
// through so a user who hit /peers, got bounced to login, and authed
// successfully still lands back on /peers.
const safeCallbackPath = (
  path: string | null,
  blocked: Set<string>,
): string => {
  if (!path) return "/";
  if (blocked.has(path)) return "/";
  for (const base of blocked) {
    if (path === base || path.startsWith(base + "/")) return "/";
  }
  return path;
};

export const SecureProvider = ({ children }: Props) => {
  const { isAuthenticated, login } = useOidc();
  const currentPath = usePathname();
  const blocked = useMemo(blockedCallbackPaths, []);
  const callbackPath = safeCallbackPath(currentPath, blocked);

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
