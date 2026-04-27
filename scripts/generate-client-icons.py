#!/usr/bin/env python3
"""Regenerate client/ui/assets/ from brand/openzro-icon.svg.

The client UI ships ~32 PNG / ICO assets for system-tray icons in
multiple states (connected / disconnected / connecting / error,
plus "update available" variants) and themes (light / dark / macOS).
This script renders all of them from a single SVG template so the
brand stays in lockstep with brand/openzro-icon.svg.

Run from repo root:

    python3 scripts/generate-client-icons.py

Requires: rsvg-convert, ImageMagick (`magick`).
"""
from __future__ import annotations

import os
import shutil
import subprocess
import sys
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


@dataclass(frozen=True)
class State:
    name: str
    # gradient stops for light theme
    light: tuple[str, str]
    # gradient stops for dark theme
    dark: tuple[str, str]


# Colour palette per state. The light/dark pair governs both the
# regular and -dark variants. The macOS variant is rendered as a
# solid black silhouette (Apple's status-bar template-image style).
STATES: list[State] = [
    State("connected",    ("#8b5cf6", "#5b21b6"), ("#a78bfa", "#6d28d9")),
    State("disconnected", ("#9ca3af", "#4b5563"), ("#d1d5db", "#6b7280")),
    State("connecting",   ("#fbbf24", "#d97706"), ("#fcd34d", "#b45309")),
    State("error",        ("#ef4444", "#b91c1c"), ("#fca5a5", "#991b1b")),
]

# `update-<state>` icons reuse the state's palette and add a small
# accent dot in the corner to signal "update available".
UPDATE_STATES = ["connected", "disconnected"]

# Colour and position of the update accent dot (in the SVG's 64×64
# coordinate space). Bright violet — visible on both light and dark
# backgrounds.
UPDATE_DOT = ("52", "12", "9", "#7c3aed", "#ffffff")  # cx, cy, r, fill, stroke


def write_svg(path: Path, fill_grad_id: str, stops: tuple[str, str],
              with_update_dot: bool = False,
              monochrome_black: bool = False) -> None:
    """Write a single state SVG.

    The Z shape is ALWAYS punched out of the disc (transparent),
    matching the canonical brand mark. monochrome_black paints the
    disc solid black — used for the macOS status-bar template image.
    """
    # Mask: white = visible disc, black = transparent cutout for the
    # Z sails. Same approach as the canonical brand SVG's dark-mode
    # rendering, applied to every variant for visual consistency.
    mask = dedent(f"""\
        <mask id="z-cutout-{fill_grad_id}">
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
            f'mask="url(#z-cutout-{fill_grad_id})"/>'
        )
    else:
        c1, c2 = stops
        defs = dedent(f"""\
            <defs>
              <linearGradient id="{fill_grad_id}" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0" stop-color="{c1}"/>
                <stop offset="1" stop-color="{c2}"/>
              </linearGradient>
              {mask}
            </defs>""")
        disc = (
            f'<circle cx="32" cy="32" r="28" fill="url(#{fill_grad_id})" '
            f'mask="url(#z-cutout-{fill_grad_id})"/>'
        )

    update_dot = ""
    if with_update_dot:
        cx, cy, r, fill, stroke = UPDATE_DOT
        update_dot = (
            f'<circle cx="{cx}" cy="{cy}" r="{r}" '
            f'fill="{fill}" stroke="{stroke}" stroke-width="1.5"/>'
        )

    svg = dedent(f"""\
        <svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 64 64">
          {defs}
          {disc}
          {update_dot}
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

    # 1. Main app icon (the brand mark used as application icon).
    write_svg(tmp / "app.svg", "g", STATES[0].light)
    render_png(tmp / "app.svg", OUT / "openzro.png", 256)
    render_ico(tmp / "app.svg", OUT / "openzro.ico",
               [16, 32, 48, 64, 128, 256])

    # 2. In-app connection-state icons (large 256px, used in the
    #    main window UI).
    write_svg(tmp / "connected-large.svg", "g", STATES[0].light)
    render_png(tmp / "connected-large.svg", OUT / "connected.png", 256)
    write_svg(tmp / "disconnected-large.svg", "g", STATES[1].light)
    render_png(tmp / "disconnected-large.svg", OUT / "disconnected.png", 256)

    # 3. Per-state system-tray icons in 3 themes × 4 (or 6 for update)
    #    formats.
    state_groups: list[tuple[str, State, bool]] = [
        (s.name, s, False) for s in STATES
    ]
    for state_name in UPDATE_STATES:
        s = next(s for s in STATES if s.name == state_name)
        state_groups.append((f"update-{state_name}", s, True))

    tray_ico_sizes = [16, 24, 32, 48, 64, 128, 256]

    for name, state, with_dot in state_groups:
        # Light theme PNG/ICO
        light_svg = tmp / f"tray-{name}.svg"
        write_svg(light_svg, "g", state.light, with_update_dot=with_dot)
        render_png(light_svg, OUT / f"openzro-systemtray-{name}.png", 256)
        render_ico(light_svg, OUT / f"openzro-systemtray-{name}.ico",
                   tray_ico_sizes)

        # Dark theme PNG/ICO
        dark_svg = tmp / f"tray-{name}-dark.svg"
        write_svg(dark_svg, "g", state.dark, with_update_dot=with_dot)
        render_png(dark_svg, OUT / f"openzro-systemtray-{name}-dark.png", 256)
        render_ico(dark_svg, OUT / f"openzro-systemtray-{name}-dark.ico",
                   tray_ico_sizes)

        # macOS template (black silhouette, Z punched out)
        macos_svg = tmp / f"tray-{name}-macos.svg"
        write_svg(macos_svg, "g", state.light,
                  monochrome_black=True, with_update_dot=with_dot)
        render_png(macos_svg, OUT / f"openzro-systemtray-{name}-macos.png", 256)

    shutil.rmtree(tmp)
    print(f"Wrote {len(list(OUT.glob('*')))} assets to {OUT}")


if __name__ == "__main__":
    main()
