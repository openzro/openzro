"use client";

import FullScreenLoading from "@components/ui/FullScreenLoading";

// Silent-renew target for the hidden iframe react-oidc uses to refresh
// tokens. Same reasoning as /auth — the library handles the exchange,
// this page just keeps Next from 404-ing the iframe load.
export default function SilentAuthCallback() {
  return <FullScreenLoading />;
}
