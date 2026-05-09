#!/bin/sh

# Step 1, decide if we should use systemd or init/upstart
use_systemctl="True"
systemd_version=0
if ! command -V systemctl >/dev/null 2>&1; then
  use_systemctl="False"
else
    systemd_version=$(systemctl --version | head -1 | sed 's/systemd //g')
fi

# Detect environments where service registration won't work — typical
# in container images that ship without an init system (Docker minimal
# bases: alpine, fedora, debian). In those, `openzro service install`
# would fail with "no such file or directory: /etc/init.d/openzro" or
# "exec: service: executable file not found". We skip with a friendly
# message instead of failing the package install — operators can run
# `openzro service install` manually if/when an init system shows up.
have_usable_init() {
    # systemctl present AND functional (running, degraded — not "offline"
    # which is what containers report).
    if [ "${use_systemctl}" = "True" ] && systemctl is-system-running >/dev/null 2>&1; then
        return 0
    fi
    # sysv fallback: /etc/init.d/ + `service` command both present.
    if [ -d /etc/init.d ] && command -V service >/dev/null 2>&1; then
        return 0
    fi
    return 1
}

skip_service_install_msg() {
    printf "\033[33m  No init system detected (container? minimal chroot?). "
    printf "Skipping service registration —\033[0m\n"
    printf "\033[33m  run 'sudo openzro service install && sudo openzro service start' "
    printf "manually once systemd/sysv is available.\033[0m\n"
}

# Tell NetworkManager to leave the openzro tunnel interface alone
# (closes the GUI race where users disconnect the tunnel and break
# their internet because NM-managed DNS gets cleared with it).
# Upstream NetBird issue #5555 has been open and unaddressed since
# Mar/2026; the fix is a one-shot config drop, no daemon to invoke.
configure_networkmanager_unmanaged() {
    nm_dir="/etc/NetworkManager/conf.d"
    nm_file="${nm_dir}/openzro.conf"
    # Only act when the directory exists — otherwise NM isn't
    # installed on this host and we'd be littering /etc.
    if [ ! -d "${nm_dir}" ]; then
        return 0
    fi
    if [ -f "${nm_file}" ] && grep -q "^unmanaged-devices=interface-name:wt0" "${nm_file}" 2>/dev/null; then
        return 0
    fi
    cat > "${nm_file}" <<'EOF'
# Managed by openzro package. Do not edit — overwritten on upgrade.
# Tells NetworkManager to ignore the openzro WireGuard tunnel
# interface so the tunnel isn't accidentally torn down via the GUI
# (which clears its DNS along the way and breaks browsing).
[keyfile]
unmanaged-devices=interface-name:wt0
EOF
    chmod 0644 "${nm_file}" 2>/dev/null || true
    if command -V nmcli >/dev/null 2>&1; then
        nmcli general reload 2>/dev/null || nmcli connection reload 2>/dev/null || true
    fi
    printf "\033[32m  NetworkManager configured to leave wt0 unmanaged (%s)\033[0m\n" "${nm_file}"
}

cleanInstall() {
    printf "\033[32m Post Install of a clean install\033[0m\n"
    configure_networkmanager_unmanaged
    if ! have_usable_init; then
        skip_service_install_msg
        return 0
    fi
    # Step 3 (clean install), enable the service in the proper way for this platform
    /usr/bin/openzro service install
    /usr/bin/openzro service start
}

upgrade() {
    printf "\033[32m Post Install of an upgrade\033[0m\n"
    configure_networkmanager_unmanaged
    if [ "${use_systemctl}" = "True" ]; then
      printf "\033[32m Stopping the service\033[0m\n"
      systemctl stop openzro 2> /dev/null || true
    fi
    if [ -e /lib/systemd/system/openzro.service ]; then
      rm -f /lib/systemd/system/openzro.service
      systemctl daemon-reload 2>/dev/null || true
    fi
    if ! have_usable_init; then
        skip_service_install_msg
        return 0
    fi
    # will throw an error until everyone upgrade
    /usr/bin/openzro service uninstall 2> /dev/null || true
    /usr/bin/openzro service install
    /usr/bin/openzro service start
}

# Check if this is a clean install or an upgrade
action="$1"
if  [ "$1" = "configure" ] && [ -z "$2" ]; then
  # Alpine linux does not pass args, and deb passes $1=configure
  action="install"
elif [ "$1" = "configure" ] && [ -n "$2" ]; then
    # deb passes $1=configure $2=<current version>
    action="upgrade"
fi

case "$action" in
  "1" | "install")
    cleanInstall
    ;;
  "2" | "upgrade")
    upgrade
    ;;
  *)
    cleanInstall
    ;;
esac