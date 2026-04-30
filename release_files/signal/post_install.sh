#!/bin/sh
# Post-install for openzro-signal bare-metal package. Stateless
# component: just creates the openzro user (if shared with mgmt
# install, useradd is a no-op) and installs the systemd unit.

set -e

OZ_USER=openzro
OZ_GROUP=openzro

if ! getent group "$OZ_GROUP" >/dev/null 2>&1; then
    groupadd --system "$OZ_GROUP"
fi
if ! getent passwd "$OZ_USER" >/dev/null 2>&1; then
    useradd --system \
        --gid "$OZ_GROUP" \
        --home-dir /var/lib/openzro \
        --shell /usr/sbin/nologin \
        --comment 'openZro service' \
        "$OZ_USER" || true
fi

if command -V systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true

    if [ "$1" = "1" ] || { [ "$1" = "configure" ] && [ -z "$2" ]; }; then
        systemctl enable openzro-signal.service >/dev/null 2>&1 || true
    fi

    if systemctl is-active openzro-signal.service >/dev/null 2>&1; then
        systemctl restart openzro-signal.service || true
    fi
fi

cat <<'EOF'

  openzro-signal installed.
  Next steps:
    1. systemctl start openzro-signal
    2. journalctl -fu openzro-signal
    3. Update your management.json `Signal.URI` to point at this host.

  Docs: https://docs.openzro.io/selfhosted

EOF
