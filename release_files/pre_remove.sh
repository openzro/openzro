#!/bin/sh
# decide if we should use systemd or init/upstart
use_systemctl="True"
systemd_version=0
if ! command -V systemctl >/dev/null 2>&1; then
  use_systemctl="False"
else
    systemd_version=$(systemctl --version | head -1 | sed 's/systemd //g')
fi

remove() {
  printf "\033[32m Pre uninstall\033[0m\n"

  if [ "${use_systemctl}" = "True" ]; then
    printf "\033[32m Stopping the service\033[0m\n"
    systemctl stop openzro || true

    if [ -e /lib/systemd/system/openzro.service ]; then
      rm -f /lib/systemd/system/openzro.service
      systemctl daemon-reload || true
    fi

  fi
  printf "\033[32m Uninstalling the service\033[0m\n"
  /usr/bin/openzro service uninstall || true


  if [ "${use_systemctl}" = "True" ]; then
     printf "\n\033[32m running daemon reload\033[0m\n"
     systemctl daemon-reload || true
  fi

  # Drop the NetworkManager unmanaged-devices config that
  # post_install.sh placed under /etc/NetworkManager/conf.d/. Without
  # this, an uninstall leaves the file orphaned, telling NM forever
  # to leave wt0 alone — wrong if the operator later installs a
  # different WireGuard tool that uses the same interface name.
  nm_file="/etc/NetworkManager/conf.d/openzro.conf"
  if [ -f "${nm_file}" ]; then
    rm -f "${nm_file}"
    if command -V nmcli >/dev/null 2>&1; then
      nmcli general reload 2>/dev/null || nmcli connection reload 2>/dev/null || true
    fi
    printf "\033[32m Removed NetworkManager unmanaged-devices config (%s)\033[0m\n" "${nm_file}"
  fi
}

action="$1"

case "$action" in
  "0" | "remove")
    remove
    ;;
  *)
    exit 0
    ;;
esac
