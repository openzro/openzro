#!/usr/bin/env bash
#
# wrap-darwin-app.sh — turn the universal openzro-ui binary into a
# proper macOS .app bundle so it shows up in Launchpad / Dock /
# Spotlight after install (instead of being a bare binary in
# /usr/local/bin/openzro-ui that only terminal users can find).
#
# Called by goreleaser as a post-hook on the universal_binaries
# step (.goreleaser.ui-darwin.yaml). Inputs are env vars passed
# from the goreleaser hook block:
#
#   OPENZRO_BIN  — path to the universal openzro-ui binary
#                  (e.g. dist/openzro-ui-darwin_darwin_all/openzro-ui)
#   OPENZRO_VER  — version string for CFBundleShortVersionString
#                  (e.g. "0.53.1-alpha.9")
#
# Output: <bin_dir>/openZro UI.app/ — a complete bundle ready to
# drop into /Applications/ via the macOS .pkg installer (the
# release_pkg job in .github/workflows/release-binaries.yml moves
# the .app to /Applications/ at install time).
#
# Tools used: sips, iconutil — both ship with macOS, so this
# script must run on the macos-14 GitHub runner. Linux runners
# don't have iconutil and would need libicns or png2icns instead.

set -euo pipefail

: "${OPENZRO_BIN:?OPENZRO_BIN is required}"
: "${OPENZRO_VER:?OPENZRO_VER is required}"

if [ ! -x "$OPENZRO_BIN" ]; then
    echo "wrap-darwin-app: $OPENZRO_BIN is not an executable" >&2
    exit 1
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="$(cd "$(dirname "$OPENZRO_BIN")" && pwd)"
APP="$BIN_DIR/openZro UI.app"
SRC_ICON="$REPO_ROOT/client/ui/assets/openzro.png"

if [ ! -f "$SRC_ICON" ]; then
    echo "wrap-darwin-app: source icon $SRC_ICON missing — run scripts/generate-client-icons.py first" >&2
    exit 1
fi

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

# Move the binary in (goreleaser archives the *.app/ directory; the
# bare binary at the BIN_DIR root would be redundant).
mv "$OPENZRO_BIN" "$APP/Contents/MacOS/openzro-ui"
chmod 0755 "$APP/Contents/MacOS/openzro-ui"

# Drop a stub binary back at OPENZRO_BIN so goreleaser's post-hook
# pipeline doesn't blow up when other steps reference the original
# path. The stub simply execs the real binary inside the .app —
# convenient if anyone wants to call openzro-ui from the terminal.
cat > "$OPENZRO_BIN" <<STUB
#!/bin/sh
exec "\$(dirname "\$0")/openZro UI.app/Contents/MacOS/openzro-ui" "\$@"
STUB
chmod 0755 "$OPENZRO_BIN"

# Info.plist — the minimum macOS needs to recognise the directory
# as an .app. LSUIElement=true makes it a menu-bar-only app (no
# Dock icon, which is what we want for a tray utility).
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>openzro-ui</string>
    <key>CFBundleIdentifier</key>
    <string>io.openzro.ui</string>
    <key>CFBundleName</key>
    <string>openZro UI</string>
    <key>CFBundleDisplayName</key>
    <string>openZro UI</string>
    <key>CFBundleVersion</key>
    <string>${OPENZRO_VER}</string>
    <key>CFBundleShortVersionString</key>
    <string>${OPENZRO_VER}</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleSignature</key>
    <string>????</string>
    <key>CFBundleIconFile</key>
    <string>Icon.icns</string>
    <key>LSUIElement</key>
    <true/>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>LSMinimumSystemVersion</key>
    <string>11.0</string>
</dict>
</plist>
PLIST

# Build Icon.icns from the brand PNG. iconutil expects a specific
# directory layout (icon.iconset with named PNGs); sips downscales.
ICONSET="$BIN_DIR/openzro.iconset"
rm -rf "$ICONSET"
mkdir -p "$ICONSET"
for sz in 16 32 128 256 512; do
    sips -z "$sz" "$sz" "$SRC_ICON" --out "$ICONSET/icon_${sz}x${sz}.png" >/dev/null
    retina=$((sz * 2))
    sips -z "$retina" "$retina" "$SRC_ICON" \
        --out "$ICONSET/icon_${sz}x${sz}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/Icon.icns"
rm -rf "$ICONSET"

echo "wrap-darwin-app: built $APP ($(du -h "$APP/Contents/MacOS/openzro-ui" | awk '{print $1}') binary, $(du -h "$APP/Contents/Resources/Icon.icns" | awk '{print $1}') icon)"
