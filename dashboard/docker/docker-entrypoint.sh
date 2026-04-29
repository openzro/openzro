#!/bin/bash
# Container entrypoint. Runs init_react_envs.sh SYNCHRONOUSLY so the
# envsubst pass over config.json + OidcTrustedDomains.js completes
# BEFORE nginx starts serving — supervisord's `priority` ordering only
# affects start order, not completion order, so without this shim
# nginx briefly serves the unsubstituted templates (config.json with
# literal `$AUTH_AUTHORITY` etc) and the dashboard tries to fetch OIDC
# discovery from the wrong URL.
#
# After envsubst completes, exec supervisord which manages nginx +
# cron + cert.

set -e

/usr/local/init_react_envs.sh

exec /usr/bin/supervisord -c /etc/supervisord.conf
