#!/usr/bin/env bash
# openZro · provision a dev OIDC client in the local Zitadel and
# write dashboard/.local-config.json so `npm run dev` Just Works.
#
# Idempotent: re-running is a no-op unless the marker file is
# missing. Re-runs after `dev.idp.down -v` (which wipes the volume)
# will reprovision from scratch.

set -euo pipefail

INSTANCE_URL="${INSTANCE_URL:-http://127.0.0.1:8080}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
TOKEN_PATH="$HERE/machinekey/zitadel-admin-sa.token"
MARKER_PATH="$HERE/machinekey/provisioned.json"
LOCAL_CONFIG="$REPO_ROOT/dashboard/.local-config.json"
MGMT_CONFIG_TMPL="$REPO_ROOT/deploy/dev-mgmt/management.json.tmpl"
MGMT_CONFIG="$REPO_ROOT/deploy/dev-mgmt/management.json"

# Dashboard dev defaults — these match next.config / OIDCProvider expectations.
DASHBOARD_URL="${DASHBOARD_URL:-http://localhost:3000}"
REDIRECT_URI="$DASHBOARD_URL/auth"
SILENT_REDIRECT_URI="$DASHBOARD_URL/silent-auth"
LOGOUT_REDIRECT_URI="$DASHBOARD_URL/"

# --- helpers ----------------------------------------------------------------

require_jq() {
  if ! command -v jq >/dev/null 2>&1; then
    echo "ERROR: jq is required. Install with: sudo apt install jq (or brew install jq)" >&2
    exit 1
  fi
}

require_curl() {
  if ! command -v curl >/dev/null 2>&1; then
    echo "ERROR: curl is required." >&2
    exit 1
  fi
}

wait_pat() {
  local elapsed=0
  # First-boot bootstrap (migrations + first-instance setup) can take
  # 2–3 min on slower disks. Subsequent boots: <10s.
  local timeout=300
  while [[ ! -s "$TOKEN_PATH" ]]; do
    if (( elapsed >= timeout )); then
      echo "ERROR: Zitadel did not produce a PAT at $TOKEN_PATH within ${timeout}s." >&2
      echo "       Check 'docker compose -f deploy/dev-idp.compose.yml logs zitadel'." >&2
      exit 1
    fi
    sleep 2
    elapsed=$((elapsed + 2))
    [[ $((elapsed % 15)) -eq 0 ]] && echo "  …still waiting for Zitadel bootstrap (${elapsed}s)"
  done
}

wait_api() {
  local pat=$1
  local elapsed=0
  while ! curl -sf --connect-timeout 2 -o /dev/null \
       -H "Authorization: Bearer $pat" \
       "$INSTANCE_URL/auth/v1/users/me"; do
    if (( elapsed >= 60 )); then
      echo "ERROR: Zitadel API did not become reachable at $INSTANCE_URL within 60s." >&2
      exit 1
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done
}

assert_response_id() {
  local response=$1
  local field=$2
  local context=$3
  local id
  id=$(echo "$response" | jq -r ".$field // \"null\"")
  if [[ "$id" == "null" || -z "$id" ]]; then
    echo "ERROR: $context returned no .$field" >&2
    echo "Response: $response" >&2
    exit 1
  fi
  echo "$id"
}

find_project_by_name() {
  local pat=$1
  local name=$2
  local response
  response=$(curl -sS -X POST "$INSTANCE_URL/management/v1/projects/_search" \
    -H "Authorization: Bearer $pat" \
    -H "Content-Type: application/json" \
    -d '{"queries": [{"nameQuery": {"name": "'"$name"'", "method": "TEXT_QUERY_METHOD_EQUALS"}}]}')
  echo "$response" | jq -r '.result[0].id // ""'
}

find_oidc_client_by_app_name() {
  local pat=$1
  local project_id=$2
  local app_name=$3
  local response
  response=$(curl -sS -X POST \
    "$INSTANCE_URL/management/v1/projects/$project_id/apps/_search" \
    -H "Authorization: Bearer $pat" \
    -H "Content-Type: application/json" \
    -d '{"queries": [{"nameQuery": {"name": "'"$app_name"'", "method": "TEXT_QUERY_METHOD_EQUALS"}}]}')
  echo "$response" | jq -r '.result[0].oidcConfig.clientId // ""'
}

create_project() {
  local pat=$1
  local existing
  existing=$(find_project_by_name "$pat" "openzro-dev")
  if [[ -n "$existing" ]]; then
    echo "$existing"
    return
  fi
  local response
  response=$(curl -sS -X POST "$INSTANCE_URL/management/v1/projects" \
    -H "Authorization: Bearer $pat" \
    -H "Content-Type: application/json" \
    -d '{"name": "openzro-dev"}')
  assert_response_id "$response" "id" "create_project"
}

create_oidc_app() {
  local pat=$1
  local project_id=$2
  local existing
  existing=$(find_oidc_client_by_app_name "$pat" "$project_id" "openzro-dashboard")
  if [[ -n "$existing" ]]; then
    echo "$existing"
    return
  fi
  local response
  response=$(curl -sS -X POST \
    "$INSTANCE_URL/management/v1/projects/$project_id/apps/oidc" \
    -H "Authorization: Bearer $pat" \
    -H "Content-Type: application/json" \
    -d '{
      "name": "openzro-dashboard",
      "redirectUris": ["'"$REDIRECT_URI"'", "'"$SILENT_REDIRECT_URI"'"],
      "postLogoutRedirectUris": ["'"$LOGOUT_REDIRECT_URI"'"],
      "additionalOrigins": ["'"$DASHBOARD_URL"'"],
      "responseTypes": ["OIDC_RESPONSE_TYPE_CODE"],
      "grantTypes": ["OIDC_GRANT_TYPE_AUTHORIZATION_CODE", "OIDC_GRANT_TYPE_REFRESH_TOKEN"],
      "appType": "OIDC_APP_TYPE_USER_AGENT",
      "authMethodType": "OIDC_AUTH_METHOD_TYPE_NONE",
      "version": "OIDC_VERSION_1_0",
      "devMode": true,
      "accessTokenType": "OIDC_TOKEN_TYPE_JWT",
      "accessTokenRoleAssertion": true,
      "skipNativeAppSuccessPage": true
    }')
  assert_response_id "$response" "clientId" "create_oidc_app"
}

write_mgmt_config() {
  local client_id=$1
  if [[ ! -f "$MGMT_CONFIG_TMPL" ]]; then
    return 0
  fi
  local enc_key
  enc_key=$(openssl rand -base64 32 | tr -d '\n')
  sed -e "s|__CLIENT_ID__|$client_id|g" \
      -e "s|__ENCRYPTION_KEY__|$enc_key|g" \
      "$MGMT_CONFIG_TMPL" >"$MGMT_CONFIG"
}

write_local_config() {
  local client_id=$1
  cat >"$LOCAL_CONFIG" <<EOF
{
  "auth0Auth": "false",
  "authAuthority": "$INSTANCE_URL",
  "authClientId": "$client_id",
  "authClientSecret": "",
  "authScopesSupported": "openid profile email offline_access",
  "authAudience": "$client_id",
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

# --- main -------------------------------------------------------------------

require_curl
require_jq

if [[ -f "$MARKER_PATH" ]]; then
  client_id=$(jq -r '.clientId' "$MARKER_PATH")
  if [[ -n "$client_id" && "$client_id" != "null" ]]; then
    needs_rewrite=0
    if [[ ! -f "$LOCAL_CONFIG" ]] || ! grep -q "\"$client_id\"" "$LOCAL_CONFIG" 2>/dev/null; then
      needs_rewrite=1
    fi
    if [[ ! -f "$MGMT_CONFIG" ]] || ! grep -q "\"$client_id\"" "$MGMT_CONFIG" 2>/dev/null; then
      needs_rewrite=1
    fi
    if (( needs_rewrite )); then
      echo "Provisioning marker present; rewriting dashboard/.local-config.json + management.json"
      write_local_config "$client_id"
      write_mgmt_config "$client_id"
    else
      echo "Already provisioned (clientId=$client_id). Nothing to do."
    fi
    exit 0
  fi
fi

echo "Waiting for Zitadel to write the admin PAT…"
wait_pat
PAT=$(cat "$TOKEN_PATH")

echo "Waiting for Zitadel API to be ready…"
wait_api "$PAT"

echo "Creating project 'openzro-dev'…"
PROJECT_ID=$(create_project "$PAT")

echo "Creating OIDC application 'openzro-dashboard'…"
CLIENT_ID=$(create_oidc_app "$PAT" "$PROJECT_ID")

echo "Writing dashboard/.local-config.json + deploy/dev-mgmt/management.json…"
write_local_config "$CLIENT_ID"
write_mgmt_config "$CLIENT_ID"

cat >"$MARKER_PATH" <<EOF
{
  "projectId": "$PROJECT_ID",
  "clientId": "$CLIENT_ID",
  "instanceUrl": "$INSTANCE_URL"
}
EOF

cat <<EOF

  Provisioned dev IdP.
  Zitadel admin UI : $INSTANCE_URL/ui/console
  Login            : test@zitadel.localhost  (or just 'test' on a fresh reset)
  Password         : test
  OIDC clientId    : $CLIENT_ID

EOF
