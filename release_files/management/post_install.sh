#!/bin/sh
# Post-install for openzro-management bare-metal package (deb / rpm /
# archlinux). Creates the openzro service user, lays down the data /
# config dirs, and installs the systemd unit. Does NOT start the
# service — operator has to drop a real /etc/openzro/management.json
# (with their DataStoreEncryptionKey + IdP issuer) before that's safe.

set -e

OZ_USER=openzro
OZ_GROUP=openzro
DATA_DIR=/var/lib/openzro/management
LOG_DIR=/var/log/openzro
CONFIG_DIR=/etc/openzro

# 1. Create system user/group if missing. uid 800-ish range is typical
# for system services across deb/rpm. Don't use /home — daemons get
# /var/lib/openzro/management as their state dir.
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

# 2. Lay down data + log dirs owned by openzro:openzro 0750.
mkdir -p "$DATA_DIR" "$LOG_DIR" "$CONFIG_DIR"
chown -R "$OZ_USER:$OZ_GROUP" "$DATA_DIR" "$LOG_DIR"
chmod 0750 "$DATA_DIR" "$LOG_DIR"

# 3. If management.json doesn't exist, drop the example as a starting
# point. Otherwise leave the operator's file alone — package upgrades
# must NOT clobber a hand-tuned config.
if [ ! -f "$CONFIG_DIR/management.json" ]; then
    if [ -f "$CONFIG_DIR/management.json.example" ]; then
        cp "$CONFIG_DIR/management.json.example" "$CONFIG_DIR/management.json"
        chown "$OZ_USER:$OZ_GROUP" "$CONFIG_DIR/management.json"
        chmod 0640 "$CONFIG_DIR/management.json"
        printf '\033[33mInstalled example management.json — edit before starting:\033[0m\n'
        printf '  %s\n' "$CONFIG_DIR/management.json"
        printf '  Required: DataStoreEncryptionKey, HttpConfig.AuthIssuer,\n'
        printf '            OIDCConfigEndpoint, Signal.URI, Relay.Addresses\n'
    fi
fi

# 4. systemd: reload unit cache; enable on first install but DON'T
# start (config might still be the example with REPLACE_WITH_ markers).
if command -V systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true

    # Enable on FIRST install only. Subsequent upgrades preserve
    # whatever the operator set (enabled or disabled).
    if [ "$1" = "1" ] || [ "$1" = "configure" ] && [ -z "$2" ]; then
        systemctl enable openzro-management.service >/dev/null 2>&1 || true
    fi

    # Intentionally NOT auto-restarting on upgrade. The package
    # scriptlet runs in many contexts (interactive `dnf upgrade`,
    # cloud-init / startup-script auto-update, unattended-upgrades)
    # and silently bouncing a long-lived service on each repo bump
    # causes invisible restarts that look like bugs from the
    # operator's perspective. Operators who want the new binary
    # active call `systemctl restart` themselves; orchestration tools
    # should add an explicit restart handler.
    if systemctl is-active openzro-management.service >/dev/null 2>&1; then
        cat <<'EOF'

  Note: openzro-management is currently running with the previous binary.
  Restart manually to activate the new binary:
      systemctl restart openzro-management

EOF
    fi
fi

cat <<'EOF'

  openzro-management installed.
  Next steps:
    1. Edit /etc/openzro/management.json (replace DataStoreEncryptionKey,
       AuthIssuer, OIDCConfigEndpoint, Signal.URI, Relay.Addresses).
    2. systemctl start openzro-management
    3. journalctl -fu openzro-management

  Docs: https://docs.openzro.io/selfhosted

EOF
