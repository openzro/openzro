#!/usr/bin/env python3
"""Regenerate client/ui/assets/ from brand/openzro-icon.svg.

Visual model — NetBird-style overlay badges:
  - The base disc is ALWAYS brand violet with the Z punch-out, so
    users always recognise the openZro mark in the tray. The disc is
    NOT recoloured per state.
  - Connection state goes in a small badge in the BOTTOM-RIGHT corner:
      - disconnected — no badge (plain brand mark)
      - connecting   — amber dots (•••)
      - connected    — green check (✓)
      - error        — red bang (!)
  - "Update available" is a separate badge in the TOP-RIGHT corner,
    matched in size to the state badge: light-blue circle with a
    white up-arrow (↑). Composes with the state badge — both
    visible at once when relevant.

Run from repo root:

    python3 scripts/generate-client-icons.py

Requires: rsvg-convert, ImageMagick (`magick`).
"""
from __future__ import annotations

import shutil
import subprocess
from dataclasses import dataclass
from pathlib import Path
from textwrap import dedent

REPO = Path(__file__).resolve().parent.parent
OUT = REPO / "client" / "ui" / "assets"

# Z-shape paths copied verbatim from brand/openzro-icon.svg so the
# rendered icons match the canonical brand mark exactly.
Z_PATH = (
    "m 42.69949,50.729586 c -5.246771,-1.783072 -10.782457,-1.832655 "
    "-16.63185,0 -0.668227,0.02122 -0.999265,-0.317022 -0.478323,-0.97503 "
    "L 47.71668,18.700815 c 0,0 0.157573,-0.196687 0.354413,-0.298049 "
    "0.147255,-0.07583 0.295663,-0.0759 0.450161,0.0028 0.163897,0.08346 "
    "0.281363,0.233899 0.343513,0.325372 5.118536,7.533388 8.852414,"
    "20.216321 -4.595268,31.484113 -0.587268,0.492072 -0.882773,0.514556 "
    "-1.57001,0.514557 z"
)

# Brand palette — used for every state's base disc. Identity stays
# constant; state is signalled by the badge overlay.
BRAND_LIGHT = ("#8b5cf6", "#5b21b6")  # Tailwind violet-500 → violet-800
BRAND_DARK = ("#a78bfa", "#6d28d9")   # violet-400 → violet-700


@dataclass(frozen=True)
class Badge:
    """Bottom-right corner badge that signals connection state."""
    color: str               # stroke colour (Tailwind palette)
    symbol_d: str            # SVG path data for the symbol
    stroke_width: int = 8    # override for symbols that look thinner
                             # at the global default (e.g. diagonal
                             # ✓ visually thins vs. vertical strokes)


# Symbol paths drawn directly on the brand disc with a white halo
# for contrast. Sized large (16-20 viewBox units) so they read
# clearly even at the 16px tray-icon size after Lanczos downscaling.
_CHECK_D_BOTTOM = "M 39 50 L 47 58 L 59 41"                       # ✓ bottom-right
_UPARROW_D_TOP  = "M 50 2 L 50 24 M 40 12 L 50 2 L 60 12"         # ↑ top-right
_BANG_D = "M 50 38 L 50 54 M 50 60 L 50 60.1"                     # ! bottom-right
_DOTS_D = "M 40 50 L 40 50 M 50 50 L 50 50 M 60 50 L 60 50"       # ••• bottom-right

# Bottom-right corner — connection state. Diagonal symbols get a
# bumped stroke so they read as visually-equivalent weight to the
# vertical/horizontal strokes of the others.
BADGES: dict[str, Badge | None] = {
    "disconnected": None,                                              # plain brand mark
    "connecting":   Badge("#fbbf24", _DOTS_D),                         # amber-400
    "connected":    Badge("#10b981", _CHECK_D_BOTTOM, stroke_width=11),# emerald-500 (thicker — diagonal)
    "error":        Badge("#ef4444", _BANG_D),                         # red-500
}
TRAY_STATES = list(BADGES.keys())

# Top-right corner — update available. Same size as state badge so
# the two corners look balanced when both fire (connected + update).
# Light blue (sky-400) = same hue NetBird upstream used for the
# update arrow; reads as "update available" universally and doesn't
# clash with violet brand or green connected badges.
UPDATE_BADGE = Badge("#38bdf8", _UPARROW_D_TOP)        # sky-400

# update-available variants are emitted only for connection states
# the daemon is likely to be in when the version checker fires.
UPDATE_STATES = ["connected", "disconnected"]

# Geometry.
BADGE_R = "13"
STATE_BADGE_CX,  STATE_BADGE_CY  = "50", "50"   # bottom-right
UPDATE_BADGE_CX, UPDATE_BADGE_CY = "50", "14"   # top-right


def write_svg(path: Path, brand: tuple[str, str], badge: Badge | None,
              with_update_dot: bool, monochrome_black: bool) -> None:
    """Write a single SVG: brand-violet base disc + optional badge.

    monochrome_black paints the disc solid black (Apple status-bar
    template-image style). Template images are tinted by macOS
    automatically based on the user's appearance — colour badges
    can't survive that, so we draw the badge in black too and rely
    on macOS' per-state title text instead. Practical compromise.
    """
    # Mask: white = visible disc, black = transparent cutout for the
    # Z sails. Same approach as the canonical brand SVG.
    mask = dedent(f"""\
        <mask id="z-cutout">
          <rect width="64" height="64" fill="#ffffff"/>
          <path fill="#000000" d="{Z_PATH}"/>
          <g transform="rotate(180 32 32)">
            <path fill="#000000" d="{Z_PATH}"/>
          </g>
        </mask>""")

    if monochrome_black:
        defs = f"<defs>{mask}</defs>"
        disc = (
            f'<circle cx="32" cy="32" r="28" fill="#000000" '
            f'mask="url(#z-cutout)"/>'
        )
    else:
        c1, c2 = brand
        defs = dedent(f"""\
            <defs>
              <linearGradient id="g" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0" stop-color="{c1}"/>
                <stop offset="1" stop-color="{c2}"/>
              </linearGradient>
              {mask}
            </defs>""")
        disc = (
            f'<circle cx="32" cy="32" r="28" fill="url(#g)" '
            f'mask="url(#z-cutout)"/>'
        )

    def render_badge(b: Badge, _cx: str, _cy: str) -> str:
        """Symbol drawn directly on the brand disc. NetBird-style:
        no circle backdrop, no white halo — just the coloured stroke.
        Visibility comes from colour contrast (green ✓ vs violet,
        blue ↑ vs violet, etc.).
        """
        stroke = "#000000" if monochrome_black else b.color
        return (
            f'<path d="{b.symbol_d}" fill="none" stroke="{stroke}" '
            f'stroke-width="{b.stroke_width}" stroke-linecap="round" '
            f'stroke-linejoin="round"/>'
        )

    badge_svg = render_badge(badge, STATE_BADGE_CX, STATE_BADGE_CY) if badge else ""
    update_svg = render_badge(UPDATE_BADGE, UPDATE_BADGE_CX, UPDATE_BADGE_CY) if with_update_dot else ""

    svg = dedent(f"""\
        <svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 64 64">
          {defs}
          {disc}
          {badge_svg}
          {update_svg}
        </svg>""")
    path.write_text(svg, encoding="utf-8")


def render_png(svg: Path, png: Path, size: int = 256) -> None:
    subprocess.run(
        ["rsvg-convert", "-w", str(size), "-h", str(size),
         str(svg), "-o", str(png)],
        check=True,
    )


def render_ico(svg: Path, ico: Path, sizes: list[int]) -> None:
    """Build a multi-resolution ICO from the SVG."""
    tmp = ico.parent / f".tmp-{ico.stem}"
    tmp.mkdir(exist_ok=True)
    pngs = []
    for sz in sizes:
        p = tmp / f"{sz}.png"
        render_png(svg, p, sz)
        pngs.append(str(p))
    subprocess.run(["magick", *pngs, str(ico)], check=True)
    shutil.rmtree(tmp)


def main() -> None:
    OUT.mkdir(exist_ok=True)
    tmp = REPO / ".tmp-icons"
    tmp.mkdir(exist_ok=True)

    # 1. Application icon — brand violet, no badge. Always violet
    #    regardless of daemon state (this is the icon Windows /
    #    macOS / GNOME show in the Start Menu / Launchpad / dock).
    write_svg(tmp / "app.svg", BRAND_LIGHT, None, False, False)
    render_png(tmp / "app.svg", OUT / "openzro.png", 256)
    render_ico(tmp / "app.svg", OUT / "openzro.ico",
               [16, 32, 48, 64, 128, 256])

    # 2. In-app indicators — used in the main window UI to show the
    #    current state at large size. Connected / disconnected pair
    #    are the long-lived ones; connecting / transmitting / error
    #    are flashy small-size things that live in the tray.
    for state in ("connected", "disconnected"):
        svg = tmp / f"in-app-{state}.svg"
        write_svg(svg, BRAND_LIGHT, BADGES[state], False, False)
        render_png(svg, OUT / f"{state}.png", 256)

    # 3. Per-state system-tray icons in 3 themes × {plain, +update}.
    tray_ico_sizes = [16, 24, 32, 48, 64, 128, 256]

    state_groups: list[tuple[str, str, bool]] = [
        (s, s, False) for s in TRAY_STATES
    ]
    for s in UPDATE_STATES:
        state_groups.append((f"update-{s}", s, True))

    for name, state, with_dot in state_groups:
        badge = BADGES[state]

        # Light theme PNG/ICO
        light_svg = tmp / f"tray-{name}.svg"
        write_svg(light_svg, BRAND_LIGHT, badge, with_dot, False)
        render_png(light_svg, OUT / f"openzro-systemtray-{name}.png", 256)
        render_ico(light_svg, OUT / f"openzro-systemtray-{name}.ico",
                   tray_ico_sizes)

        # Dark theme PNG/ICO
        dark_svg = tmp / f"tray-{name}-dark.svg"
        write_svg(dark_svg, BRAND_DARK, badge, with_dot, False)
        render_png(dark_svg, OUT / f"openzro-systemtray-{name}-dark.png", 256)
        render_ico(dark_svg, OUT / f"openzro-systemtray-{name}-dark.ico",
                   tray_ico_sizes)

        # macOS template (black silhouette).
        macos_svg = tmp / f"tray-{name}-macos.svg"
        write_svg(macos_svg, BRAND_LIGHT, badge, with_dot, True)
        render_png(macos_svg, OUT / f"openzro-systemtray-{name}-macos.png", 256)

    shutil.rmtree(tmp)
    print(f"Wrote {len(list(OUT.glob('*')))} assets to {OUT}")


if __name__ == "__main__":
    main()
