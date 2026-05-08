# Handoff: openZro macOS Menu Bar — Shield Status Icons

A monochromatic icon family for the openZro desktop daemon's macOS menu
bar (status item). Five status variants cover the daemon's lifecycle:

| Variant | Used as | Rendered when… |
|---|---|---|
| Idle | `openzro-systemtray-disconnected-macos.png`, `openzro-systemtray-error-macos.png` | Daemon off, signed out, or no mesh attached. Error state currently reuses idle pending a dedicated error variant. |
| Connecting | `openzro-systemtray-connecting-macos.png` | Posture check + key exchange in progress |
| Connected | `openzro-systemtray-connected-macos.png` | Peer admitted, traffic flowing |
| Update available | `openzro-systemtray-update-disconnected-macos.png` | New client version ready to install |
| Connected + update | `openzro-systemtray-update-connected-macos.png` | In the mesh, update pending |

## How the icons work (CRITICAL)

Every PNG renders **pure black** with alpha-only shape encoding — the
macOS template-image convention. The `fyne.io/systray` library invokes
`SetTemplateIcon` and the system paints them correctly in both
appearances:

- **Dark appearance** → glyph paints **pure white**
- **Light appearance** → glyph paints **pure black**
- **Tinted accent / Reduce Transparency / accessibility inversions** → all handled by the system

The PNGs in `client/ui/assets/openzro-systemtray-*-macos.png` were
rasterized from the source SVGs (now under `brand/macos-menubar/source/`
when re-uploaded — see SOURCES below) at 256×256 RGBA via `rsvg-convert`.
The library scales them down to the system's menu bar height (22 px on
retina) at draw time.

## Geometry & spec

All icons share the same chassis on a 64-unit grid:

| Element | Spec |
|---|---|
| Shield path | `M32 5 C40 7 48 10 55 12 V32 C55 43.5 46.5 53 32 59 C17.5 53 9 43.5 9 32 V12 C16 10 24 7 32 5 Z` |
| Shield stroke | `currentColor`, `stroke-width=2`, `stroke-linejoin=round`, no fill |
| Z mark | scaled 0.7 around centre `(32, 32)`, fill `currentColor`. Two arrowhead paths, one rotated 180° |
| Status badge cutout | `<circle cx=49 cy=49 r=13>` punched out of the shield via `<mask>` |
| Status badge ring | `<circle cx=49 cy=49 r=10.5>` stroked, `stroke-width=2` |
| Update pip cutout (combined variant) | `<circle cx=51 cy=14 r=9>` punched out |
| Update pip dot | `<circle cx=51 cy=14 r=6.5>` filled `currentColor` |

**Render targets:** 16, 18, 22 px. macOS menu bar height is 22 px on
retina; the system handles up to 44 px for retina. The 256 px PNGs we
ship downscale cleanly at the request from the system.

**Spinner geometry (connecting variant):**

- Faint background ring: `cx=49 cy=49 r=9`, `stroke-width=2`, `opacity=0.32`
- Active arc: quarter-circle starting at `(49, 40)`, sweep `9 9 0 0 1 9 9`,
  `stroke-width=2.4`, full opacity, `stroke-linecap=round`

The source SVG includes an `<animateTransform>` rotating 360° around
`(49, 49)`, 1.1s linear, infinite. **AppKit does not animate this**
when the SVG is loaded as a static NSImage. We currently ship the
static rasterization — the incomplete dial reads as "in progress"
without animation. Frame-by-frame rotation is a follow-up.

## Monochrome nuance — do not violate

Hierarchy comes from **stroke weight** and **opacity**, never from
color.

| Layer | Treatment |
|---|---|
| Shield chassis | `currentColor`, stroke 2, opacity 1.0 |
| Z mark | `currentColor`, fill, opacity 1.0 |
| Active badge ring / glyph | `currentColor`, stroke 2 / 2.4, opacity 1.0 |
| Spinner background ring | `currentColor`, stroke 2, **opacity 0.32** |
| Update pip (combined) | `currentColor` filled disc, opacity 1.0 |

Do not introduce coloured fills (green checkmarks, amber spinners,
blue arrows). Those are reserved for the **in-app shield** family at
[`brand/in-app-status/`](../in-app-status/). The macOS menu bar
variant must stay pure currentColor / black-on-alpha so the system
can paint it.

## Accessibility

Each icon swap should set the systray tooltip / accessibility label:

- Idle → "openZro: not connected"
- Connecting → "openZro: connecting"
- Connected → "openZro: connected to mesh"
- Update → "openZro: update available"
- Connected + update → "openZro: connected, update available"

Don't rely on the icon alone — the menu's first item also states the
status as text.

## Sources

The original SVG files (`shield-mac.svg`, `shield-mac-connecting.svg`,
`shield-mac-connected.svg`, `shield-mac-update.svg`,
`shield-mac-connected-update.svg`) were delivered by Claude Design but
not preserved in this repo on the first import. To re-rasterize the
PNGs (e.g. for a new size or a tweak), drop the SVG sources under
`source/` and run:

```bash
for svg in source/*.svg; do
    name=$(basename "$svg" .svg)
    # map name → final PNG path per the table at the top
    rsvg-convert -w 256 -h 256 "$svg" -o "../../client/ui/assets/openzro-systemtray-<state>-macos.png"
done
```
