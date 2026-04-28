#!/usr/bin/env bash
# openZro · provision the dev OIDC client config so `npm run dev`
# and `make dev.management.up` Just Work.
#
# Dex itself is config-file driven (deploy/dev-idp/dex.config.yaml
# carries staticClients + admin staticPassword) — this script's
# only job is:
#   1. Wait for Dex to be reachable on http://localhost:5556.
#   2. Render deploy/dev-mgmt/management.json from the template.
#   3. Render dashboard/.local-config.json so `npm run dev` finds
#      the right authority + client_id.
#
# Idempotent: re-running mints a fresh management encryption key
# only when management.json is missing. The Dex side is never
# rewritten — the operator edits dex.config.yaml and restarts
# the container if they want different connectors / passwords.

set -euo pipefail

DEX_ISSUER="${DEX_ISSUER:-http://localhost:5556}"
DEX_CLIENT_ID="${DEX_CLIENT_ID:-openzro-dashboard}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
LOCAL_CONFIG="$REPO_ROOT/dashboard/.local-config.json"
MGMT_CONFIG_TMPL="$REPO_ROOT/deploy/dev-mgmt/management.json.tmpl"
MGMT_CONFIG="$REPO_ROOT/deploy/dev-mgmt/management.json"

# --- helpers ---------------------------------------------------------------

require_curl() {
  if ! command -v curl >/dev/null 2>&1; then
    echo "ERROR: curl is required." >&2
    exit 1
  fi
}

wait_dex() {
  local elapsed=0
  local timeout=60
  while ! curl --silent --fail --max-time 2 "$DEX_ISSUER/.well-known/openid-configuration" >/dev/null 2>&1; do
    if (( elapsed >= timeout )); then
      echo "ERROR: Dex did not become reachable at $DEX_ISSUER within ${timeout}s." >&2
      echo "  Check container logs with: make dev.idp.logs" >&2
      exit 1
    fi
    sleep 2
    elapsed=$((elapsed + 2))
    # `[[ ]] && echo …` would propagate the test's false exit
    # under `set -e`, killing the script mid-wait. The `if`
    # block keeps the script-wide error semantics clean.
    if (( elapsed % 10 == 0 )); then
      echo "  …still waiting for Dex (${elapsed}s)"
    fi
  done
}

write_mgmt_config() {
  if [[ ! -f "$MGMT_CONFIG_TMPL" ]]; then
    return 0
  fi
  if [[ -f "$MGMT_CONFIG" ]] && grep -q "\"$DEX_CLIENT_ID\"" "$MGMT_CONFIG" 2>/dev/null; then
    # Already provisioned with the right client; preserve the
    # existing encryption key (rotating it would invalidate the
    # at-rest envelope of any local data).
    return 0
  fi
  local enc_key
  enc_key=$(openssl rand -base64 32 | tr -d '\n')
  sed -e "s|__CLIENT_ID__|$DEX_CLIENT_ID|g" \
      -e "s|__ENCRYPTION_KEY__|$enc_key|g" \
      -e "s|__AUTH_ISSUER__|$DEX_ISSUER|g" \
      "$MGMT_CONFIG_TMPL" >"$MGMT_CONFIG"
}

write_local_config() {
  cat >"$LOCAL_CONFIG" <<EOF
{
  "auth0Auth": "false",
  "authAuthority": "$DEX_ISSUER",
  "authClientId": "$DEX_CLIENT_ID",
  "authClientSecret": "",
  "authScopesSupported": "openid profile email offline_access groups",
  "authAudience": "$DEX_CLIENT_ID",
  "apiOrigin": "http://localhost:33071",
  "grpcApiOrigin": "http://localhost:33073",
  "redirectURI": "/auth",
  "silentRedirectURI": "/silent-auth",
  "tokenSource": "accessToken",
  "dragQueryParams": "false",
  "hotjarTrackID": "",
  "googleAnalyticsID": "",
  "googleTagManagerID": ""
}
EOF
}

# --- main ------------------------------------------------------------------

require_curl

echo "Waiting for Dex at $DEX_ISSUER…"
wait_dex

echo "Writing dashboard/.local-config.json + deploy/dev-mgmt/management.json…"
write_local_config
write_mgmt_config

cat <<EOF

Local IdP ready. Sign in to the dashboard with:
  Email:    admin@openzro.dev
  Password: openzro

To add Google / GitHub / LDAP / etc., edit
  deploy/dev-idp/dex.config.yaml
under the connectors: block (see https://dexidp.io/docs/connectors/),
then 'make dev.idp.down && make dev.idp.up'.
EOF
