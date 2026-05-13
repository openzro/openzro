"use client";

import FullScreenLoading from "@components/ui/FullScreenLoading";

// OIDC redirect target for the auth-code+state handoff. The actual
// callback work runs inside OIDCProvider — this route exists only so
// Next doesn't 404 on `/auth?code=...&state=...` when the IdP comes
// back. The library swaps in CallBackSuccess once it picks up the
// query params, so this fallback is only visible for a frame.
export default function AuthCallback() {
  return <FullScreenLoading />;
}
