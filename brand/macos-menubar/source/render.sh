#!/usr/bin/env bash
# Rasterize the menubar source SVGs to 256x256 PNGs in the client's
# asset directory. Pre-trim the transparent canvas border so the
# system-rendered menubar glyph fills ~97% of the 22pt slot instead
# of the 70% we shipped before PR #85 and the 94% PR #85 reached.
#
# The PIL trim step is what tightens the canvas: rsvg-convert
# faithfully renders the 64-unit viewBox at 256x256, but the shield
# path only occupies x:9-55 / y:5-59 of that grid, so the output
# carries ~28% transparent margin we don't want.
#
# Run from the repo root:
#   ./brand/macos-menubar/source/render.sh

set -euo pipefail

repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
src="$repo_root/brand/macos-menubar/source"
out="$repo_root/client/ui/assets"
canvas=256
fill=0.97  # target ink-vs-canvas ratio after the trim pass

# state -> source SVG. The "error" state intentionally reuses
# shield-disconnected per the brand README (no dedicated error
# variant shipped yet).
declare -A states=(
  [openzro-systemtray-connected-macos.png]=shield-connected.svg
  [openzro-systemtray-disconnected-macos.png]=shield-disconnected.svg
  [openzro-systemtray-connecting-macos.png]=shield-connecting.svg
  [openzro-systemtray-update-connected-macos.png]=shield-update-connected.svg
  [openzro-systemtray-update-disconnected-macos.png]=shield-update-disconnected.svg
  [openzro-systemtray-error-macos.png]=shield-disconnected.svg
)

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

for png in "${!states[@]}"; do
  svg_name="${states[$png]}"
  svg_path="$src/$svg_name"
  raw="$tmp/raw_${png}"
  rsvg-convert -w "$canvas" -h "$canvas" "$svg_path" -o "$raw"
  python3 - "$raw" "$out/$png" "$canvas" "$fill" <<'PY'
import sys
from PIL import Image
raw, target, canvas, fill = sys.argv[1], sys.argv[2], int(sys.argv[3]), float(sys.argv[4])
img = Image.open(raw).convert("RGBA")
bbox = img.getbbox()
if bbox is None:
    raise SystemExit(f"empty image: {raw}")
crop = img.crop(bbox)
cw, ch = crop.size
scale = (canvas * fill) / max(cw, ch)
nw, nh = int(round(cw * scale)), int(round(ch * scale))
resized = crop.resize((nw, nh), Image.LANCZOS)
sheet = Image.new("RGBA", (canvas, canvas), (0, 0, 0, 0))
sheet.paste(resized, ((canvas - nw) // 2, (canvas - nh) // 2), resized)
sheet.save(target, optimize=True)
print(f"  {target}: {cw}x{ch} -> {nw}x{nh} ({max(nw,nh)/canvas*100:.0f}% fill)")
PY
done

echo "done."
