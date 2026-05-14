#!/bin/sh
# Post-install for openzro-relay bare-metal package.

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

    # Enable on FIRST install only — but DON'T auto-start because
    # OZ_EXPOSED_ADDRESS + OZ_AUTH_SECRET are mandatory and the env
    # file ships with placeholders.
    if [ "$1" = "1" ] || { [ "$1" = "configure" ] && [ -z "$2" ]; }; then
        systemctl enable openzro-relay.service >/dev/null 2>&1 || true
    fi

    # Intentionally NOT auto-restarting on upgrade. The package
    # scriptlet runs in many contexts (interactive `dnf upgrade`,
    # cloud-init / startup-script auto-update, unattended-upgrades)
    # and silently bouncing a long-lived service on each repo bump
    # causes invisible restarts that look like bugs from the
    # operator's perspective — exactly the boot-time stop+start
    # cycle that prompted this change. Operators who want the new
    # binary active call `systemctl restart` themselves; orchestration
    # tools should add an explicit restart handler.
    if systemctl is-active openzro-relay.service >/dev/null 2>&1; then
        cat <<'EOF'

  Note: openzro-relay is currently running with the previous binary.
  Restart manually to activate the new binary:
      systemctl restart openzro-relay

EOF
    fi
fi

cat <<'EOF'

  openzro-relay installed.
  Next steps:
    1. Edit /etc/default/openzro-relay (uncomment OZ_EXPOSED_ADDRESS
       and OZ_AUTH_SECRET — both required).
    2. systemctl start openzro-relay
    3. journalctl -fu openzro-relay
    4. Update your management.json:
         "Relay": {
             "Addresses": ["rel://your-relay-host:33080"],
             "Secret": "<same as OZ_AUTH_SECRET>"
         }

  Docs: https://docs.openzro.io/selfhosted

EOF
