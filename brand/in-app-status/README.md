# Handoff: Openzro Shield — In-app Status Icons (Colored)

## Overview

The **colored** Openzro Shield family used **inside the app** — onboarding, dashboard, settings, alerts, the welcome screen — anywhere the brand is the focus and the violet-on-violet treatment is appropriate.

These are **not** the macOS menu bar icons. The menu bar uses a separate monochrome template family (see `design_handoff_macos_menubar_icons` for those).

| Variant | Filename | Use when… |
|---|---|---|
| Idle | `shield.svg` | Brand mark with no status overlay (logo, marketing, splash) |
| Connecting | `shield-connecting.svg` | Posture check + key exchange in progress (amber spinner) |
| Connected | `shield-connected.svg` | Peer admitted, traffic flowing (emerald check) |
| Update available | `shield-update.svg` | New client version ready to install (sky up-arrow) |
| Error | `shield-error.svg` | Posture rejected, key invalid, relay unreachable (acmel X) |
| Connected + update | `shield-connected-update.svg` | In the mesh, update pending — emerald check bottom-right + violet pip top-right |

## About the Design Files

The files in this bundle are **design references**. The SVGs themselves are production-ready
and can be shipped as-is into a web app, mobile app, or Electron/Tauri shell. The
`preview.html` file is a spec sheet for visual review — do not ship it.

The task is to **wire these SVGs into the app's existing UI** in whatever framework the app
uses (React, Vue, SwiftUI, Compose, etc.) and bind the visible variant to the daemon's
status state. The design is the SVG; the implementation is your codebase's idiomatic
status-display component.

## Fidelity

**High-fidelity.** Final colors, gradients, geometry, and badge placement are locked.
Stroke weights and opacity values are intentional — do not retouch.

## Visual system

All variants share a single chassis:

- **Shield body**: violet linear gradient `#8b5cf6 → #4c1d95` (top to bottom), with a brighter rim gradient `#c4b5fd → #6d28d9` for the 1.4 px stroke
- **Z mark**: white, two arrowhead paths mirrored (rotate 180° around centre), scaled 0.78 around `(32, 32)` of a 64-unit grid
- **Status badge** (when present): `circle cx=49 cy=49 r=14` filled `#0a0614` (the brand ink), with a coloured 2 px stroke and a coloured glyph inside
- **Update pip** (combined variant only): smaller circle top-right at `(50, 14) r=8`, fill `#0a0614`, violet stroke `#a78bfa`, violet up-arrow inside

### Status badge palette

Each status uses a single tailwind 400-shade hue so the badges read consistently at small
sizes:

| Variant | Hex | Tailwind | Glyph |
|---|---|---|---|
| Connected | `#34d399` | emerald-400 | Check (`M43 49.5 l4 4 l8 -8.4`) |
| Connecting | `#fbbf24` | amber-400 | Quarter-circle arc spinner + centre dot |
| Update | `#38bdf8` | sky-400 | Up arrow (`M49 42 V56 M42.5 48.5 L49 42 L55.5 48.5`) |
| Error | `#f87171` | red-400 | X (`M44.4 44.4 L53.6 53.6 M53.6 44.4 L44.4 53.6`) |

The badge fill is always `#0a0614` (the dark violet brand ink) so the badges sit cleanly on
top of the shield without colour mixing through. If you re-render these against a non-dark
background, swap the badge fill to whatever the page's surface colour is (or replace with a
2 px white halo).

## Status state machine

The same machine as the menu bar daemon — same 5 base states, same transitions:

```
            ┌─────────┐
            │  Idle   │  shield.svg
            └────┬────┘
                 │ user signs in / daemon enabled
                 ▼
          ┌────────────┐
          │ Connecting │  shield-connecting.svg
          └─────┬──────┘
        success │      │ failure
                ▼      ▼
          ┌────────────┐    ┌──────────┐
          │ Connected  │    │  Error   │  shield-error.svg
          └─────┬──────┘    └────┬─────┘
                │                │ retry / reconnect → Connecting
                │ update detected
                ▼
       ┌──────────────────────┐
       │ Connected + update   │  shield-connected-update.svg
       └──────────────────────┘
```

The standalone **update** variant (without `connected`) is shown when a new client version
is ready but the daemon is currently off — typically after the app sleeps overnight.

## Animation

The `connecting` SVG contains an `<animateTransform>` that rotates the spinner arc 1.1s
linear, infinite. **This works automatically when the SVG is loaded inline** (`<svg>` tag in
React/JSX, or `<svg>` literal in a Vue template, or `dangerouslySetInnerHTML` from the file
contents).

It does **not** animate when loaded as `<img src="…">` — browsers freeze SMIL inside
`<img>`. If you must use `<img>`, swap to a CSS-rotation approach: load `shield-error.svg`
or `shield-connecting.svg` minus its `<animateTransform>`, then rotate the badge group with
`@keyframes spin` in CSS.

For React, the easiest path is `vite-plugin-svgr` / `@svgr/webpack` — import the SVG as a
component and render inline; SMIL animations Just Work.

```tsx
import { ReactComponent as ShieldConnecting } from './shield-connecting.svg';
// …
<ShieldConnecting className="w-8 h-8" />
```

## Sizing

Drawn on a 64-unit grid; render targets are 16, 20, 32, 48, 64, 96 px. The badge stroke and
glyph weight are tuned to read at 16 px, but at 16 the connected+update double-badge gets
crowded — show the simpler `shield-connected.svg` at 16 and the combo only at 24+.

| Size | Use |
|---|---|
| 16 | Inline next to row labels in lists |
| 20 | Status pill in dashboards |
| 32 | Settings page, daemon-status card |
| 48 | Empty states, modals |
| 64–96 | Onboarding hero, splash |

## Accessibility

Bind each rendering to a status-aware label:

```tsx
const labels = {
  idle:               'Openzro: not connected',
  connecting:         'Openzro: connecting',
  connected:          'Openzro: connected to mesh',
  update:             'Openzro: update available',
  error:              'Openzro: connection failed',
  'connected-update': 'Openzro: connected, update available',
};

<img role="img" aria-label={labels[status]} src={`/icons/${status}.svg`} />
```

Don't rely on the icon's colour alone to convey status — every place the icon appears, also
write the status as text within the same row/card.

## Files

- `shield.svg`, `shield-connecting.svg`, `shield-connected.svg`, `shield-update.svg`, `shield-error.svg`, `shield-connected-update.svg` — production assets
- `preview.html` — design spec sheet, all 6 variants at multiple sizes plus a faux macOS menu-bar tray. Reference only.

## Out of scope

- The **macOS menu bar** monochrome template family. See `design_handoff_macos_menubar_icons` — different geometry (no badge fills, all currentColor) for AppKit's `isTemplate` rendering.
- Favicons / OG images. Crop and re-export from `shield.svg` at the required sizes.
- Dock icon (the colored Openzro Shield with rounded-square macOS bezel). Generate from `shield.svg` using Apple's icon template.
