// mfaSession holds the X-MFA-Token the dashboard sends alongside the
// JWT Authorization header (issue #31 review finding #3: per-session
// verification replaces the per-user last_verified_at heuristic).
//
// Storage choice: sessionStorage, NOT localStorage. The token's
// validity is bound to the current JWT session id (sha256 of the
// bearer); a different tab with a different OIDC session would have
// a different jti_binding and the token wouldn't verify anyway. Tying
// the storage to the tab lifetime also means a browser-close cleanly
// drops the elevated state without relying on the token's exp claim.

const KEY = "openzro.mfa_session_token";

// isBrowser guards SSR-safe access. The dashboard renders some
// components on the server; touching window.sessionStorage there
// throws. Returning undefined/no-op keeps the call sites clean.
function isBrowser(): boolean {
  return typeof window !== "undefined" && typeof window.sessionStorage !== "undefined";
}

export function getMfaSessionToken(): string | undefined {
  if (!isBrowser()) return undefined;
  return window.sessionStorage.getItem(KEY) || undefined;
}

export function setMfaSessionToken(token: string): void {
  if (!isBrowser()) return;
  window.sessionStorage.setItem(KEY, token);
}

export function clearMfaSessionToken(): void {
  if (!isBrowser()) return;
  window.sessionStorage.removeItem(KEY);
}

// Redirect-token storage (one-shot): the dashboard's 403 interceptor
// in api.tsx stashes the challenge_token / enrollment_token here
// before navigating to /mfa/challenge or /mfa/enroll. The destination
// page reads it once and clears it.
const REDIRECT_KEY = "openzro.mfa_redirect_token";

export function getMfaRedirectToken(): string | undefined {
  if (!isBrowser()) return undefined;
  return window.sessionStorage.getItem(REDIRECT_KEY) || undefined;
}

export function setMfaRedirectToken(token: string): void {
  if (!isBrowser()) return;
  window.sessionStorage.setItem(REDIRECT_KEY, token);
}

export function clearMfaRedirectToken(): void {
  if (!isBrowser()) return;
  window.sessionStorage.removeItem(REDIRECT_KEY);
}
