// Origins to which the @axa-fr/oidc-client lib is allowed to forward
// access tokens. Must stay in sync with `apiOrigin` from
// `.local-config.json` — the dev management API lives at :33071, not
// :3001 (the dashboard's own port). Earlier versions listed :3001/3000
// which is stale: the lib's interceptor never matched the real API
// origin, so it silently never forwarded tokens. `useOpenzroFetch`
// attaches the Bearer manually so the dev flow worked anyway, but
// that fallback is fragile and the trusted-domains list is the
// authoritative source.
const trustedDomains = {
  default: ["http://localhost:33071", "http://127.0.0.1:33071"],
  auth0: [],
};
