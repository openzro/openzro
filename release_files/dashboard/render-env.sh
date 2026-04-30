#!/usr/bin/env bash
#
# render-env.sh — inject runtime env values into the dashboard
# static bundle. Next.js with `output: 'export'` bakes
# NEXT_PUBLIC_* vars at BUILD time, but the openZro dashboard
# uses placeholder strings (`$$AUTH_AUTHORITY` etc.) that this
# script substitutes at INSTALL time so operators don't need to
# rebuild Node.js for every config change.
#
# Usage:
#
#   sudo /usr/share/openzro-dashboard/render-env.sh
#
# The script reads /etc/openzro-dashboard.env (operator-edited
# from the .example template) and rewrites every file under
# DASHBOARD_ROOT (default: /usr/share/openzro-dashboard) that
# contains placeholders.
#
# Re-runnable: each run starts from a pristine .orig copy of the
# files (created on first run), so re-rendering with new env
# values doesn't double-substitute.

set -euo pipefail

DASHBOARD_ROOT="${DASHBOARD_ROOT:-/usr/share/openzro-dashboard}"
ENV_FILE="${ENV_FILE:-/etc/openzro-dashboard.env}"

if [ ! -f "$ENV_FILE" ]; then
    echo "render-env: $ENV_FILE not found." >&2
    echo "Copy /usr/share/doc/openzro-dashboard/openzro-dashboard.env.example" >&2
    echo "to $ENV_FILE, edit, then re-run this script." >&2
    exit 1
fi

# shellcheck disable=SC1090
set -a
. "$ENV_FILE"
set +a

# Sanity-check the required vars. (Old AUTH0_* fallbacks are
# kept for parity with the upstream NetBird init_react_envs.sh —
# operators carrying over an Auth0 config don't need to rename.)
required_vars=(
    AUTH_AUTHORITY AUTH_CLIENT_ID AUTH_AUDIENCE
    AUTH_SUPPORTED_SCOPES USE_AUTH0
    OPENZRO_MGMT_API_ENDPOINT
)
missing=()
for v in "${required_vars[@]}"; do
    if [ -z "${!v:-}" ]; then
        missing+=("$v")
    fi
done
if [ ${#missing[@]} -gt 0 ]; then
    echo "render-env: missing env vars in $ENV_FILE: ${missing[*]}" >&2
    exit 1
fi

# Strip default ports from the API endpoint — same trick the
# docker container's init script uses so the gRPC client
# doesn't appendix :443 to a URL that already has it.
OPENZRO_MGMT_API_ENDPOINT="$(echo "$OPENZRO_MGMT_API_ENDPOINT" | sed -E 's/(:80|:443)$//')"
export OPENZRO_MGMT_API_ENDPOINT

# Default-or-empty exports so envsubst doesn't blow up on
# unset vars.
export AUTH_CLIENT_SECRET="${AUTH_CLIENT_SECRET:-}"
export AUTH_REDIRECT_URI="${AUTH_REDIRECT_URI:-}"
export AUTH_SILENT_REDIRECT_URI="${AUTH_SILENT_REDIRECT_URI:-}"
export OPENZRO_MGMT_GRPC_API_ENDPOINT="${OPENZRO_MGMT_GRPC_API_ENDPOINT:-}"
export OPENZRO_TOKEN_SOURCE="${OPENZRO_TOKEN_SOURCE:-accessToken}"
export OPENZRO_DRAG_QUERY_PARAMS="${OPENZRO_DRAG_QUERY_PARAMS:-false}"

ENV_STR="\$\$USE_AUTH0 \$\$AUTH_AUDIENCE \$\$AUTH_AUTHORITY \$\$AUTH_CLIENT_ID \$\$AUTH_CLIENT_SECRET \$\$AUTH_SUPPORTED_SCOPES \$\$OPENZRO_MGMT_API_ENDPOINT \$\$OPENZRO_MGMT_GRPC_API_ENDPOINT \$\$AUTH_REDIRECT_URI \$\$AUTH_SILENT_REDIRECT_URI \$\$OPENZRO_TOKEN_SOURCE \$\$OPENZRO_DRAG_QUERY_PARAMS"

# 1. OidcTrustedDomains.js is a templated file shipped as
#    `OidcTrustedDomains.js.tmpl` — render once.
TMPL="$DASHBOARD_ROOT/OidcTrustedDomains.js.tmpl"
DEST="$DASHBOARD_ROOT/OidcTrustedDomains.js"
if [ -f "$TMPL" ]; then
    envsubst "$ENV_STR" < "$TMPL" > "$DEST"
    echo "rendered $DEST"
fi

# 2. Every static file with placeholders. We snapshot the
#    original on first run (file.orig) so re-runs read from the
#    pristine copy — otherwise rendering with new values would
#    no longer find the $$VAR markers.
while IFS= read -r f; do
    if [ ! -f "$f.orig" ]; then
        cp -p "$f" "$f.orig"
    fi
    envsubst "$ENV_STR" < "$f.orig" > "$f"
done < <(grep -lr 'AUTH_SUPPORTED_SCOPES' "$DASHBOARD_ROOT" 2>/dev/null || true)

echo "render-env: dashboard rendered with $ENV_FILE"
