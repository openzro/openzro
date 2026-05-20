# macOS menu bar — source SVGs

Hand-authored from the spec in `../README.md`, using the Z mark path
recovered from `brand/openzro-icon.svg` (the source SVGs Claude Design
originally shipped were never preserved, see SOURCES note in README).

Geometry tweaks vs the original spec, motivated by an operator-visible
"the menu bar icon is small and the status badges are hard to read"
report on 2026-05-20:

- Badge cutout `r` 13 → **17** (~30 % larger; cutout mask & ring both)
- Badge ring  `r` 10.5 → **14** (proportional gap preserved)
- Badge centre `(49, 49)` → **(47, 47)** so a bigger badge fits inside
  the 64-unit grid without clipping at the lower-right shield edge.
- Update pip cutout `r` 9 → **12**; pip dot `r` 6.5 → **8.5**.
- Rasterization viewBox tightened so the rendered PNG fills ~97 % of
  the 256×256 canvas (previously ~94 % after the trim/rescale pass in
  PR #85; ~70 % before that).

Stroke widths unchanged (2 on the shield chassis / ring, 2.4 on the
spinner active arc). Colour stays `currentColor` everywhere — the
file rasterises to pure black on alpha, which `systray.SetTemplateIcon`
inverts to white in dark appearance.

Regenerate the 6 PNGs under `client/ui/assets/openzro-systemtray-*-macos.png`
by running:

```
brand/macos-menubar/source/render.sh
```
