import {
  useOidc,
  useOidcAccessToken,
  useOidcIdToken,
} from "@axa-fr/react-oidc";
import loadConfig from "@utils/config";
import {
  clearMfaSessionToken,
  getMfaSessionToken,
  setMfaRedirectToken,
} from "@utils/mfaSession";
import { usePathname } from "next/navigation";
import { isExpired } from "react-jwt";
import useSWR from "swr";
import { useApplicationContext } from "@/contexts/ApplicationProvider";
import { useErrorBoundary } from "@/contexts/ErrorBoundary";

type Method = "GET" | "POST" | "PUT" | "DELETE";

export type ErrorResponse = {
  code: number;
  message: string;
};

const config = loadConfig();

type RequestOptions = {
  key?: string;
  signal?: AbortSignal;
  origin?: string;
  globalParams?: Params;
  ignoreGlobalParams?: boolean;
  blob?: boolean;
  shouldRetryOnError?: boolean;
};

export type Params = Record<string, string | number | boolean>;

// Module-scoped lock so parallel 403s from a single login (users,
// groups, peers, settings, …) can only trigger ONE MFA navigation —
// see the comment at the assign() call site in apiRequest.
let mfaRedirectInFlight = false;

async function apiRequest<T>(
  oidcFetch: (input: RequestInfo, init?: RequestInit) => Promise<Response>,
  method: Method,
  url: string,
  data?: any,
  options?: RequestOptions,
) {
  const origin = options?.origin ? options?.origin : config.apiOrigin + "/api";
  let newUrl = mergeUrlParams(
    url,
    options?.ignoreGlobalParams ? undefined : options?.globalParams,
  );

  const res = await oidcFetch(`${origin}${newUrl}`, {
    method,
    body: JSON.stringify(data),
    signal: options?.signal,
  });

  try {
    if (!res.ok) {
      const body = (await res.json()) as ErrorResponse & {
        mfa_required?: boolean;
        mfa_enrollment_required?: boolean;
        token?: string;
      };
      // Issue #31: the middleware emits 403 + { mfa_required |
      // mfa_enrollment_required, token } to redirect the user
      // through the MFA challenge / enrollment flow. Stash the
      // one-shot token in sessionStorage and bounce the browser to
      // the corresponding public page. The originating request is
      // dropped (the caller's .catch will fire) — after the user
      // completes MFA, the dashboard reloads naturally and the next
      // request rides the X-MFA-Token header.
      //
      // A fresh login fires several useFetchApi calls in parallel
      // (users, groups, peers, settings, …); each lands a 403 with
      // an enroll/challenge token, and without the in-flight guard
      // every response would race window.location.assign, causing
      // visible flicker as the browser starts then cancels
      // navigations. The module-level flag clamps to a single
      // assign per page lifecycle; the full reload that follows
      // resets it naturally. The pathname check covers the case
      // where a 403 arrives while the user is already on the
      // destination page (e.g. polling on /mfa/challenge itself).
      if (res.status === 403 && body?.token) {
        if (body.mfa_required || body.mfa_enrollment_required) {
          setMfaRedirectToken(body.token);
          if (typeof window !== "undefined") {
            const dest = body.mfa_enrollment_required
              ? "/mfa/enroll"
              : "/mfa/challenge";
            const onDest = window.location.pathname === dest;
            if (!mfaRedirectInFlight && !onDest) {
              mfaRedirectInFlight = true;
              // Mask the current page synchronously so parallel 403s
              // that resolve between this assign() and the browser
              // actually starting the navigation cannot drive
              // visible re-renders (every other SWR fetch on the
              // page also lands a 403 right now). Cleared naturally
              // by the full page load that follows.
              if (document.body) {
                document.body.style.visibility = "hidden";
              }
              window.location.assign(dest);
            }
          }
          return Promise.reject({
            code: 403,
            message: body.mfa_required ? "mfa challenge required" : "mfa enrollment required",
          } as ErrorResponse);
        }
        // A handler-level 403 + mfa_required (no fresh challenge
        // token — the sensitive-op gate at the handler) means the
        // user's mfa_session_token is stale; clear it so the next
        // gated request gets a proper challenge_token from the
        // middleware.
        if ((body as { mfa_required?: boolean }).mfa_required) {
          clearMfaSessionToken();
        }
      }
      return Promise.reject(body as ErrorResponse);
    }
    if (options?.blob) return (await res.blob()) as T;
    return (await res.json()) as T;
  } catch (e) {
    if (!res.ok) {
      const error = {
        code: res.status,
        message: res.statusText,
      } as ErrorResponse;
      return Promise.reject(error);
    }
    return res;
  }
}

// SW_TOKEN_PLACEHOLDER_PREFIX is the literal prefix axa-fr/react-oidc
// returns from `useOidcAccessToken()` when the Service Worker is
// active — the real JWT lives inside the SW scope and is never
// handed to page JS. The full string follows the shape
// `ACCESS_TOKEN_SECURED_BY_OIDC_SERVICE_WORKER_<configurationName>`.
// `isExpired()` from `react-jwt` cannot decode this synthetic value
// and always returns true, which would 401-loop every request — so
// expiry checks must consult the lib-parsed claims (`accessTokenPayload.exp`)
// instead, and the manual Bearer header we attach gets silently
// overwritten by the SW on the wire (last-write-wins).
const SW_TOKEN_PLACEHOLDER_PREFIX = "ACCESS_TOKEN_SECURED_BY_OIDC_SERVICE_WORKER";

export function useOpenzroFetch(ignoreError: boolean = false): {
  fetch: (input: RequestInfo, init?: RequestInit) => Promise<Response>;
  token: string | undefined;
} {
  const tokenSource = config.tokenSource || "accessToken";
  const { idToken, idTokenPayload } = useOidcIdToken();
  const { accessToken, accessTokenPayload } = useOidcAccessToken();
  const token = tokenSource.toLowerCase() == "idtoken" ? idToken : accessToken;
  const payload =
    tokenSource.toLowerCase() == "idtoken" ? idTokenPayload : accessTokenPayload;
  const handleErrors = useApiErrorHandling(ignoreError);

  // Expiry check: prefer the lib's parsed claims (`payload.exp` in
  // seconds since epoch). This works in BOTH modes:
  //   - SW active: `token` is a placeholder we cannot decode, but
  //     `payload` reflects the real token's exp.
  //   - SW inactive (legacy fallback): `token` is the real JWT and
  //     `payload` is the decoded claim set — same result, no
  //     library-internal decoding needed.
  // Treat missing payload (post-logout, pre-login) as expired so the
  // request is refused cleanly via the standard error path.
  const isTokenExpired = (): boolean => {
    if (!payload?.exp) {
      // Belt-and-suspenders: if SW gave us the placeholder but no
      // payload yet (very early in the SW handshake), don't 401 — let
      // the SW handle the auth, the placeholder will be overwritten on
      // the wire. Only payload-less AND non-SW means truly unauth.
      if (typeof token === "string" && token.startsWith(SW_TOKEN_PLACEHOLDER_PREFIX)) {
        return false;
      }
      return token ? isExpired(token) : true;
    }
    return payload.exp * 1000 < Date.now();
  };

  const nativeFetch = async (input: RequestInfo, init?: RequestInit) => {
    if (isTokenExpired()) {
      return handleErrors({
        code: 401,
        message: "token expired",
      } as ErrorResponse);
    }

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      Accept: "application/json",
      // Authorization is attached manually so the legacy non-SW
      // fallback (Safari private mode, etc.) keeps working. With SW
      // active, the SW overwrites this header on the wire for every
      // URL matched by `accessTokenDomains` in
      // dashboard/public/OidcTrustedDomains.js — the manual value is
      // only used when the SW skips the request.
      Authorization: `Bearer ${token}`,
    };
    // X-MFA-Token rides alongside Authorization when present
    // (issue #31): the backend's per-session MFA gate verifies it
    // against the JWT bearer's session id. Absent on first request
    // after login until the user clears the gate.
    const mfaToken = getMfaSessionToken();
    if (mfaToken) {
      headers["X-MFA-Token"] = mfaToken;
    }

    return fetch(input, {
      ...init,
      headers,
    });
  };

  return {
    fetch: nativeFetch as (
      input: RequestInfo,
      init?: RequestInit,
    ) => Promise<Response>,
    token,
  };
}

export default function useFetchApi<T>(
  url: string,
  ignoreError = false,
  revalidate = true,
  allowFetch = true,
  options?: RequestOptions,
) {
  const { fetch, token } = useOpenzroFetch(ignoreError);
  const handleErrors = useApiErrorHandling(ignoreError);
  const { globalApiParams } = useApplicationContext();

  // Gate the SWR key on `token` so the first render right after the
  // OIDC callback — when the OIDC provider is still hydrating and
  // `token` is undefined — does not fire a fetch with `Bearer
  // undefined`. SWR treats a null key as "do not fetch"; when the
  // token arrives in a later render, the key flips to the URL and
  // SWR fires the request automatically. Defense-in-depth alongside
  // the post-login navigation fix in OIDCProvider — neither alone
  // covers every reload path.
  const shouldFetch = allowFetch && !!token;
  const cacheKey = !shouldFetch
    ? null
    : options?.key
      ? [url, options?.key]
      : url;
  const fetchFn = options?.key
    ? async ([url]: [url: string]) => {
        if (!allowFetch) return;
        return apiRequest<T>(fetch, "GET", url, undefined, {
          ...options,
          globalParams: globalApiParams,
        }).catch((err) => handleErrors(err as ErrorResponse));
      }
    : async (url: string) => {
        if (!allowFetch) return;
        return apiRequest<T>(fetch, "GET", url, undefined, {
          ...options,
          globalParams: globalApiParams,
        }).catch((err) => handleErrors(err as ErrorResponse));
      };

  const { data, error, isLoading, isValidating, mutate } = useSWR(
    cacheKey,
    fetchFn,
    {
      keepPreviousData: true,
      revalidateOnFocus: revalidate,
      revalidateIfStale: revalidate,
      revalidateOnReconnect: revalidate,
      shouldRetryOnError: options?.shouldRetryOnError ?? true,
    },
  );

  return {
    data: data as T | undefined,
    error,
    isLoading,
    isValidating,
    mutate,
  } as const;
}

export function useApiCall<T>(
  url: string,
  ignoreError = false,
  requestOptions?: RequestOptions,
) {
  const { fetch } = useOpenzroFetch(ignoreError);
  const handleErrors = useApiErrorHandling(ignoreError);
  const { globalApiParams } = useApplicationContext();

  return {
    post: async (data: any, suffix = "", options?: RequestOptions) => {
      return apiRequest<T>(fetch, "POST", url + suffix, data, {
        ...(options || requestOptions),
        globalParams: globalApiParams,
      })
        .then((res) => Promise.resolve(res as T))
        .catch((err) => handleErrors(err as ErrorResponse)) as Promise<T>;
    },
    put: async (data: any, suffix = "", options?: RequestOptions) => {
      return apiRequest<T>(fetch, "PUT", url + suffix, data, {
        ...(options || requestOptions),
        globalParams: globalApiParams,
      })
        .then((res) => Promise.resolve(res as T))
        .catch((err) => handleErrors(err as ErrorResponse)) as Promise<T>;
    },
    del: async (data: any = "", suffix = "", options?: RequestOptions) => {
      return apiRequest<T>(fetch, "DELETE", url + suffix, data, {
        ...(options || requestOptions),
        globalParams: globalApiParams,
      })
        .then((res) => Promise.resolve(res as T))
        .catch((err) => handleErrors(err as ErrorResponse)) as Promise<T>;
    },
    get: async (suffix = "", options?: RequestOptions) => {
      return apiRequest<T>(fetch, "GET", url + suffix, undefined, {
        ...(options || requestOptions),
        globalParams: globalApiParams,
      })
        .then((res) => Promise.resolve(res as T))
        .catch((err) => handleErrors(err as ErrorResponse)) as Promise<T>;
    },
  };
}

export function useApiErrorHandling(ignoreError = false) {
  const { login } = useOidc();
  const currentPath = usePathname();
  const { setError } = useErrorBoundary();
  if (ignoreError)
    return (err: ErrorResponse) => {
      console.log(err);
      return Promise.reject(err);
    };

  return (err: ErrorResponse) => {
    if (err.code == 401 && err.message == "no valid authentication provided") {
      return login(currentPath);
    }
    if (err.code == 401 && err.message == "token expired") {
      return login(currentPath);
    }
    if (err.code == 401 && err.message == "token invalid") {
      setError(err);
    }
    if (err.code == 500 && err.message == "internal server error") {
      setError(err);
    }
    if (err.code > 400 && err.code <= 500) {
      setError(err);
    }

    return Promise.reject(err);
  };
}

function mergeUrlParams(url: string, params?: Params): string {
  try {
    // Split the URL and query parts
    const [basePath, existingQuery] = url.split("?");

    // Create a search params object with existing query params
    const searchParams = new URLSearchParams(existingQuery || "");

    // Add new params if provided
    if (params && typeof params === "object") {
      Object.entries(params).forEach(([key, value]) => {
        if (value !== undefined && value !== null) {
          searchParams.set(key, String(value));
        }
      });
    }

    // Build the final URL
    const queryString = searchParams.toString();
    return queryString ? `${basePath}?${queryString}` : basePath;
  } catch (error) {
    return url;
  }
}
