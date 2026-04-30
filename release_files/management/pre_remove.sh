#!/bin/sh
# Pre-remove for openzro-management. Stops + disables the service so
# we don't leave a half-removed binary running. Data dir + config dir
# are intentionally left in place — operators have setup keys, ACL
# policies, peer registrations etc that they need to either preserve
# (postgres-backed) or back up (sqlite) before purging.

set -e

if command -V systemctl >/dev/null 2>&1; then
    if systemctl is-active openzro-management.service >/dev/null 2>&1; then
        systemctl stop openzro-management.service || true
    fi
    # Only on full uninstall — skip on upgrade ($1 = "upgrade" on deb,
    # $1 = "1" on rpm upgrade). For upgrades the new package's
    # post_install handles restart.
    case "$1" in
        remove|0)
            systemctl disable openzro-management.service >/dev/null 2>&1 || true
            ;;
    esac
fi
