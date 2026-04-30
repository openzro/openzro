#!/usr/bin/env bash
#
# publish-packages.sh — build APT and YUM/DNF/zypper repos from the
# .deb / .rpm artifacts of a goreleaser release, sign the metadata
# with our GPG key, and push the result to the `gh-pages` branch
# (which is served at pkg.openzro.io via GitHub Pages + Cloudflare DNS).
#
# Inputs (env vars):
#   VERSION              the tag we're publishing, e.g. "v0.1.0-alpha.3"
#   GPG_PRIVATE_KEY      PEM-armored private key (passed as a secret)
#   GPG_PASSPHRASE       passphrase for the key — set to an empty
#                        string if the key has no passphrase (the
#                        threat model for a CI signing key doesn't
#                        meaningfully benefit from passphrase
#                        protection when both the key and passphrase
#                        live in the same secret store)
#   GITHUB_TOKEN         to push to gh-pages
#
# Layout produced (rooted at $REPO_ROOT, which becomes pkg.openzro.io):
#
#   /openzro-archive-keyring.gpg     — public key (for apt-key)
#   /openzro-archive-key.asc         — armored public key (for rpm-key)
#   /apt/                            — APT repo
#     dists/stable/Release           — signed metadata (.gpg + InRelease)
#     dists/stable/main/binary-{amd64,arm64,armhf,i386}/Packages{,.gz}
#     pool/main/o/openzro/*.deb
#   /rpm/                            — YUM/DNF repo (also used by zypper)
#     {x86_64,aarch64,i686,armv6hl}/repodata/{repomd,primary,…}.xml
#     {x86_64,aarch64,i686,armv6hl}/*.rpm
#   /CNAME                           — pkg.openzro.io
#   /index.html                      — landing page
#
# The corresponding sources.list / yum.repo / zypp.repo URLs:
#
#   deb [signed-by=/usr/share/keyrings/openzro-archive-keyring.gpg] \
#       https://pkg.openzro.io/apt stable main
#   baseurl=https://pkg.openzro.io/rpm/$basearch
#   gpgkey=https://pkg.openzro.io/openzro-archive-key.asc
#
# This script is idempotent — re-running it for the same VERSION
# updates the existing pool entries and metadata in place. Old pool
# files for older versions are kept (apt/yum want history) until a
# separate retention policy prunes them.

set -euo pipefail

: "${VERSION:?VERSION is required (e.g. v0.1.0-alpha.3)}"
: "${GPG_PRIVATE_KEY:?GPG_PRIVATE_KEY is required}"
GPG_PASSPHRASE="${GPG_PASSPHRASE-}"   # empty allowed

WORK="${WORK:-$(pwd)/.pkg-publish}"
mkdir -p "$WORK/downloads" "$WORK/repo/apt" "$WORK/repo/rpm" "$WORK/repo/pacman"

# ----------------------------------------------------------------------
# 1. Import the signing key into a scratch GPG home so we don't pollute
#    the host's keychain.
# ----------------------------------------------------------------------
export GNUPGHOME="$WORK/gnupg"
mkdir -p "$GNUPGHOME"
chmod 700 "$GNUPGHOME"

echo "$GPG_PRIVATE_KEY" | gpg --batch --import 2>&1
GPG_KEY_ID="$(gpg --list-secret-keys --keyid-format=long | awk '/^sec/ {split($2, a, "/"); print a[2]; exit}')"
echo "Imported signing key $GPG_KEY_ID"

# Export the public key in two forms — binary keyring (for `apt-key add`
# style use, with [signed-by=…]) and ASCII armored (for `rpm --import`).
gpg --export "$GPG_KEY_ID" > "$WORK/repo/openzro-archive-keyring.gpg"
gpg --armor --export "$GPG_KEY_ID" > "$WORK/repo/openzro-archive-key.asc"

# ----------------------------------------------------------------------
# 2. Pull the release assets from the GitHub Release for VERSION.
#    The .deb / .rpm / .pkg.tar.zst feed the apt/yum/pacman repos.
#    The .msi / darwin .pkg / darwin .tar.gz feed the stable
#    download URLs that the dashboard's "Setup" modal links to
#    (so users follow https://pkg.openzro.io/<os>/openzro.<ext>
#    instead of GitHub release-asset URLs that change with every
#    tag — see step 7 below).
# ----------------------------------------------------------------------
echo "Downloading release assets for $VERSION..."
gh release download "$VERSION" \
    --repo openzro/openzro \
    --pattern '*.deb' \
    --pattern '*.rpm' \
    --pattern '*.pkg.tar.zst' \
    --pattern '*_windows_amd64.msi' \
    --pattern '*_darwin_universal.pkg' \
    --pattern 'openzro-ui_*_darwin_universal.tar.gz' \
    --dir "$WORK/downloads"

# ----------------------------------------------------------------------
# 3. Build the APT repo with aptly. Aptly handles the pool layout,
#    Packages indexes, and the Release / InRelease signing in one go.
# ----------------------------------------------------------------------
APTLY_HOME="$WORK/aptly"
mkdir -p "$APTLY_HOME"

cat > "$WORK/aptly.conf" <<EOF
{
  "rootDir": "$APTLY_HOME",
  "downloadConcurrency": 4,
  "architectures": ["amd64", "arm64", "armhf", "i386"],
  "gpgDisableSign": false,
  "gpgDisableVerify": false
}
EOF

if ! aptly -config="$WORK/aptly.conf" repo show openzro >/dev/null 2>&1; then
    aptly -config="$WORK/aptly.conf" repo create \
        -distribution=stable -component=main openzro
fi

aptly -config="$WORK/aptly.conf" repo add -force-replace openzro \
    "$WORK/downloads"/*.deb

aptly -config="$WORK/aptly.conf" publish drop stable 2>/dev/null || true
APTLY_PASS_FLAG=()
if [ -n "$GPG_PASSPHRASE" ]; then
    APTLY_PASS_FLAG=(-passphrase="$GPG_PASSPHRASE")
fi
aptly -config="$WORK/aptly.conf" publish repo \
    -gpg-key="$GPG_KEY_ID" \
    -batch "${APTLY_PASS_FLAG[@]}" \
    openzro

cp -a "$APTLY_HOME/public/." "$WORK/repo/apt/"

# ----------------------------------------------------------------------
# 4. Build the YUM/DNF/zypper repo with createrepo_c. One repo per
#    arch since RPM metadata is per-arch.
# ----------------------------------------------------------------------
for ARCH in x86_64 aarch64 i686 armv6hl; do
    mkdir -p "$WORK/repo/rpm/$ARCH"
done

# goreleaser produces files like openzro_v0.1.0-alpha.3_linux_amd64.rpm
# — translate Linux arch names to RPM arch names.
declare -A ARCH_MAP=(
    [linux_amd64]=x86_64
    [linux_arm64]=aarch64
    [linux_386]=i686
    [linux_armv6]=armv6hl
)

for f in "$WORK/downloads"/*.rpm; do
    [ -e "$f" ] || continue
    base="$(basename "$f")"
    matched=""
    for k in "${!ARCH_MAP[@]}"; do
        if [[ "$base" == *"$k.rpm"* ]] || [[ "$base" == *"$k"*.rpm ]]; then
            matched="${ARCH_MAP[$k]}"
            break
        fi
    done
    [ -z "$matched" ] && { echo "WARN: cannot map arch for $base"; continue; }
    cp "$f" "$WORK/repo/rpm/$matched/"
done

GPG_SIGN_FLAGS=(--batch --yes --pinentry-mode loopback
                --default-key "$GPG_KEY_ID" --detach-sign --armor)
if [ -n "$GPG_PASSPHRASE" ]; then
    GPG_SIGN_FLAGS+=(--passphrase "$GPG_PASSPHRASE")
fi

# rpmsign needs ~/.rpmmacros telling it how to sign + which key.
# We use a custom __gpg_sign_cmd that pipes the passphrase via
# loopback (the default macro expects an interactive pinentry,
# which CI doesn't have). The passphrase is interpolated into the
# macro at sign time via --define '_gpg_pass …'.
mkdir -p "$HOME"
cat > "$HOME/.rpmmacros" <<RPMMACROS
%_signature gpg
%_gpg_name $GPG_KEY_ID
%__gpg_sign_cmd %{__gpg} gpg --batch --yes --pinentry-mode loopback --passphrase "%{_gpg_pass}" --no-secmem-warning --no-tty --default-key %{_gpg_name} --detach-sign --output %{__signature_filename} %{__plaintext_filename}
RPMMACROS

for ARCH in x86_64 aarch64 i686 armv6hl; do
    if compgen -G "$WORK/repo/rpm/$ARCH/*.rpm" >/dev/null; then
        # Sign each .rpm with the same key as the repo metadata.
        # Without this, dnf/yum reject the package with "not signed"
        # when the operator's repo file has gpgcheck=1 (the recommended
        # setting). Pre-sign the packages so install.sh's add_rpm_repo
        # can use gpgcheck=1, repo_gpgcheck=1 (full chain verified).
        rpmsign --define="_gpg_pass $GPG_PASSPHRASE" \
                --addsign "$WORK/repo/rpm/$ARCH"/*.rpm

        # Build the repo metadata index AFTER signing so the
        # createrepo_c index records the post-sign sha256.
        createrepo_c --update "$WORK/repo/rpm/$ARCH"

        # Sign the repodata/repomd.xml (yum/dnf/zypper verify this
        # via repo_gpgcheck=1).
        gpg "${GPG_SIGN_FLAGS[@]}" "$WORK/repo/rpm/$ARCH/repodata/repomd.xml"
    fi
done

# ----------------------------------------------------------------------
# 5. Build the pacman repo. goreleaser nfpms with format `archlinux`
#    produces `openzro_<v>_linux_<arch>.pkg.tar.zst` files which we
#    arrange into pkg.openzro.io/pacman/<arch>/ + a signed `openzro.db`
#    repo database. This avoids the AUR makepkg path entirely (no
#    second account, no review queue) and parallels the apt + rpm flows.
#
#    `repo-add` is a bash script shipped in pacman, which Ubuntu
#    doesn't have — we run it inside a transient archlinux Docker
#    container. Each package gets a detached gpg sig done on the host
#    (so the sig is keyed by our existing GPG_KEY_ID); the container
#    only does `repo-add` + signs the resulting db file.
# ----------------------------------------------------------------------
declare -A PACMAN_ARCH_MAP=(
    [linux_amd64]=x86_64
    [linux_arm64]=aarch64
)

PACMAN_ANY_FOUND=""
for f in "$WORK/downloads"/*.pkg.tar.zst; do
    [ -e "$f" ] || continue
    PACMAN_ANY_FOUND=1
    base="$(basename "$f")"
    matched=""
    for k in "${!PACMAN_ARCH_MAP[@]}"; do
        if [[ "$base" == *"$k"*.pkg.tar.zst ]]; then
            matched="${PACMAN_ARCH_MAP[$k]}"
            break
        fi
    done
    [ -z "$matched" ] && { echo "WARN: cannot map arch for $base"; continue; }
    mkdir -p "$WORK/repo/pacman/$matched"
    cp "$f" "$WORK/repo/pacman/$matched/"
done

if [ -n "$PACMAN_ANY_FOUND" ]; then
    # Detached-sign each .pkg.tar.zst on the host so pacman's
    # SigLevel = Required Verify checks pass.
    for ARCH_DIR in "$WORK/repo/pacman"/*/; do
        for pkg in "$ARCH_DIR"*.pkg.tar.zst; do
            [ -e "$pkg" ] || continue
            gpg --batch --yes --pinentry-mode loopback \
                ${GPG_PASSPHRASE:+--passphrase "$GPG_PASSPHRASE"} \
                --default-key "$GPG_KEY_ID" \
                --detach-sign --no-armor \
                --output "${pkg}.sig" "$pkg"
        done
    done

    # `repo-add` lives in pacman; spin up archlinux:latest, mount the
    # arch dirs and the gnupg home, and let it build + sign the db.
    for ARCH in x86_64 aarch64; do
        ARCH_DIR="$WORK/repo/pacman/$ARCH"
        [ -d "$ARCH_DIR" ] || continue
        compgen -G "$ARCH_DIR/*.pkg.tar.zst" >/dev/null || continue

        # Two-phase signing because repo-add's --sign uses the
        # container's plain `gpg` binary (it doesn't honor the $GPG
        # env override we tried earlier). gpg with passphrase from
        # an unrelated GNUPGHOME mounts ends up failing silently in
        # CI — the "Signing database" log line prints, no .sig file
        # appears, repo-add exits 0. Workaround: build the db
        # UNSIGNED inside the container, then detached-sign the
        # resulting db + files OUTSIDE the container with the same
        # gpg flags we already use on the .pkg.tar.zst files.
        docker run --rm \
            -v "$ARCH_DIR:/repo" \
            -w /repo \
            archlinux:latest \
            bash -c '
                set -eu
                repo-add /repo/openzro.db.tar.gz /repo/*.pkg.tar.zst
            '

        # Now detached-sign the freshly-built db + files on the host
        # using the same loopback-passphrase pattern that worked for
        # the .pkg.tar.zst signatures earlier in this script. pacman
        # follows the symlinks (`openzro.db -> openzro.db.tar.gz`,
        # `openzro.db.sig -> openzro.db.tar.gz.sig`), so we only
        # sign the .tar.gz files and let the existing symlinks point
        # at the resulting .sig files.
        for db in openzro.db.tar.gz openzro.files.tar.gz; do
            [ -f "$ARCH_DIR/$db" ] || continue
            gpg --batch --yes --pinentry-mode loopback \
                ${GPG_PASSPHRASE:+--passphrase "$GPG_PASSPHRASE"} \
                --default-key "$GPG_KEY_ID" \
                --detach-sign --no-armor \
                --output "$ARCH_DIR/${db}.sig" "$ARCH_DIR/$db"
            # repo-add creates the openzro.db / openzro.files
            # symlinks pointing to the .tar.gz; pacman expects
            # corresponding `.sig` symlinks for the unsuffixed
            # form too. Recreate them here.
            short="${db%.tar.gz}"
            ln -sfn "${db}.sig" "$ARCH_DIR/${short}.sig"
        done
    done
fi

# ----------------------------------------------------------------------
# 6. install.sh — distro-detecting bootstrap script the dashboard's
#    SetupModal points at (`curl -fsSL https://pkg.openzro.io/install.sh
#    | sh`). Lives in the source tree at release_files/install.sh and
#    is the canonical client install path for distros our packaged
#    repos don't cover (Arch/AUR + binary fallback for everything else).
# ----------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT_GIT="$(cd "$SCRIPT_DIR/.." && pwd)"
if [ -f "$REPO_ROOT_GIT/release_files/install.sh" ]; then
    cp "$REPO_ROOT_GIT/release_files/install.sh" "$WORK/repo/install.sh"
    chmod 0755 "$WORK/repo/install.sh"
    echo "Copied install.sh ($(wc -l < "$WORK/repo/install.sh") lines) to repo root."
else
    echo "WARN: release_files/install.sh not found at $REPO_ROOT_GIT — skipping."
fi

# ----------------------------------------------------------------------
# 7. Stable download paths for the dashboard's Setup modal.
#    The dashboard's WindowsTab / MacOSTab link to these URLs instead
#    of GitHub Release asset URLs (which change every tag). Users
#    bookmark / paste these URLs and they keep working forever.
#
#    Layout:
#      /windows/openzro.msi              ← one-click installer (CLI+UI+wintun)
#      /macos/openzro.pkg                ← double-click installer (CLI+UI .app+launchd)
#      /macos/openzro-ui.tar.gz          ← UI .app tarball alone
#
#    Linux users go through apt/dnf/pacman (versioned by repo) so we
#    don't need a corresponding "stable .deb" path.
# ----------------------------------------------------------------------
mkdir -p "$WORK/repo/windows" "$WORK/repo/macos"

# Strip the leading "v" so the asset filename matches what the
# .goreleaser configs emit ("0.53.1-alpha.8_windows_amd64.msi", not
# "v0.53.1-alpha.8_…").
VER_NOV="${VERSION#v}"

if [ -f "$WORK/downloads/openzro_${VER_NOV}_windows_amd64.msi" ]; then
    cp "$WORK/downloads/openzro_${VER_NOV}_windows_amd64.msi" \
        "$WORK/repo/windows/openzro.msi"
    echo "Staged windows/openzro.msi → $VERSION"
else
    echo "WARN: windows MSI for $VERSION missing — skipping stable URL."
fi

if [ -f "$WORK/downloads/openzro_${VER_NOV}_darwin_universal.pkg" ]; then
    cp "$WORK/downloads/openzro_${VER_NOV}_darwin_universal.pkg" \
        "$WORK/repo/macos/openzro.pkg"
    echo "Staged macos/openzro.pkg → $VERSION"
else
    echo "WARN: macOS PKG for $VERSION missing — skipping stable URL."
fi

if [ -f "$WORK/downloads/openzro-ui_${VER_NOV}_darwin_universal.tar.gz" ]; then
    cp "$WORK/downloads/openzro-ui_${VER_NOV}_darwin_universal.tar.gz" \
        "$WORK/repo/macos/openzro-ui.tar.gz"
    echo "Staged macos/openzro-ui.tar.gz → $VERSION"
fi

# Also drop a tiny latest.json so the dashboard can show
# "Download openZro v0.53.1-alpha.X" without round-tripping the
# GitHub API. Cheap to fetch, hosted on the same origin as the
# download — no rate limit, no extra DNS lookup.
cat > "$WORK/repo/latest.json" <<JSON
{"tag":"$VERSION","version":"$VER_NOV","updated":"$(date -u +%Y-%m-%dT%H:%M:%SZ)"}
JSON
echo "Wrote latest.json → $VERSION"

# ----------------------------------------------------------------------
# 8. Static index + CNAME for GitHub Pages.
# ----------------------------------------------------------------------
echo "pkg.openzro.io" > "$WORK/repo/CNAME"

cat > "$WORK/repo/index.html" <<'HTML'
<!doctype html>
<meta charset="utf-8">
<title>openZro package repository</title>
<style>
  body { font-family: 'Geist', system-ui, sans-serif; max-width: 720px; margin: 4em auto; padding: 0 1em; color: #0f0a1f; }
  h1 { color: #7c3aed; letter-spacing: -0.025em; }
  code { background: #f5f3ff; padding: 0.15em 0.4em; border-radius: 4px; }
  pre { background: #f5f3ff; padding: 1em; border-radius: 8px; overflow-x: auto; }
  a { color: #7c3aed; }
</style>
<h1>openZro package repository</h1>
<p>This domain serves signed APT, YUM, DNF, and zypper repositories
for openZro releases. See the
<a href="https://docs.openzro.io/get-started/install/linux">Linux
install guide</a> for the recommended setup.</p>

<h2>APT (Debian, Ubuntu)</h2>
<pre>curl -fsSL https://pkg.openzro.io/openzro-archive-keyring.gpg \
  | sudo tee /usr/share/keyrings/openzro-archive-keyring.gpg > /dev/null
echo 'deb [signed-by=/usr/share/keyrings/openzro-archive-keyring.gpg] \
https://pkg.openzro.io/apt stable main' \
  | sudo tee /etc/apt/sources.list.d/openzro.list
sudo apt-get update
sudo apt-get install openzro</pre>

<h2>YUM / DNF (RHEL, Fedora, Amazon Linux)</h2>
<pre>sudo tee /etc/yum.repos.d/openzro.repo &lt;&lt;EOF
[openzro]
name=openZro
baseurl=https://pkg.openzro.io/rpm/\$basearch
enabled=1
gpgcheck=1
gpgkey=https://pkg.openzro.io/openzro-archive-key.asc
EOF
sudo dnf install openzro</pre>

<h2>zypper (openSUSE, SLES)</h2>
<pre>sudo zypper addrepo \
  https://pkg.openzro.io/rpm/x86_64/openzro.repo openzro
sudo rpm --import https://pkg.openzro.io/openzro-archive-key.asc
sudo zypper install openzro</pre>

<h2>pacman (Arch, CachyOS, Manjaro, EndeavourOS)</h2>
<pre>curl -fsSL https://pkg.openzro.io/openzro-archive-key.asc \
  | sudo pacman-key -a -
sudo pacman-key --lsign-key dev@openzro.io
sudo tee -a /etc/pacman.conf &lt;&lt;EOF
[openzro]
SigLevel = Required DatabaseRequired
Server = https://pkg.openzro.io/pacman/\$arch
EOF
sudo pacman -Sy
sudo pacman -S openzro</pre>

<h2>One-line install (covers Gentoo, Alpine, …)</h2>
<p>For distros not covered by the package repos above, the
<code>install.sh</code> script auto-detects your distro and falls
through APT, YUM/DNF, zypper, pacman, or the binary tarball:</p>
<pre>curl -fsSL https://pkg.openzro.io/install.sh | sh</pre>
<p>The script source is at
<a href="https://github.com/openzro/openzro/blob/main/release_files/install.sh"><code>release_files/install.sh</code></a>
in the openzro/openzro repo — review before piping to a shell, as
always.</p>

<p>Source for the repository tooling lives under
<code>scripts/publish-packages.sh</code> in
<a href="https://github.com/openzro/openzro">openzro/openzro</a>.</p>
HTML

echo "Repo built at $WORK/repo. Next step: push to gh-pages branch."
