// Dev-mode trusted-domains list (loaded when `npm run copytrusted`
// overwrites the substituted prod file with this one). Mirrors the
// regex-escaped, MFA-excluded shape used by the prod template at
// dashboard/public/OidcTrustedDomains.js.tmpl. See that file for the
// full rationale on:
//
//   - regex escaping (so "localhost:33071" does NOT match an evil
//     "localhost:330715.attacker.test"),
//   - host-boundary anchor ((?:$|/) prevents subdomain spoofing),
//   - `/api/mfa/*` negative lookahead (the MFA flow uses its own
//     short-lived JWTs, not the OIDC access token).
//
// Values must stay in sync with `.local-config.json`:
//   authAuthority → "http://localhost:5556"   (bundled Dex)
//   apiOrigin     → "http://localhost:33071"  (management API)

// Same normalization as the prod template — defends against someone
// later pasting a URL with a trailing slash and silently breaking the
// regex match.
const _stripTrailingSlash = (s) => s.replace(/\/+$/, "");
const _esc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
const _hostBoundary = "(?:$|/)";

const _idpRegex = new RegExp(
  "^" + _esc(_stripTrailingSlash("http://localhost:5556")) + _hostBoundary,
);

// Two API regexes because the dev daemon accepts both "localhost" and
// "127.0.0.1" loopbacks; the dashboard config can use either.
const _apiRegexLocalhost = new RegExp(
  "^" + _esc(_stripTrailingSlash("http://localhost:33071")) + "/api/(?!mfa/)",
);
const _apiRegexLoopback = new RegExp(
  "^" + _esc(_stripTrailingSlash("http://127.0.0.1:33071")) + "/api/(?!mfa/)",
);

const trustedDomains = {
  default: {
    oidcDomains: [_idpRegex],
    accessTokenDomains: [_apiRegexLocalhost, _apiRegexLoopback],
    showAccessToken: false,
    allowMultiTabLogin: false,
  },
  auth0: {
    oidcDomains: [_idpRegex],
    accessTokenDomains: [_apiRegexLocalhost, _apiRegexLoopback],
    showAccessToken: false,
    allowMultiTabLogin: false,
  },
};
