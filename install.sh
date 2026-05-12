# This code is based on the openzro-installer contribution by physk on GitHub.
# Source: https://github.com/physk/openzro-installer
set -e

CONFIG_FOLDER="/etc/openzro"
CONFIG_FILE="$CONFIG_FOLDER/install.conf"

OWNER="openzro"
REPO="openzro"
CLI_APP="openzro"
UI_APP="openzro-ui"

# Set default variable
OS_NAME=""
OS_TYPE=""
ARCH="$(uname -m)"
PACKAGE_MANAGER="bin"
INSTALL_DIR=""
SUDO=""


if command -v sudo > /dev/null && [ "$(id -u)" -ne 0 ]; then
    SUDO="sudo"
elif command -v doas > /dev/null && [ "$(id -u)" -ne 0 ]; then
    SUDO="doas"
fi

if [ -z ${OPENZRO_RELEASE+x} ]; then
    OPENZRO_RELEASE=latest
fi

get_release() {
    local RELEASE=$1
    if [ "$RELEASE" = "latest" ]; then
        # /releases/latest excludes prereleases (everything pre-1.0 is
        # tagged as prerelease), and /releases sorts by tag_name
        # lexicographically — *not* by date — so v0.53.1-alpha.9
        # ranks "newer" than v0.53.1-alpha.14. Pull a page of recent
        # releases and pick the highest tag with `sort -V` (version
        # sort), which handles pre-release suffixes correctly. Both
        # GNU sort (Linux) and BSD sort (macOS 11+) support -V.
        local URL="https://api.github.com/repos/${OWNER}/${REPO}/releases?per_page=100"
        if [ -n "$GITHUB_TOKEN" ]; then
              curl -H "Authorization: token ${GITHUB_TOKEN}" -s "${URL}" \
                  | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' \
                  | sort -V | tail -1
        else
              curl -s "${URL}" \
                  | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' \
                  | sort -V | tail -1
        fi
    else
        local URL="https://api.github.com/repos/${OWNER}/${REPO}/releases/tags/${RELEASE}"
        if [ -n "$GITHUB_TOKEN" ]; then
              curl -H "Authorization: token ${GITHUB_TOKEN}" -s "${URL}" \
                  | grep -m1 '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/'
        else
              curl -s "${URL}" \
                  | grep -m1 '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/'
        fi
    fi
}

download_release_binary() {
    VERSION=$(get_release "$OPENZRO_RELEASE")
    if [ -z "$VERSION" ]; then
        echo "Could not resolve openzro release tag from GitHub API." >&2
        echo "Common causes:" >&2
        echo "  - unauthenticated rate limit hit (60 req/hour); set GITHUB_TOKEN" >&2
        echo "  - network blocks api.github.com" >&2
        echo "  - OPENZRO_RELEASE='$OPENZRO_RELEASE' tag does not exist" >&2
        exit 1
    fi
    BASE_URL="https://github.com/${OWNER}/${REPO}/releases/download"

    # The desktop UI on macOS is a single universal artifact (built by
    # .goreleaser.ui-darwin.yaml — fyne signtool would normally also
    # produce a *_signed.zip, but we don't ship signed UI yet — that
    # waits on SignPath/Apple Developer enrollment). Everywhere else
    # (CLI on any OS, UI on Linux) follows the per-arch tar.gz pattern.
    if [ "$OS_TYPE" = "darwin" ] && [ "$1" = "$UI_APP" ]; then
        BINARY_BASE_NAME="${VERSION#v}_darwin_universal.tar.gz"
    else
        BINARY_BASE_NAME="${VERSION#v}_${OS_TYPE}_${ARCH}.tar.gz"
    fi

    BINARY_NAME="$1_${BINARY_BASE_NAME}"
    DOWNLOAD_URL="${BASE_URL}/${VERSION}/${BINARY_NAME}"

    echo "Installing $1 from $DOWNLOAD_URL"
    if [ -n "$GITHUB_TOKEN" ]; then
      cd /tmp && curl -fLO -H "Authorization: token ${GITHUB_TOKEN}" "$DOWNLOAD_URL"
    else
      cd /tmp && curl -fLO "$DOWNLOAD_URL"
    fi

    # The darwin UI archive ships just the openzro-ui binary, not a
    # .app bundle — fyne package + signing is gated on SignPath/Apple
    # enrollment. Drop the binary in /usr/local/bin alongside the CLI;
    # users invoke `openzro-ui` from a terminal until we ship the
    # signed .app.
    ${SUDO} mkdir -p "$INSTALL_DIR"
    tar -xzvf "$BINARY_NAME"
    ${SUDO} mv "${1%_"${BINARY_BASE_NAME}"}" "$INSTALL_DIR/"
}

add_apt_repo() {
    ${SUDO} apt-get update
    ${SUDO} apt-get install ca-certificates curl gnupg -y

    # Remove old keys and repo source files
    ${SUDO} rm -f \
        /etc/apt/sources.list.d/openzro.list \
        /etc/apt/sources.list.d/wiretrustee.list \
        /etc/apt/trusted.gpg.d/wiretrustee.gpg \
        /usr/share/keyrings/openzro-archive-keyring.gpg \
        /usr/share/keyrings/wiretrustee-archive-keyring.gpg

    # The repo serves the GPG key in ASCII-armored form at the root
    # path; both apt and yum use the same canonical key file.
    curl -sSL https://pkg.openzro.io/openzro-archive-key.asc \
    | ${SUDO} gpg --dearmor -o /usr/share/keyrings/openzro-archive-keyring.gpg

    # Explicitly set the file permission
    ${SUDO} chmod 0644 /usr/share/keyrings/openzro-archive-keyring.gpg

    echo 'deb [signed-by=/usr/share/keyrings/openzro-archive-keyring.gpg] https://pkg.openzro.io/apt stable main' \
    | ${SUDO} tee /etc/apt/sources.list.d/openzro.list

    ${SUDO} apt-get update
}

add_rpm_repo() {
# Full-chain GPG verification — publish-packages.sh signs both
# the individual .rpm files (via rpmsign --addsign) and the
# repodata/repomd.xml. dnf/yum/zypper verify both:
#   gpgcheck=1      — each .rpm signature
#   repo_gpgcheck=1 — repodata/repomd.xml.asc signature
cat <<-EOF | ${SUDO} tee /etc/yum.repos.d/openzro.repo
[Openzro]
name=Openzro
baseurl=https://pkg.openzro.io/rpm/\$basearch
enabled=1
gpgcheck=1
gpgkey=https://pkg.openzro.io/openzro-archive-key.asc
repo_gpgcheck=1
EOF
}

add_pacman_repo() {
    # pkg.openzro.io publishes a signed pacman repo at /pacman/<arch>/.
    # Older versions of this script built openzro from AUR with
    # makepkg — that path is removed; the AUR package is currently
    # not maintained, and the upstream repo is the supported flow.
    ${SUDO} pacman -Sy --noconfirm --needed gnupg curl

    ${SUDO} pacman-key --init >/dev/null 2>&1 || true

    # Fetch the public key, extract its fingerprint, import + locally
    # sign so pacman trusts it for SigLevel = Required.
    TMP_KEY=$(mktemp)
    curl -fsSL https://pkg.openzro.io/openzro-archive-key.asc -o "$TMP_KEY"
    FP=$(gpg --show-keys --with-colons --import-options show-only "$TMP_KEY" \
         | awk -F: '/^fpr:/ {print $10; exit}')
    if [ -z "$FP" ]; then
        echo "Could not determine GPG fingerprint for openZro repo key"
        rm -f "$TMP_KEY"
        exit 1
    fi
    ${SUDO} pacman-key -a "$TMP_KEY"
    ${SUDO} pacman-key --lsign-key "$FP"
    rm -f "$TMP_KEY"

    # Drop in the repo definition. /etc/pacman.conf has no .d
    # directory; we append once and skip on re-runs.
    if ! grep -q '^\[openzro\]' /etc/pacman.conf; then
        PAC_ARCH="$(uname -m)"
        ${SUDO} tee -a /etc/pacman.conf > /dev/null <<EOF

[openzro]
SigLevel = Required DatabaseRequired
Server = https://pkg.openzro.io/pacman/$PAC_ARCH
EOF
    fi
    ${SUDO} pacman -Sy
}

prepare_tun_module() {
  # Create the necessary file structure for /dev/net/tun
  if [ ! -c /dev/net/tun ]; then
    if [ ! -d /dev/net ]; then
      mkdir -m 755 /dev/net
    fi
    mknod /dev/net/tun c 10 200
    chmod 0755 /dev/net/tun
  fi

  # Load the tun module if not already loaded
  if ! lsmod | grep -q "^tun\s"; then
    insmod /lib/modules/tun.ko
  fi
}

install_native_binaries() {
    # Checks  for supported architecture
    case "$ARCH" in
        x86_64|amd64)
            ARCH="amd64"
        ;;
        i?86|x86)
            ARCH="386"
        ;;
        aarch64|arm64)
            ARCH="arm64"
        ;;
        *)
            echo "Architecture ${ARCH} not supported"
            exit 2
        ;;
    esac

    # download and copy binaries to INSTALL_DIR
    download_release_binary "$CLI_APP"
    if ! $SKIP_UI_APP; then
        download_release_binary "$UI_APP"
    fi
}

# Handle macOS .pkg installer
install_pkg() {
  VERSION=$(get_release "$OPENZRO_RELEASE")
  if [ -z "$VERSION" ]; then
      echo "Could not resolve openzro release tag from GitHub API" >&2
      exit 1
  fi

  # goreleaser produces a single universal .pkg covering both
  # x86_64 and arm64 macs.
  PKG_NAME="openzro_${VERSION#v}_darwin_universal.pkg"
  PKG_URL="https://github.com/${OWNER}/${REPO}/releases/download/${VERSION}/${PKG_NAME}"

  echo "Downloading openZro macOS installer from ${PKG_URL}"
  curl -fsSL -o /tmp/openzro.pkg "${PKG_URL}"
  ${SUDO} installer -pkg /tmp/openzro.pkg -target /
  rm -f /tmp/openzro.pkg
}

check_use_bin_variable() {
    if [ "${USE_BIN_INSTALL}-x" = "true-x" ]; then
      echo "The installation will be performed using binary files"
      return 0
    fi
    return 1
}

install_openzro() {
    if [ -x "$(command -v openzro)" ]; then
      status_output="$(openzro status 2>&1 || true)"

      if echo "$status_output" | grep -q 'failed to connect to daemon error: context deadline exceeded'; then
          echo "Warning: could not reach Openzro daemon (timeout), proceeding anyway"
      else
          if echo "$status_output" | grep -q 'Management: Connected' && \
              echo "$status_output" | grep -q 'Signal: Connected'; then
              echo "Openzro service is running, please stop it before proceeding"
              exit 1
          fi

          if [ -n "$status_output" ]; then
              echo "Openzro seems to be installed already, please remove it before proceeding"
              exit 1
          fi
      fi
    fi

    # Run the installation, if a desktop environment is not detected
    # only the CLI will be installed
    case "$PACKAGE_MANAGER" in
    apt)
        add_apt_repo
        ${SUDO} apt-get install openzro -y

        if ! $SKIP_UI_APP; then
            ${SUDO} apt-get install openzro-ui -y
        fi
    ;;
    yum)
        add_rpm_repo
        ${SUDO} yum -y install openzro
        if ! $SKIP_UI_APP; then
            ${SUDO} yum -y install openzro-ui
        fi
    ;;
    dnf)
        add_rpm_repo
        ${SUDO} dnf -y install openzro

        if ! $SKIP_UI_APP; then
            ${SUDO} dnf -y install openzro-ui
        fi
    ;;
    rpm-ostree)
        add_rpm_repo
        ${SUDO} rpm-ostree -y install openzro
        if ! $SKIP_UI_APP; then
            ${SUDO} rpm-ostree -y install openzro-ui
        fi
    ;;
    pacman)
        add_pacman_repo
        ${SUDO} pacman -S --noconfirm openzro
        # openzro-ui is not yet packaged for pacman; users who want
        # the UI on Arch can grab the binary from the GitHub release
        # tarball or use AUR once the package is published.
        if ! $SKIP_UI_APP; then
            echo "Note: openzro-ui is not packaged for pacman yet."
            echo "      Skipping UI install; CLI 'openzro' is available."
        fi
    ;;
    pkg)
        # Check if the package is already installed
        if [ -f /Library/Receipts/openzro.pkg ]; then
            echo "Openzro is already installed. Please remove it before proceeding."
            exit 1
        fi

        # Install the package
        install_pkg
    ;;
    brew)
        # Remove Openzro if it had been installed using Homebrew before
        if brew ls --versions openzro >/dev/null 2>&1; then
            echo "Removing existing openzro client"

            # Stop and uninstall daemon service:
            openzro service stop
            openzro service uninstall

            # Unlink the app
            brew unlink openzro
        fi

        brew install openzro/tap/openzro
        if ! $SKIP_UI_APP; then
            brew install --cask openzro/tap/openzro-ui
        fi
    ;;
    *)
      if [ "$OS_NAME" = "nixos" ];then
        echo "Please add Openzro to your NixOS configuration.nix directly:"
			  echo ""
			  echo "services.openzro.enable = true;"

        if ! $SKIP_UI_APP; then
          echo "environment.systemPackages = [ pkgs.openzro-ui ];"
        fi

        echo "Build and apply new configuration:"
        echo ""
        echo "${SUDO} nixos-rebuild switch"
			  exit 0
      fi

        install_native_binaries
    ;;
    esac

    if [ "$OS_NAME" = "synology" ]; then
        prepare_tun_module
    fi

    # Add package manager to config
    ${SUDO} mkdir -p "$CONFIG_FOLDER"
    echo "package_manager=$PACKAGE_MANAGER" | ${SUDO} tee "$CONFIG_FILE" > /dev/null

    # Load and start openzro service
    if [ "$PACKAGE_MANAGER" != "rpm-ostree" ] && [ "$PACKAGE_MANAGER" != "pkg" ]; then
        if ! ${SUDO} openzro service install 2>&1; then
            echo "Openzro service has already been loaded"
        fi
        if ! ${SUDO} openzro service start 2>&1; then
            echo "Openzro service has already been started"
        fi
    fi


    echo "Installation has been finished. To connect, you need to run Openzro by executing the following command:"
    echo ""
    echo "openzro up"
}

version_greater_equal() {
    printf '%s\n%s\n' "$2" "$1" | sort -V -c
}

is_bin_package_manager() {
  if ${SUDO} test -f "$1" && ${SUDO} grep -q "package_manager=bin" "$1" ; then
    return 0
  else
    return 1
  fi
}

stop_running_openzro_ui() {
  OZ_UI_PROC=$(ps -ef | grep "[n]etbird-ui" | awk '{print $2}')
  if [ -n "$OZ_UI_PROC" ]; then
    echo "Openzro UI is running with PID $OZ_UI_PROC. Stopping it..."
    kill -9 "$OZ_UI_PROC"
  fi
}

update_openzro() {
  if is_bin_package_manager "$CONFIG_FILE"; then
    latest_release=$(get_release "latest")
    latest_version=${latest_release#v}
    installed_version=$(openzro version)

    if [ "$latest_version" = "$installed_version" ]; then
      echo "Installed Openzro version ($installed_version) is up-to-date"
      exit 0
    fi

    if version_greater_equal "$latest_version" "$installed_version"; then
      echo "Openzro new version ($latest_version) available. Updating..."
      echo ""
      echo "Initiating Openzro update. This will stop the openzro service and restart it after the update"

      ${SUDO} openzro service stop || true
      ${SUDO} openzro service uninstall || true
      stop_running_openzro_ui
      install_native_binaries

      ${SUDO} openzro service install
      ${SUDO} openzro service start
    fi
  else
     echo "Openzro installation was done using a package manager. Please use your system's package manager to update"
  fi
}

# Checks if SKIP_UI_APP env is set
if [ -z "$SKIP_UI_APP" ]; then
    SKIP_UI_APP=false
else
    if $SKIP_UI_APP; then
      echo "SKIP_UI_APP has been set to true in the environment"
      echo "Openzro UI installation will be omitted based on your preference"
    fi
fi

# Identify OS name and default package manager
if type uname >/dev/null 2>&1; then
	case "$(uname)" in
        Linux)
          OS_TYPE="linux"
          UNAME_OUTPUT="$(uname -a)"
          if echo "$UNAME_OUTPUT" | grep -qi "synology"; then
            OS_NAME="synology"
            INSTALL_DIR="/usr/local/bin"
            PACKAGE_MANAGER="bin"
            SKIP_UI_APP=true
          else
            if [ -f /etc/os-release ]; then
              OS_NAME="$(. /etc/os-release && echo "$ID")"
              INSTALL_DIR="/usr/bin"

              # Allow openzro UI installation for x64 arch only
              if [ "$ARCH" != "amd64" ] && [ "$ARCH" != "arm64" ] \
                  && [ "$ARCH" != "x86_64" ];then
                  SKIP_UI_APP=true
                  echo "Openzro UI installation will be omitted as $ARCH is not a compatible architecture"
              fi

              # Allow openzro UI installation for linux running desktop environment
              if [ -z "$XDG_CURRENT_DESKTOP" ];then
                  SKIP_UI_APP=true
                  echo "Openzro UI installation will be omitted as Linux does not run desktop environment"
              fi

              # Check the availability of a compatible package manager
              if check_use_bin_variable; then
                  PACKAGE_MANAGER="bin"
              elif [ -x "$(command -v apt-get)" ]; then
                  PACKAGE_MANAGER="apt"
                  echo "The installation will be performed using apt package manager"
              elif [ -x "$(command -v dnf)" ]; then
                  PACKAGE_MANAGER="dnf"
                  echo "The installation will be performed using dnf package manager"
              elif [ -x "$(command -v rpm-ostree)" ]; then
                  PACKAGE_MANAGER="rpm-ostree"
                  echo "The installation will be performed using rpm-ostree package manager"
              elif [ -x "$(command -v yum)" ]; then
                  PACKAGE_MANAGER="yum"
                  echo "The installation will be performed using yum package manager"
              elif [ -x "$(command -v pacman)" ]; then
                  PACKAGE_MANAGER="pacman"
                  echo "The installation will be performed using pacman package manager"
              fi

            else
              echo "Unable to determine OS type from /etc/os-release"
              exit 1
            fi
          fi


		;;
		Darwin)
            OS_NAME="macos"
			OS_TYPE="darwin"
            INSTALL_DIR="/usr/local/bin"

            # Check the availability of a compatible package manager
            if check_use_bin_variable; then
                PACKAGE_MANAGER="bin"
            else
              PACKAGE_MANAGER="pkg"
            fi
		;;
	esac
fi

UPDATE_FLAG=$1

if [ "${UPDATE_OPENZRO}-x" = "true-x" ]; then
  UPDATE_FLAG="--update"
fi

case "$UPDATE_FLAG" in
    --update)
      update_openzro
    ;;
    *)
      install_openzro
esac
