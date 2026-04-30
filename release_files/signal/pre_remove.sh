#!/bin/sh
set -e
if command -V systemctl >/dev/null 2>&1; then
    if systemctl is-active openzro-signal.service >/dev/null 2>&1; then
        systemctl stop openzro-signal.service || true
    fi
    case "$1" in
        remove|0)
            systemctl disable openzro-signal.service >/dev/null 2>&1 || true
            ;;
    esac
fi
