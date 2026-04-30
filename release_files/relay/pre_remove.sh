#!/bin/sh
set -e
if command -V systemctl >/dev/null 2>&1; then
    if systemctl is-active openzro-relay.service >/dev/null 2>&1; then
        systemctl stop openzro-relay.service || true
    fi
    case "$1" in
        remove|0)
            systemctl disable openzro-relay.service >/dev/null 2>&1 || true
            ;;
    esac
fi
